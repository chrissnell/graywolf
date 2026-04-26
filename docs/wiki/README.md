# Graywolf wiki

Index for cross-system questions about this repo. The wiki *navigates*;
the code, the operator handbook, and the plan files keep their existing
roles.

## When to use this wiki vs. other docs

| If you want | Look at |
|---|---|
| Where pieces connect, what runs where, what to touch when changing X | This wiki |
| Operator-facing setup, configuration, hardware, REST API reference | [`../handbook/`](../handbook/) (HTML, also at chrissnell.com/software/graywolf/) |
| Feature overview, performance numbers, project pitch | [`../../README.md`](../../README.md) |
| Why a subsystem was built that way (design rationale) | `.context/*.md` plan files |
| The Go<->Rust IPC wire format | [`../../proto/graywolf.proto`](../../proto/graywolf.proto) |
| What a single function does | The code |

## Pages

- [`system-topology.md`](system-topology.md) -- processes, ports, persistence, hardware surface, deployment targets.
- [`code-map.md`](code-map.md) -- feature/concern -> file lookup, table per component.
- [`build-pipelines.md`](build-pipelines.md) -- how each artifact is produced (Go binary, Rust modem, web UI, proto codegen, OpenAPI, goreleaser, packages, in-app release notes).
- [`invariants.md`](invariants.md) -- cross-cutting "if X then also Y" rules with reasons.
- [`glossary.md`](glossary.md) -- domain terms as graywolf uses them, with pointers.

## Maintenance

A stale wiki is worse than none, because it gets trusted. If you grep
for something this wiki should have answered, add it. If the wiki
disagrees with the code, fix the wiki in the same change. The triggers
are spelled out in [`../../CLAUDE.md`](../../CLAUDE.md).
