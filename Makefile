CARGO   ?= cargo
RUSTFLAGS_NATIVE := -C target-cpu=native

.PHONY: all build release test bench clean check fmt lint doc run-bench proto go-build go-test

all: build

build:
	$(CARGO) build

release:
	RUSTFLAGS="$(RUSTFLAGS_NATIVE)" $(CARGO) build --release

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
		--go_opt=Mproto/graywolf.proto=github.com/chrissnell/graywolf/pkg/ipcproto \
		proto/graywolf.proto

go-build:
	go build ./...

go-test:
	go test ./...

run-bench: release
	@echo "Usage: make run-bench FLAC=<file> [ITER=5]"
	@test -n "$(FLAC)" || { echo "error: FLAC not set"; exit 1; }
	./bench.sh "$(FLAC)" "$(or $(ITER),5)"
