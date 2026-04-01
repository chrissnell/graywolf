CARGO   ?= cargo
RUSTFLAGS_NATIVE := -C target-cpu=native

.PHONY: all build release test bench clean check fmt lint doc run-bench

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

run-bench: release
	@echo "Usage: make run-bench FLAC=<file> [ITER=5]"
	@test -n "$(FLAC)" || { echo "error: FLAC not set"; exit 1; }
	./bench.sh "$(FLAC)" "$(or $(ITER),5)"
