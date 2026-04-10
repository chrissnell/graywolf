CARGO   ?= cargo
RUSTFLAGS_NATIVE := -C target-cpu=native
# Version is derived from the latest git tag (e.g. v0.7.9 → 0.7.9). Falls back
# to the VERSION file for source tarballs without git history.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || cat VERSION 2>/dev/null || echo dev)
GIT_REMOTE ?= origin

GO_LDFLAGS := -X main.Version=$(VERSION)

# Subproject directories (see refactor/split-modem-and-app).
MODEM_DIR := graywolf-modem
APP_DIR   := graywolf
WEB_DIR   := $(APP_DIR)/web

MANIFEST := --manifest-path $(MODEM_DIR)/Cargo.toml

.PHONY: all build release test bench clean check fmt lint doc run-bench proto go-build go-test web graywolf version bump-minor bump-point

all: release web
	mkdir -p bin && cd $(APP_DIR) && go build -ldflags="$(GO_LDFLAGS)" -o ../bin/graywolf ./cmd/graywolf/

build:
	GRAYWOLF_VERSION="$(VERSION)" $(CARGO) build $(MANIFEST)

release:
	GRAYWOLF_VERSION="$(VERSION)" RUSTFLAGS="$(RUSTFLAGS_NATIVE)" $(CARGO) build --release $(MANIFEST)

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

web:
	cd $(WEB_DIR) && npm run build

go-build:
	cd $(APP_DIR) && go build -ldflags="$(GO_LDFLAGS)" ./...

go-test:
	cd $(APP_DIR) && go test ./...

# Build everything: Rust release, Svelte UI, Go binary
graywolf: release web
	mkdir -p bin && cd $(APP_DIR) && go build -ldflags="$(GO_LDFLAGS)" -o ../bin/graywolf ./cmd/graywolf/

run-bench: release
	@echo "Usage: make run-bench FLAC=<file> [ITER=5]"
	@test -n "$(FLAC)" || { echo "error: FLAC not set"; exit 1; }
	$(MODEM_DIR)/bench.sh "$(FLAC)" "$(or $(ITER),5)"

version:
	@echo "v$(VERSION)"

bump-minor:
	@echo "Current version: $(VERSION)"
	$(eval NEW := $(shell echo $(VERSION) | awk -F. '{printf "%d.%d.0", $$1, $$2+1}'))
	@echo "$(NEW)" > VERSION
	@sed -i '' 's/^version = ".*"/version = "$(NEW)"/' $(MODEM_DIR)/Cargo.toml
	@echo "New version: $(NEW)"
	git add VERSION $(MODEM_DIR)/Cargo.toml
	git commit -m "Release v$(NEW)"
	git tag "v$(NEW)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "v$(NEW)"

bump-point:
	@echo "Current version: $(VERSION)"
	$(eval NEW := $(shell echo $(VERSION) | awk -F. '{printf "%d.%d.%d", $$1, $$2, $$3+1}'))
	@echo "$(NEW)" > VERSION
	@sed -i '' 's/^version = ".*"/version = "$(NEW)"/' $(MODEM_DIR)/Cargo.toml
	@echo "New version: $(NEW)"
	git add VERSION $(MODEM_DIR)/Cargo.toml
	git commit -m "Release v$(NEW)"
	git tag "v$(NEW)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "v$(NEW)"
