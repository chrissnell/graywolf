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

.PHONY: all build release test bench clean check fmt lint doc run-bench proto go-build go-test go-fuzz web graywolf version bump-minor bump-point bump-beta

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

clean:
	$(CARGO) clean $(MANIFEST)

# Regenerate Go protobuf bindings from proto/graywolf.proto. Requires protoc
# and protoc-gen-go on PATH. Install the latter with:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
proto:
	cd $(APP_DIR) && protoc \
		--proto_path=../proto \
		--go_out=. --go_opt=module=github.com/chrissnell/graywolf \
		../proto/graywolf.proto

NODE_STAMP := $(WEB_DIR)/node_modules/.stamp-$(shell uname -s)-$(shell uname -m)

$(NODE_STAMP): $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	rm -rf $(WEB_DIR)/node_modules
	cd $(WEB_DIR) && npm ci
	@touch $@

web: $(NODE_STAMP)
	cd $(WEB_DIR) && npm run build

go-build:
	cd $(APP_DIR) && go build -ldflags="$(GO_LDFLAGS)" ./...

go-test:
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
	@echo "Current version: $(VERSION)"
	$(eval NEW := $(shell echo $(VERSION) | awk -F. '{printf "%d.%d.0", $$1, $$2+1}'))
	@echo "$(NEW)" > VERSION
	@sed -i '' 's/^version = ".*"/version = "$(NEW)"/' $(MODEM_DIR)/Cargo.toml
	@sed -i '' 's/^pkgver=.*/pkgver=$(NEW)/' packaging/aur/PKGBUILD
	@sed -i '' 's/pkgver = .*/pkgver = $(NEW)/' packaging/aur/.SRCINFO
	@sed -i '' 's|source = graywolf-.*\.tar\.gz::.*|source = graywolf-$(NEW).tar.gz::https://github.com/chrissnell/graywolf/archive/v$(NEW).tar.gz|' packaging/aur/.SRCINFO
	@sed -i '' 's|v[0-9]*\.[0-9]*\.[0-9]*-abc1234|v$(NEW)-abc1234|' docs/handbook/installation.html
	$(CARGO) update $(MANIFEST)
	@echo "New version: $(NEW)"
	git add VERSION $(MODEM_DIR)/Cargo.toml Cargo.lock packaging/aur/PKGBUILD packaging/aur/.SRCINFO docs/handbook/installation.html
	git commit -m "Release v$(NEW)"
	git tag "v$(NEW)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "v$(NEW)"

bump-point:
	@echo "Current version: $(VERSION)"
	$(eval NEW := $(shell echo $(VERSION) | awk -F. '{printf "%d.%d.%d", $$1, $$2, $$3+1}'))
	@echo "$(NEW)" > VERSION
	@sed -i '' 's/^version = ".*"/version = "$(NEW)"/' $(MODEM_DIR)/Cargo.toml
	@sed -i '' 's/^pkgver=.*/pkgver=$(NEW)/' packaging/aur/PKGBUILD
	@sed -i '' 's/pkgver = .*/pkgver = $(NEW)/' packaging/aur/.SRCINFO
	@sed -i '' 's|source = graywolf-.*\.tar\.gz::.*|source = graywolf-$(NEW).tar.gz::https://github.com/chrissnell/graywolf/archive/v$(NEW).tar.gz|' packaging/aur/.SRCINFO
	@sed -i '' 's|v[0-9]*\.[0-9]*\.[0-9]*-abc1234|v$(NEW)-abc1234|' docs/handbook/installation.html
	$(CARGO) update $(MANIFEST)
	@echo "New version: $(NEW)"
	git add VERSION $(MODEM_DIR)/Cargo.toml Cargo.lock packaging/aur/PKGBUILD packaging/aur/.SRCINFO docs/handbook/installation.html
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
	@sed -i '' 's/^version = ".*"/version = "$(NEW)"/' $(MODEM_DIR)/Cargo.toml
	$(CARGO) update $(MANIFEST)
	@echo "Beta release: $(BETA_TAG)"
	git add VERSION $(MODEM_DIR)/Cargo.toml Cargo.lock
	git diff --cached --quiet || git commit -m "Beta $(BETA_TAG)"
	git tag "$(BETA_TAG)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "$(BETA_TAG)"
