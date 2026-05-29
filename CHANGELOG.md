# Changelog

## v0.0.1 - Anchor MVP

### Added

- Defined Anchor's direction as a debugger for coding agents: GDB for Codex and Claude Code.
- Added Codex hook ingestion for raw agent events.
- Added stable `AgentEvent` IDs.
- Added project-local opt-in with `.anchor/config.json`.
- Added `anchor enable`, `anchor disable`, and `anchor status`.
- Added session storage under `.anchor/sessions/<session_id>/`.
- Added event-to-world-delta mapping with `event_world_delta_map.json`.
- Added Git-backed `WorldDelta` creation for potentially mutating hooks.
- Added patch artifacts for observed deltas.
- Added filtering so Anchor does not treat its own `.anchor/` files as world changes.
- Added tests for non-mutating hooks, Git-backed deltas, patch creation, map persistence, and project opt-in behavior.

### Planned

- Normalized `AnchorEvent` records across Codex, Claude Code, and future agents.
- More complete world tracking beyond Git patches.
- Timeline debugger for prompts, responses, tool calls, tool results, and world deltas.
- Timeline Viewer GUI for stepping through sessions visually.
- Debugging algorithm to rank suspicious events and explain likely failure points.
- Failure localization to identify where the agent first diverged from the user's intent.
- Reconstruction of the relevant world state around any agent event.
- Suggested fixes, including patches, reverts, and improved follow-up instructions.
- Claude Code support using the same event-to-world-delta model.
