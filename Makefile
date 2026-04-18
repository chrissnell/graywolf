CARGO   ?= cargo
RUSTFLAGS_NATIVE := -C target-cpu=native

# Version comes from the VERSION file (authoritative). Commit hash + dirty
# flag come from git. The two are joined into v<VERSION>-<COMMIT>[-dirty]
# at display time by both the Go and Rust sides, so keep them separate here.
VERSION     ?= $(shell cat VERSION 2>/dev/null || echo dev)
GIT_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_DIRTY   := $(shell git diff-index --quiet HEAD -- 2>/dev/null || echo -dirty)
FULL_COMMIT := $(GIT_COMMIT)$(GIT_DIRTY)

GIT_REMOTE ?= origin

GO_LDFLAGS := -X main.Version=$(VERSION) -X main.GitCommit=$(FULL_COMMIT)

# Subproject directories (see refactor/split-modem-and-app).
MODEM_DIR := graywolf-modem
APP_DIR   := graywolf
WEB_DIR   := $(APP_DIR)/web

MANIFEST := --manifest-path $(MODEM_DIR)/Cargo.toml

# Rust picks up these env vars in build.rs.
CARGO_ENV := GRAYWOLF_VERSION="$(VERSION)" GRAYWOLF_GIT_COMMIT="$(FULL_COMMIT)"

# swag: OpenAPI spec generator. Installed locally under scratch/bin so
# the repo doesn't leak a runtime dep into go.mod. The CLI is a dev
# tool only — nothing in the compiled graywolf binary depends on it.
#
# Install (one-time):
#   GOBIN=$(pwd)/scratch/bin go install github.com/swaggo/swag/cmd/swag@latest
#
# The `docs` target below refuses to run if swag isn't on PATH or in
# scratch/bin, with a pointer to this comment.
SWAG ?= $(abspath scratch/bin/swag)

# Generated OpenAPI spec lives inside the Go module so `swag init` can
# emit its docs.go next to the source it describes. The rendered
# Swagger UI page and its sibling openapi.{json,yaml} live in the
# handbook tree so they ship with the rest of the static docs.
DOCS_GEN_DIR     := $(APP_DIR)/pkg/webapi/docs/gen
DOCS_HANDBOOK    := docs/handbook
SWAGGER_UI_VENDOR := $(DOCS_HANDBOOK)/vendor/swagger-ui

# Generated artifacts that must stay in sync with the Go source (swag
# annotations) and the rendered swagger spec. The release bump targets
# regenerate these and stage them so a release commit is always self-
# consistent — drift in these files is what the CI docs-check and
# api-client-check guards catch.
GENERATED_SPEC_FILES := $(DOCS_GEN_DIR)/swagger.json $(DOCS_GEN_DIR)/swagger.yaml $(WEB_DIR)/src/api/generated/api.d.ts

.PHONY: all build release test bench clean clean-web distclean check fmt lint doc run-bench proto go-build go-test go-fuzz web graywolf version bump-minor bump-point bump-beta handbook-sync docs docs-api-html docs-check docs-lint api-client api-client-check

all: release web
	mkdir -p bin
	cp target/release/graywolf-modem bin/
	cd $(APP_DIR) && go build -ldflags="$(GO_LDFLAGS)" -o ../bin/graywolf ./cmd/graywolf/

build:
	$(CARGO_ENV) $(CARGO) build $(MANIFEST)

release:
	$(CARGO_ENV) RUSTFLAGS="$(RUSTFLAGS_NATIVE)" $(CARGO) build --release $(MANIFEST)

check:
	$(CARGO) check $(MANIFEST)

test:
	$(CARGO) test $(MANIFEST)

bench:
	$(CARGO) bench $(MANIFEST)

fmt:
	$(CARGO) fmt $(MANIFEST)

lint: fmt
	$(CARGO) clippy $(MANIFEST) -- -D warnings

doc:
	$(CARGO) doc --no-deps --open $(MANIFEST)

# clean: remove build artifacts (Rust target/, Go binaries, web node_modules
# and dist). Leaves committed generated files alone — use `make distclean`
# for those.
clean: clean-web
	$(CARGO) clean $(MANIFEST)
	rm -rf bin
	rm -f $(APP_DIR)/graywolf

# clean-web: force-reinstall node_modules on next build. Useful after pulling
# a branch that regenerated package-lock.json on a different OS (npm CLI
# bug #4828 leaves Rollup's platform-specific native binary uninstalled).
clean-web:
	rm -rf $(WEB_DIR)/node_modules $(WEB_DIR)/dist

# distclean: clean + wipe committed generated artifacts. Use this only when
# you intend to regenerate the OpenAPI spec, Swagger UI page, and
# TypeScript client from scratch.
distclean: clean
	rm -rf $(DOCS_GEN_DIR)
	rm -f $(DOCS_HANDBOOK)/api.html $(DOCS_HANDBOOK)/openapi.json $(DOCS_HANDBOOK)/openapi.yaml
	rm -rf $(WEB_DIR)/src/api/generated

# Regenerate Go protobuf bindings from proto/graywolf.proto. Requires protoc
# and protoc-gen-go on PATH. Install the latter with:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
proto:
	cd $(APP_DIR) && protoc \
		--proto_path=../proto \
		--go_out=. --go_opt=module=github.com/chrissnell/graywolf \
		../proto/graywolf.proto

NODE_STAMP := $(WEB_DIR)/node_modules/.stamp-$(shell uname -s)-$(shell uname -m)

# `npm install` (not `npm ci`) because npm CLI bug #4828 drops platform-specific
# optional deps from the lockfile when it's regenerated on a different OS —
# `npm ci` then refuses to fetch them, breaking Rollup on Pi/Linux builds
# after a macOS-originated lockfile change. `npm install` tolerates the drift
# and fetches what the current platform needs.
$(NODE_STAMP): $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	rm -rf $(WEB_DIR)/node_modules
	cd $(WEB_DIR) && npm install --no-audit --no-fund
	@touch $@

web: $(NODE_STAMP)
	cd $(WEB_DIR) && npm run build

go-build:
	cd $(APP_DIR) && go build -ldflags="$(GO_LDFLAGS)" ./...

go-test: docs-check api-client-check
	cd $(APP_DIR) && go test -race ./...

# Run Go fuzz targets for a bounded duration. Override FUZZTIME to change it.
FUZZTIME ?= 60s
go-fuzz:
	cd $(APP_DIR) && go test -run=^$$ -fuzz=FuzzDecode -fuzztime=$(FUZZTIME) ./pkg/ax25/
	cd $(APP_DIR) && go test -run=^$$ -fuzz=FuzzParseInfo -fuzztime=$(FUZZTIME) ./pkg/aprs/

# Build everything: Rust release, Svelte UI, Go binary.
# Also stages graywolf-modem into bin/ so ./bin/graywolf can find it via
# the next-to-executable lookup.
graywolf: release web
	mkdir -p bin
	cp target/release/graywolf-modem bin/
	cd $(APP_DIR) && go build -ldflags="$(GO_LDFLAGS)" -o ../bin/graywolf ./cmd/graywolf/

run-bench: release
	@echo "Usage: make run-bench FLAC=<file> [ITER=5]"
	@test -n "$(FLAC)" || { echo "error: FLAC not set"; exit 1; }
	$(MODEM_DIR)/bench.sh "$(FLAC)" "$(or $(ITER),5)"

version:
	@echo "v$(VERSION)-$(FULL_COMMIT)"

bump-minor:
	@$(MAKE) --no-print-directory api-client
	@echo "Current version: $(VERSION)"
	$(eval NEW := $(shell echo $(VERSION) | awk -F. '{printf "%d.%d.0", $$1, $$2+1}'))
	@echo "$(NEW)" > VERSION
	@sed -i.bak 's/^version = ".*"/version = "$(NEW)"/' $(MODEM_DIR)/Cargo.toml && rm $(MODEM_DIR)/Cargo.toml.bak
	@sed -i.bak 's/^pkgver=.*/pkgver=$(NEW)/' packaging/aur/PKGBUILD && rm packaging/aur/PKGBUILD.bak
	@sed -i.bak 's/pkgver = .*/pkgver = $(NEW)/' packaging/aur/.SRCINFO && rm packaging/aur/.SRCINFO.bak
	@sed -i.bak 's|source = graywolf-.*\.tar\.gz::.*|source = graywolf-$(NEW).tar.gz::https://github.com/chrissnell/graywolf/archive/v$(NEW).tar.gz|' packaging/aur/.SRCINFO && rm packaging/aur/.SRCINFO.bak
	@sed -i.bak 's|v[0-9]*\.[0-9]*\.[0-9]*-abc1234|v$(NEW)-abc1234|' docs/handbook/installation.html && rm docs/handbook/installation.html.bak
	$(CARGO) update $(MANIFEST)
	@echo "New version: $(NEW)"
	git add VERSION $(MODEM_DIR)/Cargo.toml Cargo.lock packaging/aur/PKGBUILD packaging/aur/.SRCINFO docs/handbook/installation.html $(GENERATED_SPEC_FILES)
	git commit -m "Release v$(NEW)"
	git tag "v$(NEW)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "v$(NEW)"

bump-point:
	@$(MAKE) --no-print-directory api-client
	@echo "Current version: $(VERSION)"
	$(eval NEW := $(shell echo $(VERSION) | awk -F. '{printf "%d.%d.%d", $$1, $$2, $$3+1}'))
	@echo "$(NEW)" > VERSION
	@sed -i.bak 's/^version = ".*"/version = "$(NEW)"/' $(MODEM_DIR)/Cargo.toml && rm $(MODEM_DIR)/Cargo.toml.bak
	@sed -i.bak 's/^pkgver=.*/pkgver=$(NEW)/' packaging/aur/PKGBUILD && rm packaging/aur/PKGBUILD.bak
	@sed -i.bak 's/pkgver = .*/pkgver = $(NEW)/' packaging/aur/.SRCINFO && rm packaging/aur/.SRCINFO.bak
	@sed -i.bak 's|source = graywolf-.*\.tar\.gz::.*|source = graywolf-$(NEW).tar.gz::https://github.com/chrissnell/graywolf/archive/v$(NEW).tar.gz|' packaging/aur/.SRCINFO && rm packaging/aur/.SRCINFO.bak
	@sed -i.bak 's|v[0-9]*\.[0-9]*\.[0-9]*-abc1234|v$(NEW)-abc1234|' docs/handbook/installation.html && rm docs/handbook/installation.html.bak
	$(CARGO) update $(MANIFEST)
	@echo "New version: $(NEW)"
	git add VERSION $(MODEM_DIR)/Cargo.toml Cargo.lock packaging/aur/PKGBUILD packaging/aur/.SRCINFO docs/handbook/installation.html $(GENERATED_SPEC_FILES)
	git commit -m "Release v$(NEW)"
	git tag "v$(NEW)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "v$(NEW)"

bump-beta:
	@echo "Current version: $(VERSION)"
	$(eval EXISTING_BETA := $(shell git tag -l "v$(VERSION)-beta.*" | sed 's/.*beta\.//' | sort -n | tail -1))
	$(eval NEW := $(if $(EXISTING_BETA),$(VERSION),$(shell echo $(VERSION) | awk -F. '{printf "%d.%d.%d", $$1, $$2, $$3+1}')))
	$(eval BETA_N := $(shell git tag -l "v$(NEW)-beta.*" | sed 's/.*beta\.//' | sort -n | tail -1))
	$(eval BETA_NEXT := $(shell echo $$(( $(if $(BETA_N),$(BETA_N),0) + 1 ))))
	$(eval BETA_TAG := v$(NEW)-beta.$(BETA_NEXT))
	@echo "$(NEW)" > VERSION
	@sed -i.bak 's/^version = ".*"/version = "$(NEW)"/' $(MODEM_DIR)/Cargo.toml && rm $(MODEM_DIR)/Cargo.toml.bak
	$(CARGO) update $(MANIFEST)
	@echo "Beta release: $(BETA_TAG)"
	git add VERSION $(MODEM_DIR)/Cargo.toml Cargo.lock
	git diff --cached --quiet || git commit -m "Beta $(BETA_TAG)"
	git tag "$(BETA_TAG)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "$(BETA_TAG)"

handbook-sync:
	rsync -av --delete docs/handbook/ /Volumes/NFS/static-sites/chrissnell.com/software/graywolf

# --- OpenAPI documentation pipeline --------------------------------------
#
# `make docs`          regenerate the committed OpenAPI spec.
# `make docs-api-html` render the static Swagger UI page.
# `make docs-check`    fail if the committed spec is out of date (run
#                      as part of `go-test`).
# `make docs-lint`     fail if any @ID annotation isn't declared as a
#                      constant in pkg/webapi/docs/op_ids.go.
#
# See the SWAG variable comment near the top of this file for install
# instructions. swag v1.16.x emits Swagger 2.0 (OpenAPI 2.0); Swagger
# UI and every known OpenAPI code-generator accept that shape, so it's
# what we commit. swag v2.0.0-rc5 was evaluated (see the phase-webapi-
# followup-9 handoff): it produces a broken `oneOf` wrapper around
# every requestBody in `--v3.1` mode, still silently drops
# `@tag.name`/`@tag.description` general-info directives, and has no
# stable release — so we stay on v1.16.x and post-process the spec to
# inject a curated `tags:` ordering (see pkg/webapi/docs/cmd/tagify).

# --outputTypes json,yaml omits the docs.go register file. We don't
# serve the spec from the daemon, and keeping docs.go out of the tree
# means swag never leaks into go.mod as a runtime dependency.
SWAG_INIT := $(SWAG) init -g server.go --dir pkg/webapi,pkg/modembridge,pkg/webauth --packageName gen --outputTypes json,yaml --parseDependency --parseInternal --quiet

# tagify post-processes the swag output to inject the curated
# top-level tag ordering. Runs via `go run` so no binary to install.
TAGIFY := go run ./pkg/webapi/docs/cmd/tagify

docs:
	@test -x "$(SWAG)" || { echo "swag not found at $(SWAG). See SWAG comment in Makefile."; exit 1; }
	cd $(APP_DIR) && $(SWAG_INIT) -o pkg/webapi/docs/gen
	cd $(APP_DIR) && $(TAGIFY) --json pkg/webapi/docs/gen/swagger.json --yaml pkg/webapi/docs/gen/swagger.yaml

docs-api-html: docs
	@test -d $(SWAGGER_UI_VENDOR) || { echo "missing $(SWAGGER_UI_VENDOR); vendor Swagger UI dist files first"; exit 1; }
	@test -f $(DOCS_HANDBOOK)/api.html || { echo "missing $(DOCS_HANDBOOK)/api.html; copy from a known-good checkout or regenerate"; exit 1; }
	cp $(DOCS_GEN_DIR)/swagger.json $(DOCS_HANDBOOK)/openapi.json
	cp $(DOCS_GEN_DIR)/swagger.yaml $(DOCS_HANDBOOK)/openapi.yaml
	@echo "rendered $(DOCS_HANDBOOK)/api.html (uses sibling openapi.json)"

docs-check:
	@test -x "$(SWAG)" || { echo "swag not found at $(SWAG); cannot verify docs."; echo "See the SWAG comment in Makefile for install instructions."; exit 1; }
	@tmpdir=$$(mktemp -d 2>/dev/null || mktemp -d -t docs-check); \
		trap 'rm -rf "$$tmpdir"' EXIT; \
		cd $(APP_DIR) && $(SWAG_INIT) -o "$$tmpdir" >/dev/null \
			&& $(TAGIFY) --strict --json "$$tmpdir/swagger.json" --yaml "$$tmpdir/swagger.yaml" >/dev/null; \
		for f in swagger.json swagger.yaml; do \
			if ! diff -q "$$tmpdir/$$f" pkg/webapi/docs/gen/$$f >/dev/null 2>&1; then \
				echo "docs-check: $$f drift detected. Run 'make docs' and commit the result."; \
				diff -u pkg/webapi/docs/gen/$$f "$$tmpdir/$$f" | head -40; \
				exit 1; \
			fi; \
		done; \
		echo "docs-check: generated spec matches committed copy."

docs-lint:
	cd $(APP_DIR) && go run ./pkg/webapi/docs/cmd/idlint

# --- OpenAPI TypeScript client -------------------------------------------
#
# `make api-client`       regenerate graywolf/web/src/api/generated/api.d.ts
#                         from the committed Swagger 2.0 spec. The generator
#                         lives in graywolf/web/scripts/generate-api.mjs and
#                         is driven by `npm run api:generate`.
# `make api-client-check` regenerate into a scratch dir and diff against the
#                         committed file. Mirrors docs-check. Wired into
#                         go-test so PRs that touch @ID-bearing handlers
#                         without regenerating the client fail CI.
#
# The client itself is library code committed under
# graywolf/web/src/api/generated/. The hand-written wrapper at
# graywolf/web/src/api/client.ts is the only non-generated file in that
# tree. Existing .svelte fetch calls are NOT migrated — that's deferred
# to a separate initiative.

api-client: docs $(NODE_STAMP)
	cd $(WEB_DIR) && npm run api:generate

api-client-check: $(NODE_STAMP)
	@tmpdir=$$(mktemp -d 2>/dev/null || mktemp -d -t api-client-check); \
		trap 'rm -rf "$$tmpdir"' EXIT; \
		out="$$tmpdir/api.d.ts"; \
		( cd $(WEB_DIR) && GRAYWOLF_API_OUT="$$out" node scripts/generate-api.mjs --strict >/dev/null ) || exit 1; \
		if ! diff -q "$$out" $(WEB_DIR)/src/api/generated/api.d.ts >/dev/null 2>&1; then \
			echo "api-client-check: api.d.ts drift detected. Run 'make api-client' and commit the result."; \
			diff -u $(WEB_DIR)/src/api/generated/api.d.ts "$$out" | head -40; \
			exit 1; \
		fi; \
		echo "api-client-check: generated client matches committed copy."
