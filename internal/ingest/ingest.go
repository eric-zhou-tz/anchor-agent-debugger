package ingest

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	// ErrEmptyInput is returned when stdin contains no JSON payload.
	ErrEmptyInput = errors.New("empty stdin")

	// ErrInvalidJSON is returned when stdin is not syntactically valid JSON.
	ErrInvalidJSON = errors.New("invalid JSON")

	// ErrMissingSource is returned when the ingest source flag is blank.
	ErrMissingSource = errors.New("missing --source")
)

const anchorConfigFile = "config.json"

// AgentEvent is the durable capture envelope for an incoming Codex hook event.
type AgentEvent struct {
	EventID       string          `json:"event_id"`
	Source        string          `json:"source"`
	ReceivedAt    time.Time       `json:"received_at"`
	SessionID     string          `json:"session_id,omitempty"`
	HookEventName string          `json:"hook_event_name,omitempty"`
	ToolName      string          `json:"tool_name,omitempty"`
	CWD           string          `json:"cwd,omitempty"`
	RawPayload    json.RawMessage `json:"raw_payload"`
}

// RawEvent is kept as an alias for the original barebones ingestion API.
type RawEvent = AgentEvent

// WorldDelta records an observed repository change after a hook triggered Git
// inspection. Hooks are only triggers; Git status and diff output are the
// source of truth for whether Anchor observed a world change.
type WorldDelta struct {
	ID              string       `json:"id"`
	SessionID       string       `json:"session_id"`
	AgentEventID    string       `json:"agent_event_id"`
	CWD             string       `json:"cwd"`
	CreatedAt       string       `json:"created_at"`
	StatusPorcelain string       `json:"status_porcelain"`
	ChangedFiles    []FileChange `json:"changed_files"`
	PatchPath       string       `json:"patch_path,omitempty"`
	UntrackedFiles  []string     `json:"untracked_files,omitempty"`
}

type FileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type GitInspection struct {
	StatusPorcelain string
	Diff            string
	CachedDiff      string
	UntrackedFiles  []string
}

type GitInspector interface {
	Inspect(cwd string) (*GitInspection, error)
}

type ProjectConfig struct {
	Enabled bool `json:"enabled"`
	Version int  `json:"version"`
}

// Options provides test seams for event metadata generation.
type Options struct {
	Now              func() time.Time
	GenerateID       func() (string, error)
	GenerateDeltaID  func() (string, error)
	StorageRoot      string
	GitInspector     GitInspector
	SkipEnabledCheck bool
}

// Capture validates raw JSON input and wraps it in a raw event envelope.
func Capture(raw []byte, source string, opts Options) (*AgentEvent, error) {
	if strings.TrimSpace(source) == "" {
		return nil, ErrMissingSource
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, ErrEmptyInput
	}

	if !json.Valid(trimmed) {
		return nil, ErrInvalidJSON
	}

	now := opts.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}

	generateID := opts.GenerateID
	if generateID == nil {
		generateID = NewEventID
	}

	eventID, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generate event id: %w", err)
	}

	fields := parseHookFields(trimmed)

	return &AgentEvent{
		EventID:       eventID,
		Source:        strings.TrimSpace(source),
		ReceivedAt:    now().UTC(),
		SessionID:     fields.SessionID,
		HookEventName: fields.HookEventName,
		ToolName:      fields.ToolName,
		CWD:           fields.CWD,
		RawPayload:    json.RawMessage(trimmed),
	}, nil
}

// Ingest captures a hook event, appends it to session storage, and maps the
// event to a WorldDelta ID or null. Write-capable hooks trigger Git inspection,
// but Anchor only creates a WorldDelta when Git reports changed files.
func Ingest(raw []byte, source string, opts Options) (*AgentEvent, *WorldDelta, error) {
	event, err := Capture(raw, source, opts)
	if err != nil {
		return nil, nil, err
	}

	sessionID := event.SessionID
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "unknown_session"
	}
	event.SessionID = sessionID

	cwd := event.CWD
	if strings.TrimSpace(cwd) == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("get cwd: %w", err)
		}
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve cwd: %w", err)
	}
	event.CWD = cwd

	if !opts.SkipEnabledCheck {
		enabled, err := IsProjectEnabled(cwd)
		if err != nil {
			return nil, nil, err
		}
		if !enabled {
			return nil, nil, nil
		}
	}

	storageRoot := opts.StorageRoot
	if strings.TrimSpace(storageRoot) == "" {
		storageRoot = ProjectStorageRoot(cwd)
	}

	store := sessionStore{
		root:      storageRoot,
		sessionID: sessionID,
	}
	var delta *WorldDelta

	if err := store.WithLock(func() error {
		if err := store.AppendEvent(event); err != nil {
			return err
		}

		if IsPotentialWorldChangeHook(event) {
			inspector := opts.GitInspector
			if inspector == nil {
				inspector = GitCommandInspector{}
			}

			inspection, err := inspector.Inspect(cwd)
			if err != nil {
				return err
			}
			inspection = FilterAnchorFiles(inspection)

			if strings.TrimSpace(inspection.StatusPorcelain) != "" {
				delta, err = buildWorldDelta(event, sessionID, cwd, inspection, opts)
				if err != nil {
					return err
				}
				if err := store.WriteDelta(delta, inspection); err != nil {
					return err
				}
			}
		}

		var deltaID *string
		if delta != nil {
			deltaID = &delta.ID
		}
		return store.UpdateEventWorldDeltaMap(event.EventID, deltaID)
	}); err != nil {
		return nil, nil, err
	}

	return event, delta, nil
}

// NewEventID returns a random event identifier for captured raw events.
func NewEventID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return "evt_" + hex.EncodeToString(b[:]), nil
}

func NewWorldDeltaID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return "delta_" + hex.EncodeToString(b[:]), nil
}

func ProjectStorageRoot(cwd string) string {
	return filepath.Join(cwd, ".anchor")
}

func EnableProject(cwd string) error {
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	if err := writeProjectConfig(cwd, ProjectConfig{Enabled: true, Version: 1}); err != nil {
		return err
	}
	if err := addAnchorToLocalGitExclude(cwd); err != nil {
		return err
	}
	return nil
}

func DisableProject(cwd string) error {
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	return writeProjectConfig(cwd, ProjectConfig{Enabled: false, Version: 1})
}

func IsProjectEnabled(cwd string) (bool, error) {
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return false, fmt.Errorf("resolve cwd: %w", err)
	}

	raw, err := os.ReadFile(filepath.Join(ProjectStorageRoot(cwd), anchorConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read anchor config: %w", err)
	}

	var config ProjectConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return false, fmt.Errorf("decode anchor config: %w", err)
	}
	return config.Enabled, nil
}

func writeProjectConfig(cwd string, config ProjectConfig) error {
	root := ProjectStorageRoot(cwd)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create anchor config dir: %w", err)
	}

	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode anchor config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, anchorConfigFile), append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write anchor config: %w", err)
	}
	return nil
}

func addAnchorToLocalGitExclude(cwd string) error {
	gitDir, err := runGit(cwd, "rev-parse", "--git-dir")
	if err != nil {
		return nil
	}

	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(cwd, gitDir)
	}

	excludePath := filepath.Join(gitDir, "info", "exclude")
	raw, err := os.ReadFile(excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read git exclude: %w", err)
	}

	for _, line := range splitLines(string(raw)) {
		if strings.TrimSpace(line) == ".anchor/" {
			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("create git info dir: %w", err)
	}

	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open git exclude: %w", err)
	}
	defer f.Close()

	if len(raw) > 0 && !bytes.HasSuffix(raw, []byte("\n")) {
		if _, err := f.WriteString("\n"); err != nil {
			return fmt.Errorf("append git exclude newline: %w", err)
		}
	}
	if _, err := f.WriteString(".anchor/\n"); err != nil {
		return fmt.Errorf("append git exclude: %w", err)
	}
	return nil
}

func IsPotentialWorldChangeHook(event *AgentEvent) bool {
	if event == nil || !strings.EqualFold(event.HookEventName, "PostToolUse") {
		return false
	}

	toolName := strings.TrimSpace(event.ToolName)
	switch strings.ToLower(toolName) {
	case "bash", "apply_patch", "edit", "write":
		return true
	}

	if strings.HasPrefix(strings.ToLower(toolName), "mcp__") {
		return appearsWriteCapable(toolName)
	}

	return false
}

func appearsWriteCapable(toolName string) bool {
	name := strings.ToLower(toolName)
	writeWords := []string{
		"write",
		"edit",
		"patch",
		"create",
		"update",
		"delete",
		"remove",
		"move",
		"rename",
		"insert",
		"upsert",
		"apply",
	}
	for _, word := range writeWords {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}

func buildWorldDelta(event *AgentEvent, sessionID, cwd string, inspection *GitInspection, opts Options) (*WorldDelta, error) {
	generateDeltaID := opts.GenerateDeltaID
	if generateDeltaID == nil {
		generateDeltaID = NewWorldDeltaID
	}

	deltaID, err := generateDeltaID()
	if err != nil {
		return nil, fmt.Errorf("generate world delta id: %w", err)
	}

	now := opts.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}

	delta := &WorldDelta{
		ID:              deltaID,
		SessionID:       sessionID,
		AgentEventID:    event.EventID,
		CWD:             cwd,
		CreatedAt:       now().UTC().Format(time.RFC3339Nano),
		StatusPorcelain: inspection.StatusPorcelain,
		ChangedFiles:    ParseGitStatusPorcelain(inspection.StatusPorcelain),
		UntrackedFiles:  inspection.UntrackedFiles,
		PatchPath:       filepath.ToSlash(filepath.Join("deltas", deltaID+".patch")),
	}

	return delta, nil
}

type hookFields struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
	ToolName      string `json:"tool_name"`
	CWD           string `json:"cwd"`
}

func parseHookFields(raw []byte) hookFields {
	var fields hookFields
	_ = json.Unmarshal(raw, &fields)
	return fields
}

type GitCommandInspector struct{}

func (GitCommandInspector) Inspect(cwd string) (*GitInspection, error) {
	status, err := runGit(cwd, "status", "--porcelain=v1")
	if err != nil {
		return nil, err
	}
	diff, err := runGit(cwd, "diff", "--binary")
	if err != nil {
		return nil, err
	}
	cachedDiff, err := runGit(cwd, "diff", "--cached", "--binary")
	if err != nil {
		return nil, err
	}
	untracked, err := runGit(cwd, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}

	return &GitInspection{
		StatusPorcelain: status,
		Diff:            diff,
		CachedDiff:      cachedDiff,
		UntrackedFiles:  splitLines(untracked),
	}, nil
}

func runGit(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func FilterAnchorFiles(inspection *GitInspection) *GitInspection {
	if inspection == nil {
		return &GitInspection{}
	}

	return &GitInspection{
		StatusPorcelain: filterStatusPorcelain(inspection.StatusPorcelain),
		Diff:            filterDiffByFile(inspection.Diff),
		CachedDiff:      filterDiffByFile(inspection.CachedDiff),
		UntrackedFiles:  filterAnchorPaths(inspection.UntrackedFiles),
	}
}

func ParseGitStatusPorcelain(status string) []FileChange {
	var changes []FileChange
	for _, line := range splitLines(status) {
		if len(line) < 3 {
			continue
		}

		rawStatus := line[:2]
		path := line[3:]
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}

		changes = append(changes, FileChange{
			Path:   unquotePorcelainPath(path),
			Status: strings.TrimSpace(rawStatus),
		})
	}
	return changes
}

func filterStatusPorcelain(status string) string {
	var kept []string
	for _, line := range splitLines(status) {
		if len(line) < 3 {
			continue
		}
		if isAnchorPath(statusLinePath(line)) {
			continue
		}
		kept = append(kept, line)
	}
	return joinGitLines(kept)
}

func statusLinePath(line string) string {
	path := line[3:]
	if strings.Contains(path, " -> ") {
		parts := strings.Split(path, " -> ")
		path = parts[len(parts)-1]
	}
	return unquotePorcelainPath(path)
}

func filterAnchorPaths(paths []string) []string {
	var kept []string
	for _, path := range paths {
		if isAnchorPath(path) {
			continue
		}
		kept = append(kept, path)
	}
	return kept
}

func isAnchorPath(path string) bool {
	cleaned := filepath.ToSlash(strings.TrimPrefix(path, "./"))
	return cleaned == ".anchor" || strings.HasPrefix(cleaned, ".anchor/")
}

func filterDiffByFile(diff string) string {
	lines := splitLines(diff)
	if len(lines) == 0 {
		return ""
	}

	var kept []string
	for i := 0; i < len(lines); {
		line := lines[i]
		if !strings.HasPrefix(line, "diff --git ") {
			kept = append(kept, line)
			i++
			continue
		}

		j := i + 1
		for j < len(lines) && !strings.HasPrefix(lines[j], "diff --git ") {
			j++
		}
		if !diffHeaderTouchesAnchor(line) {
			kept = append(kept, lines[i:j]...)
		}
		i = j
	}

	return joinGitLines(kept)
}

func diffHeaderTouchesAnchor(header string) bool {
	fields := strings.Fields(header)
	for _, field := range fields {
		if strings.HasPrefix(field, "a/") || strings.HasPrefix(field, "b/") {
			path := strings.TrimPrefix(strings.TrimPrefix(field, "a/"), "b/")
			if isAnchorPath(path) {
				return true
			}
		}
	}
	return false
}

func joinGitLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func unquotePorcelainPath(path string) string {
	if !strings.HasPrefix(path, `"`) {
		return path
	}
	unquoted, err := strconv.Unquote(path)
	if err != nil {
		return path
	}
	return unquoted
}

func splitLines(s string) []string {
	trimmed := strings.TrimRight(s, "\r\n")
	if trimmed == "" {
		return nil
	}
	rawLines := strings.Split(trimmed, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, strings.TrimSuffix(line, "\r"))
	}
	return lines
}

type sessionStore struct {
	root      string
	sessionID string
}

func (s sessionStore) sessionDir() string {
	return filepath.Join(s.root, "sessions", safePathName(s.sessionID))
}

func (s sessionStore) deltasDir() string {
	return filepath.Join(s.sessionDir(), "deltas")
}

func (s sessionStore) WithLock(fn func() error) error {
	if err := os.MkdirAll(s.sessionDir(), 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	lockFile, err := os.OpenFile(filepath.Join(s.sessionDir(), "event_world_delta_map.json"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open session lock: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock session: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	return fn()
}

func (s sessionStore) AppendEvent(event *AgentEvent) error {
	if err := os.MkdirAll(s.sessionDir(), 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(s.sessionDir(), "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open events jsonl: %w", err)
	}
	defer f.Close()

	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode agent event: %w", err)
	}
	if _, err := f.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append agent event: %w", err)
	}
	return nil
}

func (s sessionStore) WriteDelta(delta *WorldDelta, inspection *GitInspection) error {
	if err := os.MkdirAll(s.deltasDir(), 0o755); err != nil {
		return fmt.Errorf("create deltas dir: %w", err)
	}

	encoded, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode world delta: %w", err)
	}
	deltaPath := filepath.Join(s.deltasDir(), delta.ID+".json")
	if err := os.WriteFile(deltaPath, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write world delta: %w", err)
	}

	patch := inspection.Diff + "\n# --- staged changes ---\n" + inspection.CachedDiff
	patchPath := filepath.Join(s.sessionDir(), filepath.FromSlash(delta.PatchPath))
	if err := os.WriteFile(patchPath, []byte(patch), 0o644); err != nil {
		return fmt.Errorf("write world delta patch: %w", err)
	}

	return nil
}

func (s sessionStore) UpdateEventWorldDeltaMap(eventID string, deltaID *string) error {
	if err := os.MkdirAll(s.sessionDir(), 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	path := filepath.Join(s.sessionDir(), "event_world_delta_map.json")
	mapping := map[string]*string{}
	if raw, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &mapping); err != nil {
			return fmt.Errorf("decode event world delta map: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read event world delta map: %w", err)
	}

	mapping[eventID] = deltaID

	encoded, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return fmt.Errorf("encode event world delta map: %w", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write event world delta map: %w", err)
	}
	return nil
}

func safePathName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown_session"
	}
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}
