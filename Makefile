.PHONY: help build install test vet check website serve clean bump

.DEFAULT_GOAL := help

BIN     := bulle
PREFIX  ?= $(HOME)/.local
VERSION ?= dev
LDFLAGS = -X github.com/vincentarelbundock/bulle/internal/app.Version=$(VERSION)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "}; /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Compile the bulle binary
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/bulle

install: build ## Install bulle to $(DESTDIR)$(PREFIX)/bin
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 755 $(BIN) "$(DESTDIR)$(PREFIX)/bin/$(BIN)"

test: ## Run all tests
	go test ./...

vet: ## Run go vet
	go vet ./...

check: vet test ## Run vet and tests

website: ## Render docs-src/ into docs/
	go run ./cmd/bulle-docs
	@if [ -d .cache ]; then chmod -R u+w .cache 2>/dev/null || true; fi
	uv run zensical build --clean

serve: ## Build and serve the website at http://localhost:8000
	uv run zensical serve

bump: ## Release VERSION=x.y.z: update VERSION file, commit, and tag
	@if [ "$(origin VERSION)" != "command line" ]; then \
		echo "usage: make bump VERSION=x.y.z"; \
		exit 2; \
	fi
	@set -eu; \
	version="$(VERSION)"; \
	version="$${version#v}"; \
	tag="v$$version"; \
	if ! printf '%s\n' "$$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$'; then \
		echo "VERSION must look like x.y.z or vx.y.z"; \
		exit 2; \
	fi; \
	if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "working tree has uncommitted changes; commit or stash them first"; \
		exit 1; \
	fi; \
	if git rev-parse "$$tag" >/dev/null 2>&1; then \
		echo "tag $$tag already exists"; \
		exit 1; \
	fi; \
	printf '%s\n' "$$version" > VERSION; \
	git add VERSION; \
	git commit -m "Bump version to $$tag"; \
	git tag -a "$$tag" -m "$$tag"; \
	echo "Created commit and tag $$tag. Push with: git push origin HEAD $$tag"

clean: ## Remove build artifacts
	rm -f $(BIN)
