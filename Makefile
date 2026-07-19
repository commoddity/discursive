# Discursive — common targets
# Prefer: make verify  (lint + test + build)

GO ?= go
GOLANGCI_LINT ?= golangci-lint

# --- Version ---
# Increments the patch version, creates a tag, and pushes it.
# The GitHub Actions release-binaries workflow triggers on the pushed tag.
# Requires: clean working tree on main, GH_PAT secret set in repo settings.
VERSION_FILE := VERSION

_current_version = $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.0.0")

.PHONY: help lint test build verify vet fmt install-hooks
.PHONY: release-patch release-minor _tag_and_push

help:
	@echo "Targets:"
	@echo "  make verify        - lint + test + build (default gate)"
	@echo "  make lint          - golangci-lint run"
	@echo "  make test          - go test ./..."
	@echo "  make build         - go build ./..."
	@echo "  make vet           - go vet ./..."
	@echo "  make fmt           - gofmt -w on packages"
	@echo "  make install-hooks - lefthook install (pre-commit → make verify)"
	@echo "  make release-patch - bump patch, tag, push → triggers CI release"
	@echo "  make release-minor - bump minor, tag, push → triggers CI release"

lint:
	$(GOLANGCI_LINT) run ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

build:
	$(GO) build ./...

fmt:
	$(GO) fmt ./...

verify: lint test build

install-hooks:
	lefthook install

# --- Semver helpers ---

_tag_and_push:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "ERROR: working tree is dirty. Commit or stash changes first."; \
		exit 1; \
	fi
	@echo "Current version: $(_current_version)"
	@echo "$(NEW_VERSION)" > $(VERSION_FILE)
	git add $(VERSION_FILE)
	git commit -m "Release v$(NEW_VERSION)"
	git tag -a "v$(NEW_VERSION)" -m "Release v$(NEW_VERSION)"
	git push origin HEAD
	git push origin "v$(NEW_VERSION)"
	@echo "Pushed tag v$(NEW_VERSION). CI will build and publish the release."

release-patch: _current_version = $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.0.0")
release-patch:
	@$(MAKE) --no-print-directory NEW_VERSION=$$(echo $(_current_version) | awk -F. '{printf "%d.%d.%d", $$1, $$2, $$3+1}') _tag_and_push

release-minor: _current_version = $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.0.0")
release-minor:
	@$(MAKE) --no-print-directory NEW_VERSION=$$(echo $(_current_version) | awk -F. '{printf "%d.%d.0", $$1, $$2+1}') _tag_and_push
