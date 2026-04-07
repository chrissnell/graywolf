CARGO   ?= cargo
RUSTFLAGS_NATIVE := -C target-cpu=native
VERSION ?= $(shell cat VERSION 2>/dev/null || echo dev)
GIT_REMOTE ?= origin

GO_LDFLAGS := -X main.Version=$(VERSION)

.PHONY: all build release test bench clean check fmt lint doc run-bench proto go-build go-test web graywolf version bump-minor bump-point

all: release web
	go build -ldflags="$(GO_LDFLAGS)" -o graywolf ./cmd/graywolf/

build:
	GRAYWOLF_VERSION="$(VERSION)" $(CARGO) build

release:
	GRAYWOLF_VERSION="$(VERSION)" RUSTFLAGS="$(RUSTFLAGS_NATIVE)" $(CARGO) build --release

check:
	$(CARGO) check

test:
	$(CARGO) test

bench:
	$(CARGO) bench

fmt:
	$(CARGO) fmt

lint: fmt
	$(CARGO) clippy -- -D warnings

doc:
	$(CARGO) doc --no-deps --open

clean:
	$(CARGO) clean

# Regenerate Go protobuf bindings from proto/graywolf.proto. Requires protoc
# and protoc-gen-go on PATH. Install the latter with:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
proto:
	protoc --go_out=. --go_opt=module=github.com/chrissnell/graywolf \
		proto/graywolf.proto

web:
	cd web && npm run build

go-build:
	go build -ldflags="$(GO_LDFLAGS)" ./...

go-test:
	go test ./...

# Build everything: Rust release, Svelte UI, Go binary
graywolf: release web
	go build -ldflags="$(GO_LDFLAGS)" -o graywolf ./cmd/graywolf/

run-bench: release
	@echo "Usage: make run-bench FLAC=<file> [ITER=5]"
	@test -n "$(FLAC)" || { echo "error: FLAC not set"; exit 1; }
	./bench.sh "$(FLAC)" "$(or $(ITER),5)"

version:
	@echo "v$(VERSION)"

bump-minor:
	@echo "Current version: $(VERSION)"
	$(eval NEW := $(shell echo $(VERSION) | awk -F. '{printf "%d.%d.0", $$1, $$2+1}'))
	@echo "$(NEW)" > VERSION
	@sed -i '' 's/^version = ".*"/version = "$(NEW)"/' Cargo.toml
	@echo "New version: $(NEW)"
	git add VERSION Cargo.toml
	git commit -m "Release v$(NEW)"
	git tag "v$(NEW)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "v$(NEW)"

bump-point:
	@echo "Current version: $(VERSION)"
	$(eval NEW := $(shell echo $(VERSION) | awk -F. '{printf "%d.%d.%d", $$1, $$2, $$3+1}'))
	@echo "$(NEW)" > VERSION
	@sed -i '' 's/^version = ".*"/version = "$(NEW)"/' Cargo.toml
	@echo "New version: $(NEW)"
	git add VERSION Cargo.toml
	git commit -m "Release v$(NEW)"
	git tag "v$(NEW)"
	git push $(GIT_REMOTE) && git push $(GIT_REMOTE) "v$(NEW)"
