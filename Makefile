.PHONY: all deps build build/app build/web clean dev
.POSIX:
.SUFFIXES:

SERVICE = moviepickarr
GO = go
RM = rm
BINDIR = bin

all: clean build

deps:
	bun install --cwd ./web --frozen-lockfile
	go mod download

build: deps build/web build/app

build/app:
	go build -o $(BINDIR)/$(SERVICE) main.go

build/web:
	bun run --cwd ./web build
	@touch web/dist/.gitkeep 2>/dev/null  # To avoid accidental commit of the deletionn

clean:
	$(RM) -rf $(BINDIR)

dev:
	@if ! command -v tmux >/dev/null 2>&1; then \
		echo "tmux is not installed. Please install it to use dev mode."; \
		echo "On Ubuntu/Debian: sudo apt install tmux"; \
		echo "On macOS: brew install tmux"; \
		exit 1; \
	fi
	@tmux new-session -d -s moviepickarr-dev 'bun run --cwd ./web dev'
	@tmux split-window -h 'go run main.go'
	@tmux -2 attach-session -d
