package ingest

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCaptureValidRawJSONInput(t *testing.T) {
	got, err := Capture([]byte(`{"hook":"example","nested":{"ok":true}}`), "codex", Options{
		Now: func() time.Time {
			return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
		},
		GenerateID: func() (string, error) {
			return "evt_test", nil
		},
	})
	if err != nil {
		t.Fatalf("Capture returned error: %v", err)
	}

	if got.EventID != "evt_test" {
		t.Fatalf("EventID = %q, want %q", got.EventID, "evt_test")
	}
	if got.Source != "codex" {
		t.Fatalf("Source = %q, want %q", got.Source, "codex")
	}
	if !got.ReceivedAt.Equal(time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("ReceivedAt = %s, want fixed test time", got.ReceivedAt)
	}
	if string(got.RawPayload) != `{"hook":"example","nested":{"ok":true}}` {
		t.Fatalf("RawPayload = %s", got.RawPayload)
	}
}

func TestCaptureInvalidJSONInput(t *testing.T) {
	_, err := Capture([]byte(`{"hook":`), "codex", Options{})
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("Capture error = %v, want %v", err, ErrInvalidJSON)
	}
}

func TestCaptureEmptyInput(t *testing.T) {
	_, err := Capture([]byte(" \n\t "), "codex", Options{})
	if !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("Capture error = %v, want %v", err, ErrEmptyInput)
	}
}

func TestCaptureMissingSource(t *testing.T) {
	_, err := Capture([]byte(`{"hook":"example"}`), "", Options{})
	if !errors.Is(err, ErrMissingSource) {
		t.Fatalf("Capture error = %v, want %v", err, ErrMissingSource)
	}
}

func TestIngestDisabledProjectNoops(t *testing.T) {
	repo := newGitRepo(t)
	raw := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"sess_disabled","cwd":"` + repo + `"}`)

	event, delta, err := Ingest(raw, "codex", Options{
		GenerateID: func() (string, error) {
			return "evt_disabled", nil
		},
	})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if event != nil || delta != nil {
		t.Fatalf("event, delta = %#v, %#v; want nil, nil for disabled project", event, delta)
	}
	if _, err := os.Stat(filepath.Join(repo, ".anchor")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".anchor stat err = %v, want not exist", err)
	}
}

func TestEnableProjectAllowsIngest(t *testing.T) {
	repo := newGitRepo(t)
	if err := EnableProject(repo); err != nil {
		t.Fatalf("EnableProject returned error: %v", err)
	}

	enabled, err := IsProjectEnabled(repo)
	if err != nil {
		t.Fatalf("IsProjectEnabled returned error: %v", err)
	}
	if !enabled {
		t.Fatal("project is disabled, want enabled")
	}

	raw := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"sess_enabled","cwd":"` + repo + `"}`)
	event, delta, err := Ingest(raw, "codex", Options{
		GenerateID: func() (string, error) {
			return "evt_enabled", nil
		},
	})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if event == nil {
		t.Fatal("event = nil, want captured event for enabled project")
	}
	if delta != nil {
		t.Fatalf("delta = %#v, want nil for non-mutating hook", delta)
	}

	got := readEventWorldDeltaMap(t, filepath.Join(repo, ".anchor"), "sess_enabled")
	if value, ok := got["evt_enabled"]; !ok || value != nil {
		t.Fatalf("event_world_delta_map entry = %#v, ok=%v, want null entry", value, ok)
	}

	exclude, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read git exclude: %v", err)
	}
	if !strings.Contains(string(exclude), ".anchor/") {
		t.Fatalf("git exclude missing .anchor/: %q", exclude)
	}
}

func TestIngestNonMutatingHookMapsToNullAndSkipsWorldScan(t *testing.T) {
	inspector := &recordingInspector{}
	storageRoot := t.TempDir()
	raw := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"sess_test","cwd":"/tmp/project"}`)

	event, delta, err := Ingest(raw, "codex", Options{
		StorageRoot:      storageRoot,
		GitInspector:     inspector,
		SkipEnabledCheck: true,
		GenerateID: func() (string, error) {
			return "evt_non_mutating", nil
		},
	})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if delta != nil {
		t.Fatalf("delta = %#v, want nil", delta)
	}
	if inspector.calls != 0 {
		t.Fatalf("Git inspector calls = %d, want 0", inspector.calls)
	}

	got := readEventWorldDeltaMap(t, storageRoot, event.SessionID)
	if value, ok := got["evt_non_mutating"]; !ok || value != nil {
		t.Fatalf("event_world_delta_map entry = %#v, ok=%v, want null entry", value, ok)
	}
}

func TestIngestPostToolUseBashWithNoRepoChangesMapsToNull(t *testing.T) {
	repo := newGitRepo(t)
	storageRoot := t.TempDir()
	raw := []byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","session_id":"sess_clean","cwd":"` + repo + `"}`)

	event, delta, err := Ingest(raw, "codex", Options{
		StorageRoot:      storageRoot,
		SkipEnabledCheck: true,
		GenerateID: func() (string, error) {
			return "evt_clean_bash", nil
		},
	})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if delta != nil {
		t.Fatalf("delta = %#v, want nil", delta)
	}

	got := readEventWorldDeltaMap(t, storageRoot, event.SessionID)
	if value, ok := got["evt_clean_bash"]; !ok || value != nil {
		t.Fatalf("event_world_delta_map entry = %#v, ok=%v, want null entry", value, ok)
	}
}

func TestIngestPostToolUseBashAfterFileEditCreatesWorldDelta(t *testing.T) {
	repo := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("edit tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	storageRoot := t.TempDir()
	raw := []byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","session_id":"sess_dirty","cwd":"` + repo + `"}`)

	event, delta, err := Ingest(raw, "codex", Options{
		StorageRoot:      storageRoot,
		SkipEnabledCheck: true,
		GenerateID: func() (string, error) {
			return "evt_dirty_bash", nil
		},
		GenerateDeltaID: func() (string, error) {
			return "delta_dirty", nil
		},
		Now: func() time.Time {
			return time.Date(2026, 5, 29, 12, 30, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if delta == nil {
		t.Fatal("delta = nil, want WorldDelta")
	}
	if delta.ID != "delta_dirty" {
		t.Fatalf("delta ID = %q, want delta_dirty", delta.ID)
	}
	if delta.AgentEventID != event.EventID {
		t.Fatalf("delta AgentEventID = %q, want %q", delta.AgentEventID, event.EventID)
	}
	if delta.PatchPath == "" {
		t.Fatal("PatchPath is empty, want patch path for tracked file diff")
	}
	if !reflect.DeepEqual(delta.UntrackedFiles, []string{"new.txt"}) {
		t.Fatalf("UntrackedFiles = %#v, want [new.txt]", delta.UntrackedFiles)
	}

	mapFile := readEventWorldDeltaMap(t, storageRoot, event.SessionID)
	if value := mapFile["evt_dirty_bash"]; value == nil || *value != "delta_dirty" {
		t.Fatalf("event_world_delta_map entry = %#v, want delta_dirty", value)
	}

	deltaPath := filepath.Join(storageRoot, "sessions", "sess_dirty", "deltas", "delta_dirty.json")
	if _, err := os.Stat(deltaPath); err != nil {
		t.Fatalf("stat delta json: %v", err)
	}
}

func TestParseGitStatusPorcelain(t *testing.T) {
	got := ParseGitStatusPorcelain(strings.Join([]string{
		" M modified.txt",
		"A  staged.txt",
		"MM both.txt",
		"R  old.txt -> renamed.txt",
		"?? new.txt",
	}, "\n"))

	want := []FileChange{
		{Path: "modified.txt", Status: "M"},
		{Path: "staged.txt", Status: "A"},
		{Path: "both.txt", Status: "MM"},
		{Path: "renamed.txt", Status: "R"},
		{Path: "new.txt", Status: "??"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseGitStatusPorcelain() = %#v, want %#v", got, want)
	}
}

func TestPatchFileIsWrittenWhenDiffExists(t *testing.T) {
	repo := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("edit tracked file: %v", err)
	}

	storageRoot := t.TempDir()
	raw := []byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","session_id":"sess_patch","cwd":"` + repo + `"}`)

	_, delta, err := Ingest(raw, "codex", Options{
		StorageRoot:      storageRoot,
		SkipEnabledCheck: true,
		GenerateID: func() (string, error) {
			return "evt_patch", nil
		},
		GenerateDeltaID: func() (string, error) {
			return "delta_patch", nil
		},
	})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if delta == nil {
		t.Fatal("delta = nil, want WorldDelta")
	}

	patchPath := filepath.Join(storageRoot, "sessions", "sess_patch", filepath.FromSlash(delta.PatchPath))
	patch, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("read patch file: %v", err)
	}
	if !strings.Contains(string(patch), "diff --git a/tracked.txt b/tracked.txt") {
		t.Fatalf("patch file does not contain tracked diff:\n%s", patch)
	}
	if !strings.Contains(string(patch), "# --- staged changes ---") {
		t.Fatalf("patch file missing separator:\n%s", patch)
	}
}

func TestUntrackedOnlyDeltaWritesPatchFile(t *testing.T) {
	repo := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	storageRoot := t.TempDir()
	raw := []byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","session_id":"sess_untracked","cwd":"` + repo + `"}`)

	_, delta, err := Ingest(raw, "codex", Options{
		StorageRoot:      storageRoot,
		SkipEnabledCheck: true,
		GenerateID: func() (string, error) {
			return "evt_untracked", nil
		},
		GenerateDeltaID: func() (string, error) {
			return "delta_untracked", nil
		},
	})
	if err != nil {
		t.Fatalf("Ingest returned error: %v", err)
	}
	if delta == nil {
		t.Fatal("delta = nil, want WorldDelta")
	}
	if delta.PatchPath != "deltas/delta_untracked.patch" {
		t.Fatalf("PatchPath = %q, want deltas/delta_untracked.patch", delta.PatchPath)
	}

	patch, err := os.ReadFile(filepath.Join(storageRoot, "sessions", "sess_untracked", filepath.FromSlash(delta.PatchPath)))
	if err != nil {
		t.Fatalf("read patch file: %v", err)
	}
	if strings.TrimSpace(string(patch)) != "# --- staged changes ---" {
		t.Fatalf("patch file = %q, want only separator for untracked-only delta", patch)
	}
}

func TestFilterAnchorFilesFromInspection(t *testing.T) {
	got := FilterAnchorFiles(&GitInspection{
		StatusPorcelain: strings.Join([]string{
			"?? .anchor/",
			" M tracked.txt",
			"?? .anchor/sessions/sess/events.jsonl",
			"?? new.txt",
		}, "\n") + "\n",
		Diff: strings.Join([]string{
			"diff --git a/.anchor/log b/.anchor/log",
			"new file mode 100644",
			"index 0000000..1111111",
			"--- /dev/null",
			"+++ b/.anchor/log",
			"@@ -0,0 +1 @@",
			"+log",
			"diff --git a/tracked.txt b/tracked.txt",
			"index 1111111..2222222 100644",
			"--- a/tracked.txt",
			"+++ b/tracked.txt",
			"@@ -1 +1 @@",
			"-old",
			"+new",
		}, "\n") + "\n",
		UntrackedFiles: []string{
			".anchor/sessions/sess/events.jsonl",
			"new.txt",
		},
	})

	if strings.Contains(got.StatusPorcelain, ".anchor") {
		t.Fatalf("StatusPorcelain still contains .anchor: %q", got.StatusPorcelain)
	}
	if strings.Contains(got.Diff, ".anchor") {
		t.Fatalf("Diff still contains .anchor:\n%s", got.Diff)
	}
	if !strings.Contains(got.Diff, "diff --git a/tracked.txt b/tracked.txt") {
		t.Fatalf("Diff lost tracked file section:\n%s", got.Diff)
	}
	if !reflect.DeepEqual(got.UntrackedFiles, []string{"new.txt"}) {
		t.Fatalf("UntrackedFiles = %#v, want [new.txt]", got.UntrackedFiles)
	}
}

func TestIngestMaintainsMapEntriesForEveryEvent(t *testing.T) {
	storageRoot := t.TempDir()
	for _, eventID := range []string{"evt_one", "evt_two", "evt_three"} {
		id := eventID
		_, _, err := Ingest([]byte(`{"hook_event_name":"UserPromptSubmit","session_id":"sess_many","cwd":"/tmp/project"}`), "codex", Options{
			StorageRoot:      storageRoot,
			SkipEnabledCheck: true,
			GenerateID: func() (string, error) {
				return id, nil
			},
		})
		if err != nil {
			t.Fatalf("Ingest %s returned error: %v", eventID, err)
		}
	}

	got := readEventWorldDeltaMap(t, storageRoot, "sess_many")
	if len(got) != 3 {
		t.Fatalf("map entry count = %d, want 3: %#v", len(got), got)
	}
	for _, eventID := range []string{"evt_one", "evt_two", "evt_three"} {
		if value, ok := got[eventID]; !ok || value != nil {
			t.Fatalf("map[%s] = %#v, ok=%v, want null entry", eventID, value, ok)
		}
	}
}

type recordingInspector struct {
	calls int
}

func (r *recordingInspector) Inspect(string) (*GitInspection, error) {
	r.calls++
	return &GitInspection{}, nil
}

func newGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	run(t, dir, "git", "add", "tracked.txt")
	run(t, dir, "git", "commit", "-m", "initial")

	return dir
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func readEventWorldDeltaMap(t *testing.T, storageRoot, sessionID string) map[string]*string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(storageRoot, "sessions", sessionID, "event_world_delta_map.json"))
	if err != nil {
		t.Fatalf("read event world delta map: %v", err)
	}

	var got map[string]*string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode event world delta map: %v", err)
	}
	return got
}
