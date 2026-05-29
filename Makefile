.PHONY: build test clean smoke

build:
	mkdir -p bin
	go build -o bin/anchor ./cmd/anchor

test:
	go test ./...

clean:
	rm -rf bin

smoke: build
	tmp=$$(mktemp -d); \
	cd "$$tmp"; \
	git init >/dev/null; \
	git config user.email test@example.com; \
	git config user.name "Test User"; \
	printf 'base\n' > tracked.txt; \
	git add tracked.txt; \
	git commit -m initial >/dev/null; \
	"$(CURDIR)/bin/anchor" enable --cwd "$$tmp"; \
	printf '{"hook_event_name":"UserPromptSubmit","session_id":"sess_smoke","cwd":"%s"}' "$$tmp" | "$(CURDIR)/bin/anchor" ingest --source codex; \
	find "$$tmp/.anchor" -maxdepth 4 -type f -print | sort
