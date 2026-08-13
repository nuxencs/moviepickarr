.PHONY: all deps test test/e2e build build/app build/web clean fmt gofix-changed lint precommit dev dev/fixtures dev/fixtures-reset prod
.POSIX:
.SUFFIXES:

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse HEAD 2> /dev/null)
BINARY_NAME = moviepickarr
WEB_DIR = web
BINDIR = bin

all: clean build

deps:
	bun install --cwd ./$(WEB_DIR) --frozen-lockfile
	go mod download

test:
	go test -race -count=3 -v ./...
	@echo "Testing frontend..."
	cd $(WEB_DIR) && bun run test

test/e2e:
	bun run --cwd ./$(WEB_DIR) test:e2e

build: build/web build/app

build/app: deps
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(GIT_COMMIT)" -o $(BINDIR)/$(BINARY_NAME) main.go

build/web: deps
	bun run --cwd ./$(WEB_DIR) build
	@touch $(WEB_DIR)/dist/.gitkeep 2>/dev/null  # To avoid accidental commit of the deletion

clean:
	rm -rf $(BINDIR)
	rm -rf $(WEB_DIR)/dist

fmt:
	@echo "Formatting changed Go code..."
	@gofiles=$$({ git diff --name-only --diff-filter=d; git diff --name-only --cached --diff-filter=d; } | sort -u | grep '\.go$$' || true); \
		if [ -n "$$gofiles" ]; then echo "$$gofiles" | xargs gofmt -w; fi
	@echo "Formatting changed frontend code..."
	@webfiles=$$({ git diff --name-only --diff-filter=d -- '$(WEB_DIR)/'; git diff --name-only --cached --diff-filter=d -- '$(WEB_DIR)/'; } | sort -u | sed 's|^$(WEB_DIR)/||' | grep -E '\.(ts|tsx|js|jsx)$$' || true); \
		if [ -n "$$webfiles" ]; then cd $(WEB_DIR) && echo "$$webfiles" | xargs bun run lint --fix; fi

gofix-changed:
	@echo "Running go fix on changed Go files..."
	@gofiles=$$({ git diff --name-only --diff-filter=d; git diff --name-only --cached --diff-filter=d; } | sort -u | grep '\.go$$' || true); \
		if [ -z "$$gofiles" ]; then \
			echo "No changed Go files for go fix."; \
			exit 0; \
		fi; \
		gopkgs=$$(printf '%s\n' "$$gofiles" | xargs -n 1 dirname | sort -u); \
		printf '%s\n' "$$gopkgs" | while IFS= read -r pkg; do \
			[ -n "$$pkg" ] || continue; \
			go fix "./$$pkg" || true; \
		done; \
		tmp=$$(mktemp); \
		printf '%s\n' "$$gopkgs" | while IFS= read -r pkg; do \
			[ -n "$$pkg" ] || continue; \
			go fix -diff "./$$pkg" >> "$$tmp" || true; \
		done; \
		if [ -s "$$tmp" ]; then \
			echo "go fix left pending changes for changed Go files:"; \
			cat "$$tmp"; \
			rm -f "$$tmp"; \
			echo "Re-run 'make gofix-changed'."; \
			exit 1; \
		fi; \
		rm -f "$$tmp"; \
		echo "go fix applied."

lint:
	@echo "Linting changed Go code..."
	golangci-lint run --new-from-merge-base=develop --timeout=5m
	@echo "Linting frontend..."
	cd $(WEB_DIR) && bun run lint

precommit: fmt gofix-changed lint
	@echo "Pre-commit checks passed."

prod: build
	./$(BINDIR)/$(BINARY_NAME)

dev:
	@if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux is not installed. Please install it to use dev mode."; \
		echo "On Ubuntu/Debian: sudo apt install tmux"; \
		echo "On macOS: brew install tmux"; \
		exit 1; \
	fi
	@tmux new-session -d -s moviepickarr-dev 'bun run --cwd ./$(WEB_DIR) dev'
	@tmux split-window -h 'go run main.go'
	@tmux -2 attach-session -d

# Load a full developer dataset (roster with logins, movies across every state,
# watched history, an active turn holder) into the local DB. Refuses on a
# non-empty DB; use dev/fixtures-reset to wipe and reload.
dev/fixtures:
	go run ./cmd/devfixtures

dev/fixtures-reset:
	go run ./cmd/devfixtures -reset
