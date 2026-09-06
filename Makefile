# goscape-singleplayer

SHELL := /bin/bash
BIN   := goscape-singleplayer
CMD   := ./cmd/goscape-singleplayer

# rev-225 packs a split layout and compiles wordenc into pack/client/wordenc
# from Content's own wordenc/*.txt sources, so the bundle has no raw/ member
# and the packer needs no --raw-dir. On rev-244+ both are required.
BUNDLE_DIR := internal/content/embedded/bundle
PACK_DIR   := $(BUNDLE_DIR)/pack
# The SoundFont is the one engine-owned blob this revision still copies out of
# the goscape module. It gets its own bundle member, which is why rev-225 needs
# no raw/ to hold it — the member is public/ on every revision branch. Must
# match content.SoundFontName and content.soundFontBundlePath.
SOUNDFONT  := SCC1_Florestan.sf2
PUBLIC_DIR := $(BUNDLE_DIR)/public
# Sibling of $(BUNDLE_DIR), not inside it: the embed directive is
# //go:embed all:bundle, and the all: prefix would sweep a marker placed
# inside bundle/ into the embedded tree and into PACK_DIGEST. Written only
# after a bundle finishes successfully, so build-embedded can tell a
# complete bundle from one left partial by a failed embed-pack.
MARKER := internal/content/embedded/.bundle-complete

CONTENT_REPO   := $(shell sed -n 's/^repo[[:space:]]*=[[:space:]]*//p' content.lock)
CONTENT_BRANCH := $(shell sed -n 's/^branch[[:space:]]*=[[:space:]]*//p' content.lock)
CONTENT_COMMIT := $(shell sed -n 's/^commit[[:space:]]*=[[:space:]]*//p' content.lock)

GIT_REVISION := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_BRANCH   := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
BUILD_TAG    ?= $(shell git describe --tags --exact-match 2>/dev/null)
BUILD_DATE   := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

BPREFIX := github.com/zsrv/goscape-singleplayer/internal/build
CPREFIX := github.com/zsrv/goscape-singleplayer/internal/content

# PACK_DIGEST_CMD is a deterministic digest over the bundle: every file's
# SHA-256 and path, sorted, hashed again.
#
# It is a shell command, not a make variable, on purpose. A $(shell ...) here
# would run when make EXPANDS the recipe, and make expands a recipe in full
# before running any of its lines — so inside embed-pack it evaluated against
# whatever bundle existed BEFORE the rm -rf, reporting an empty digest on a
# first run and the previous run's digest on every one after. Recipes must
# substitute this at run time with $$(...).
# -b matches what the release workflow does: it pins the '*' binary flag in
# the listing lines, which GNU sha256sum emits by default on Windows and
# shasum does not on Linux/macOS. Without it the same bundle gets one digest
# here and a different one in the Windows build job.
PACK_DIGEST_CMD = cd $(BUNDLE_DIR) && find . -type f -print0 \
    | LC_ALL=C sort -z | xargs -0 shasum -a 256 -b | shasum -a 256 -b \
    | cut -d' ' -f1 | sed 's/^/sha256:/'

LDFLAGS = -s -w \
    -X $(BPREFIX).Version=$(BUILD_TAG) \
    -X $(BPREFIX).Revision=$(GIT_REVISION) \
    -X $(BPREFIX).Branch=$(GIT_BRANCH) \
    -X $(BPREFIX).BuildUser=$(shell whoami)@$(shell hostname) \
    -X $(BPREFIX).BuildDate=$(BUILD_DATE)

# Content provenance is stamped only into embedded builds: a plain `make
# build` binary carries no content, so its -version output must not claim a
# repo/branch/commit it doesn't have.
CONTENT_LDFLAGS = \
    -X $(CPREFIX).Repo=$(CONTENT_REPO) \
    -X $(CPREFIX).Branch=$(CONTENT_BRANCH) \
    -X $(CPREFIX).Commit=$(CONTENT_COMMIT)

.PHONY: help build build-embedded embed-pack embed-pack-fixture test clean

help: ## list targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-20s %s\n", $$1, $$2}'

build: ## build without embedded content (the default; needs --cache-dir at runtime)
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) $(CMD)

build-embedded: ## build with the bundle in $(BUNDLE_DIR) embedded
	@test -f $(MARKER) || { echo "no complete bundle at $(BUNDLE_DIR); run 'make embed-pack' first" >&2; exit 1; }
	CGO_ENABLED=1 go build -trimpath -tags embedcache \
	    -ldflags "$(LDFLAGS) $(CONTENT_LDFLAGS) -X $(CPREFIX).PackDigest=$$($(PACK_DIGEST_CMD))" -o $(BIN) $(CMD)

embed-pack: ## pack the pinned Content revision into $(BUNDLE_DIR)
	@test -n "$(CONTENT_COMMIT)" || { echo "content.lock: no commit pinned" >&2; exit 1; }
	rm -f $(MARKER)
	rm -rf $(BUNDLE_DIR)
	mkdir -p $(PACK_DIR) $(PUBLIC_DIR)
# One shell, so the trap can remove the clone on EVERY exit path. Make runs
# each recipe line in its own shell and aborts at the first failure, so a
# trailing `rm -rf` is only reached when nothing went wrong. Keep it one
# unbroken command: make joins the \-continued lines before handing them to
# the shell, so a # comment anywhere inside would swallow everything after it.
#
# One engine-owned blob is copied out of the goscape module alongside the
# packed Content: the SoundFont, from the module root's public/ into the
# bundle's own public/ member. (rev-244+ also copies wordenc; here it is
# compiled into pack/client/wordenc from Content's own sources.) Embedding the SoundFont is what lets a release binary put it
# where the ondemand static root can serve it, instead of every user sourcing
# a 3 MB file by hand to get music.
	set -euo pipefail; \
	WORK=$$(mktemp -d); \
	trap 'rm -rf "$$WORK"' EXIT; \
	git clone --filter=blob:none --no-checkout \
	    https://github.com/$(CONTENT_REPO).git "$$WORK"; \
	git -C "$$WORK" checkout --detach $(CONTENT_COMMIT); \
	GOSCAPE_DIR=$$(go list -m -f '{{.Dir}}' github.com/zsrv/goscape); \
	CGO_ENABLED=0 go run github.com/zsrv/goscape/cmd/goscape-cli pack \
	    --src-dir "$$WORK" --out-dir $(PACK_DIR); \
	install -m 0644 "$$GOSCAPE_DIR"/public/$(SOUNDFONT) $(PUBLIC_DIR)/$(SOUNDFONT)
	touch $(MARKER)
	@echo "bundle ready: $$(du -sh $(BUNDLE_DIR) | cut -f1), digest $$($(PACK_DIGEST_CMD))"

embed-pack-fixture: ## install the tiny CI fixture as the bundle
	rm -f $(MARKER)
	rm -rf $(BUNDLE_DIR)
	mkdir -p $(BUNDLE_DIR)
	cp -R internal/content/testdata/fixturebundle/. $(BUNDLE_DIR)/
	touch $(MARKER)

test: ## run the test suite
	CGO_ENABLED=1 go test ./...

clean: ## remove build output and the generated bundle
	rm -rf $(BIN) $(BUNDLE_DIR) $(MARKER)
