# Anchor

Anchor is aiming to become GDB for coding agents like Codex and Claude Code.

When an agent breaks a project, the hard part is usually not the final diff. It is figuring out where the agent's reasoning, tool use, and filesystem changes first diverged from the user's intent. Anchor records enough context to let you step through that path, see exactly where the agent went wrong, reconstruct the failure, and generate suggested fixes.

## Direction

Anchor is being built around a few core workflows:

- Trace an agent session from prompt to tool call to observed repo change.
- Identify the exact event where the agent's behavior diverged.
- Reconstruct the changed world state around that event.
- Compare agent intent, tool input, tool output, and Git-observed mutations.
- Suggest fixes that can repair the project or improve the next agent instruction.

The long-term goal is an agent debugging loop:

```text
agent action -> observed world delta -> failure point -> reconstruction -> suggested fix
```

## Upcoming Features

### Normalized AnchorEvents

Anchor should normalize raw hook payloads into a stable event model across Codex, Claude Code, and future agents. Raw payloads will stay available, but the debugger should work against consistent fields for prompts, responses, tool calls, tool results, and session metadata.

### More Complete World Tracking

Anchor should track more than Git patches. The world model can grow to include snapshots, file metadata, command output references, ignored-file handling, and eventually enough state to reconstruct or replay important parts of a session.

### Timeline Debugger

Anchor should expose an ordered session timeline: user prompts, assistant responses, tool calls, tool results, and world deltas. The timeline should make it obvious which action changed the repository and what the agent thought it was doing at the time.

### Timeline Viewer

Anchor should provide a GUI for stepping through a session. The viewer should make prompts, tool calls, diffs, world deltas, and suggested fixes readable without digging through JSONL files.

### Failure Localization

Anchor should help answer: "Where did the agent go wrong?" That means correlating the user's request with each tool call and Git-observed change, then highlighting the first suspicious divergence.

### Debugging Algorithm

Anchor should develop a debugging algorithm that walks the event timeline, compares intent against observed changes, ranks suspicious events, and explains why a specific step is likely where the agent went wrong.

### Reconstruction

Anchor should be able to reconstruct the relevant world state around an agent event. The first version records Git patches. Later versions can add snapshots, replay, and richer filesystem state so debugging does not depend on the current working tree.

### Suggested Fixes

Anchor should propose fixes after it identifies a bad step. A suggested fix might be a patch, a revert, a follow-up instruction for Codex or Claude Code, or a tighter prompt that prevents the same mistake from recurring.

### Multi-Agent Support

Codex hooks are the first integration. Claude Code and other coding agents should fit the same model: agent events trigger inspection, but Git and filesystem observations remain the source of truth.

## Current MVP

The current implementation is intentionally small:

- Ingests Codex hook JSON.
- Assigns a stable `AgentEvent` ID for each logged hook.
- Opts in per project with `.anchor/config.json`.
- Records events in `.anchor/sessions/<session_id>/events.jsonl`.
- Detects potentially mutating hooks and inspects Git.
- Creates `WorldDelta` JSON files only when Git reports changes.
- Stores patch files under `.anchor/sessions/<session_id>/deltas/`.
- Records `event_world_delta_map.json` so each event maps to a delta ID or `null`.

Hooks are triggers. Git inspection is the source of truth for observed world changes.

## Usage

Build the local binary:

```sh
make build
```

Enable Anchor for a project before running Codex there:

```sh
/path/to/Anchor/bin/anchor enable --cwd /path/to/project
```

Check project status:

```sh
/path/to/Anchor/bin/anchor status --cwd /path/to/project
```

Disable Anchor for a project:

```sh
/path/to/Anchor/bin/anchor disable --cwd /path/to/project
```

When enabled, Codex hooks write project-local artifacts:

```text
.anchor/
  config.json
  sessions/
    <session_id>/
      events.jsonl
      event_world_delta_map.json
      deltas/
        <world_delta_id>.json
        <world_delta_id>.patch
```

## Development

Run tests:

```sh
make test
```

Run a smoke test in a temporary enabled repo:

```sh
make smoke
```

An example Codex hooks config is provided in `examples/codex_hooks.json`.
