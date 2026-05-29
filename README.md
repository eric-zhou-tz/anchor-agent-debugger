# Anchor

Anchor is starting as a deliberately small raw ingestion layer for Codex hook payloads.

## Raw Codex Hook Ingestion

The first CLI command reads raw JSON from stdin, attaches capture metadata, and writes a formatted raw event envelope to stdout.

```sh
cat examples/codex_hook_raw.json | anchor ingest --source codex
```

Generic usage:

```sh
cat sample_hook.json | anchor ingest --source codex
```

For local development before installing the binary:

```sh
cat examples/codex_hook_raw.json | go run ./cmd/anchor ingest --source codex
```

This layer does not parse Codex-specific fields, infer event types, normalize payloads, or connect to Postgres yet.

## Codex Hook Example

An example Codex hooks config is provided at:

```text
examples/codex_hooks.json
```

It wires a small set of lifecycle hooks into:

```sh
/bin/sh -lc 'go run /Users/ericzhou/Desktop/Productivity/Projects/Serious/Anchor/cmd/anchor ingest --source codex >>/tmp/anchor-codex-hooks.log'
```

For this local checkout, copy it into the Codex hooks location you want to use, such as `~/.codex/hooks.json`, or point Codex at it if your Codex version supports an explicit hooks path.

The `anchor ingest` command still writes the captured event envelope to stdout for direct use. The Codex hook example redirects that stdout because Codex hook stdout is reserved for hook-control responses. Durable storage will come later.
