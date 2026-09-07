Run all Go unit tests before doing anything, to establish a baseline. Your job
CANNOT be considered complete if you do not run tests after completing the work.
Therefore it is imperative to verify that tests are working before you begin.

If there is an issue running tests at all (i.e. missing modules that can't be loaded),
STOP.
If there are broken tests, begin by fixing those broken tests.

DO NOT consider the job to be complete until unit tests have been updated
and `go test ./...` completes successfully.

If you have been asked to add new functionality, you must write unit tests
that verify the new functionality.
If you have been asked to perform a refactoring, you need only change unit
tests to the extent needed to ensure all functionality has good tests.

## Session Worklog protocol

All agents doing non-trivial repository work MUST follow the Session Worklog
protocol in `/Users/tyler/.agents/skills/session-worklog/SKILL.md`.

- Create or continue a task worktree before writing operating artifacts.
- Keep the tracked session worklog under `ephemeral/worklog/` unless a more
  specific repository policy supersedes that path.
- Keep review prompts, review results, scratch notes, source captures, generated
  packets, and all other temporary or raw session artifacts under `ephemeral/`.
- NEVER place ephemeral or raw session artifacts in `docs/`. Only polished,
  durable project documentation belongs there.
- Update the worklog with commands, decisions, corrections, proof, and final
  branch or review state before closeout. Do not delete an active worklog.

## Project Overview

polytype is a type projection tool. It lowers Go types statically into one closed type grammar (`typegrammar`) and projects that grammar into JSON Schema for LLM function calling (the CLI's always-on output), validation methods, Go JSON codecs for enums and sealed unions, TypeScript declarations, and devalue transport codecs for SvelteKit (`devalue`, `devalue/codegen`). The `grammar` package exposes the lowering so other tools can write their own backend. The CLI uses `//go:build jsonschema` build tags to separate schema registration from production code.

## Commands

```bash
# Run all tests
go test ./...

# Run a specific test
go test ./... -run 'TestName'

# Lint (requires: modernize, staticcheck, govulncheck, golangci-lint, goimports)
just lint

# Build every examples/ package with a //go:build jsonschema registration
# file — go test ./... doesn't compile these, so this is the only thing
# that catches a broken registration before generation/CI does.
just build-tagged

# Build the CLI
go build ./polytype

# Generate schemas for an example
cd examples/basictypes && go generate ./...
```

Task runner is `just` (justfile), not `make`.

## Architecture

### Two-Phase Generation Pipeline

1. **Phase 1 — JSON schema files**: Scans Go types via AST, generates `.json` files in `jsonschema/` subdirectory
2. **Phase 2 — Go code**: Generates `jsonschema_gen.go` with `embed.FS` for runtime schema access

### Package Layout

- **`polytype/`** — CLI entry point (`gen`, the only subcommand)
- **`typegrammar/`** — The closed, validated type grammar every backend consumes (`Scalar`, `Time`, `Enum`, `Object`, `Pointer`, `Slice`, `Array`, `Ref`; field values `Required`/`Optional`/`Nullable`/`Union`/`OptionalUnion`/`UnionSlice`). `Validate` is the admission boundary
- **`grammar/`** — Public lowering entry point: `Load(dir)`, `(*Package).Types()`, `(*Package).Lower(roots)`; thin bridge over `builder.SchemaBuilder.LowerRoots`
- **`devalue/`** — Go port of the devalue flat `stringify`/`parse` runtime; goldens in `testdata/golden.json` recorded from devalue 5.9 by `testdata/record`
- **`devalue/codegen/`** — Emits strict Go encoders/decoders (`EncodeT`/`DecodeT`/`StringifyT`/`ParseT`) for a lowered definition graph; compile-and-run fixture in `testdata/fixture`
- **`typescript/`** — Public TypeScript declaration backend over `typegrammar`; `Generate` returns the files plus the emitted identifier per definition. The marker-free fixture in `testdata/fixture` proves the library path matches `--typescript` byte for byte
- **`internal/syntax/`** — AST parsing, package loading (uses `golang.org/x/tools/go/packages` with `jsonschema` build tag), type scanning, comment extraction
- **`internal/builder/`** — Schema generation engine. `SchemaBuilder` orchestrates: type scanning → schema node construction → JSON output → Go code generation
- **`internal/builder/model.go`** — Schema node types: `ObjectNode`, `PropertyNode`, `ArrayNode`, `UnionTypeNode`, `RefNode`, `TemplateHoleNode`
- **`internal/common/`** — Struct tag parsing, helpers
- **Root package (`polytype`)** — Declaration/registration API (`Declare`, `.StringerEnum`, `.Ref`, `SealedUnion`, ...) and runtime codec types (`Optional[T]`, `Nullable[T]`)
- **`jsonschema/`** — Hand-rolled JSON Schema construction helpers (`JSONSchema`, `ObjectSchema`, `ParentSchema`, `StringSchema`, `ArraySchema`, `ConstSchema`, `EnumSchema`, ...) for writing provider functions

### Registration System

Schema types are registered via no-op marker functions in build-tagged `schema.go` files. The scanner reads these as AST call expressions. `Declare(fn)` — a method expression (`T.Schema`) or a free function taking `T` — is the primary entry point; chain options onto the returned `*Declaration[T]`:

- `Declare(T.Schema)` — primary registration
- `.StringerEnum(T{}.Field)` — emit an integer enum field's constant names as strings
- `.Accessor(...)` / `.Method(...)` / `.Function(...)` / `.RenderProviders()` — provider-based template rendering
- `.Ref()` — render this type as `"$ref"` wherever it's referenced

Enums and unions are not declared per field. A type is an enum when it declares the marker method `func (T) enum()` in ordinary (non-build-tagged) Go; its values are the typed constants in the same package. A field whose type is a sealed interface (one whose own body declares an unexported method) becomes a discriminated union of every named struct in the same package that declares that method directly. The discriminator property defaults to `"type"`; override it once per interface with the package-level `SealedUnion[I](name)` marker in the interface's package.

`NewJSONSchemaMethod`/`NewJSONSchemaFunc` with their remaining `With*` options stay supported for source compatibility (each carries a `Deprecated:` godoc comment naming its fluent equivalent), but new registration code should use `Declare(...)`. `.Enum`, `.Interface`, `Discriminator`, `Impl`, `WithEnum`, `WithInterface*`, `WithDiscriminator`, `NewEnumType`, and `NewInterfaceImpl` were removed in v1.0.0-rc.7.

### Key Patterns

- **Build tags**: `//go:build jsonschema` for registration code, `//go:build !jsonschema` for generated code
- **Discriminators**: Default `"type"`, overridable per interface with `SealedUnion[I](name)`; the wire value is always the concrete variant type name
- **Comments → descriptions**: Go doc comments automatically become JSON Schema `description` fields
- **Optional fields**: `polytype.Optional[T]` with `json:",omitzero"` (not `omitempty`, which only affects Go marshaling)

### Validation

Pass `--validate` to generation, and add a panic stub such as `func (Person) ValidateJSON(_ []byte) error { panic("not implemented") }` to the tagged file, to get a generated `ValidateJSON([]byte) error` method. Schemas are compiled once in `init()` using `github.com/santhosh-tekuri/jsonschema/v6`. Rendered/template types don't receive one because their schemas depend on runtime values.

### Limitations

- No support for maps, channels, functions, or inline interfaces
- Circular/recursive references are detected and rejected
- External package types limited to `time.Time` (rendered as string with RFC3339 guidance)
- Max nesting depth: 100

## Test Structure

- Unit tests alongside source files (`*_test.go`)
- Integration test fixtures in `internal/builder/testfixtures/` and `internal/builder/test_run/`
- Golden file comparisons via `internal/testutils/golden_file.go`
- All tests are plain `go test`. Two consult Node tooling only when present and skip otherwise: `typescript` compiles its edge-case output with `tsc`, and `devalue` compares against goldens recorded from devalue 5.9. `npm ci` at the repo root installs both pins; CI does this. Do not add TypeScript test code, ledgers, or provenance machinery
- Example directories each contain types, registration, and generated output
