.PHONY: help build completions install test vet check website serve clean bump

.DEFAULT_GOAL := help

BIN     := bulle
PREFIX  ?= $(HOME)/.local
CALEPIN ?= calepin
# DOCS_SRC is the hand-authored website (pages, calepin.toml, assets); SITE_DIR
# is what Calepin writes from it. Nothing in SITE_DIR is edited directly.
DOCS_SRC := docs-src
SITE_DIR := docs
VERSION ?= dev
LDFLAGS = -X github.com/vincentarelbundock/bulle/internal/app.Version=$(VERSION)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "}; /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Compile the bulle binary
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/bulle

completions: ## Generate shell completion scripts into completions/
	./scripts/generate-completions.sh

install: build completions ## Install bulle and completions to $(DESTDIR)$(PREFIX)
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 755 $(BIN) "$(DESTDIR)$(PREFIX)/bin/$(BIN)"
	install -d "$(DESTDIR)$(PREFIX)/share/bash-completion/completions"
	install -m 644 completions/bulle.bash "$(DESTDIR)$(PREFIX)/share/bash-completion/completions/$(BIN)"
	install -d "$(DESTDIR)$(PREFIX)/share/zsh/site-functions"
	install -m 644 completions/_bulle "$(DESTDIR)$(PREFIX)/share/zsh/site-functions/_bulle"
	install -d "$(DESTDIR)$(PREFIX)/share/fish/vendor_completions.d"
	install -m 644 completions/bulle.fish "$(DESTDIR)$(PREFIX)/share/fish/vendor_completions.d/bulle.fish"

test: ## Run all tests
	go test ./...

vet: ## Run go vet
	go vet ./...

check: vet test ## Run vet and tests

website: ## Render docs-src/ into docs/ with Calepin
	go run ./cmd/bulle-docs
# Every byte under docs/ is generated, and Calepin refuses to overwrite an
# output directory it does not recognise, so start from nothing.
	rm -rf $(SITE_DIR)
	$(CALEPIN) compile $(DOCS_SRC) $(SITE_DIR)
# GitHub Pages runs Jekyll unless this file is present, and Jekyll drops every
# path beginning with an underscore, including the assets Calepin emits.
	@touch $(SITE_DIR)/.nojekyll

serve: website ## Build and serve the website at http://localhost:8000
	$(CALEPIN) serve $(SITE_DIR)

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
	rm -rf completions
