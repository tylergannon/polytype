---
name: polytype
description: >
  Use when projecting Go types into JSON Schema, validation, Go JSON codecs, TypeScript
  declarations, or devalue (SvelteKit) transport codecs, or when writing a custom backend on
  polytype's type grammar.
---

# polytype

polytype is a type projection tool. It lowers Go types, statically at
`go generate` time, into one closed type grammar and projects that grammar
into the other type systems a program speaks:

- JSON Schema files plus Go accessors (the CLI's always-on output), tuned for
  LLM function calling: struct field order, `additionalProperties: false`,
  ordinary and nullable fields required, `Optional[T]` optional, doc comments
  as descriptions.
- Validation methods (`--validate`) and YAML input (`--formats=both`).
- Go JSON codecs inferred from the types: membership-checked enums and
  discriminated sealed unions. No flag selects them.
- Structural TypeScript declarations (`--typescript DIR`, or the `typescript`
  package from a Go program that already knows its roots).
- devalue transport for SvelteKit: a Go runtime port plus generated strict
  Go codecs (`devalue`, `devalue/codegen`), driven from a Go program.
- A library entry point for your own backend (`grammar`, `typegrammar`).

Every projection refuses a shape it cannot represent faithfully rather than
widening to `any`. Generated encoding/decoding is limited to the documented
shapes; no general-purpose typed round trip is implied.

Import paths: `github.com/tylergannon/polytype` (configuration and wrappers),
`github.com/tylergannon/polytype/codegen` (programmatic generation),
`github.com/tylergannon/polytype/polytype` (CLI), and the library packages
above under the same module.

## Mental model: two build-tagged files

- `schema.go` — `//go:build jsonschema`. You write this. Panic stubs + marker
  registrations. Compiled only during generation, never in production.
- `jsonschema_gen.go` — `//go:build !jsonschema`. Generated. Real schema,
  validation, and selected codec methods over an `embed.FS` of
  `jsonschema/*.json`. JSON is the default; YAML is opt-in.

The build tags make them mutually exclusive, so the package always compiles —
before and after generation. Commit all generated outputs: `jsonschema_gen.go`
and the whole `jsonschema/` directory (each `T.json` schema comes with a
`T.json.sum` checksum the tool uses for change detection).

## Setup workflow

1. **Add the tool** (Go 1.27+ tool directive — keeps the version in go.mod so
   every contributor and CI runs the same binary):

   ```bash
   go get -tool github.com/tylergannon/polytype/polytype@latest
   ```

2. **Add the generate directive** to the file defining your types:

   ```go
   //go:generate go tool polytype
   ```

   Add `--validate` to generate validation methods (recommended for LLM
   output): `//go:generate go tool polytype --validate`. Add
   `--formats=both` when inputs may be JSON or YAML; validation then includes
   `ValidateYAML`.

3. **Write `schema.go` by hand** — see the example below. One panic stub per
   generated method and one `Declare` line per root type. Add a
   `ValidateJSON` stub when the directive passes `--validate`, and a
   `ValidateYAML` stub when it also passes `--formats=both`. Then run
   `go generate ./...`.

4. **Tidy** when generation adds dependencies: run `go mod tidy`. Validation
   imports `github.com/santhosh-tekuri/jsonschema/v6`; opted-in YAML support
   imports `go.yaml.in/yaml/v4`.

5. **Verify**: `go build ./...` and `go test ./...` must pass, and a second
   `go generate ./...` must produce no diff (generation is idempotent).

6. **Wire it into commits/CI** so schemas never drift from types — read
   [references/hooks-and-ci.md](references/hooks-and-ci.md) for lefthook and
   GitHub Actions recipes (auto-stage vs fail-on-drift).

## TypeScript declarations and the Go JSON boundary

For a Go and TypeScript integration, pin one explicit module release for both
the tool and imported marker/runtime package. This combined surface requires
`v1.0.0-rc.8` or newer: `v1.0.0-rc.4` includes TypeScript declarations but
predates generated owner codecs, and releases before `v1.0.0-rc.7` predate the
marker-based enum and sealed-union registration:

```bash
go get -tool github.com/tylergannon/polytype/polytype@v1.0.0-rc.10
```

Generate the schema, validation, Go output, and TypeScript declarations in one
run:

```go
//go:generate go tool polytype --validate --typescript web/src/generated --typescript-barrel
```

The barrel flag is optional. Sealed interface fields and `.StringerEnum`
registrations automatically select JSON codecs on the containing Go struct;
there is no codec flag. Encode that owner with `json.Marshal`. For incoming JSON,
call its generated `ValidateJSON` before `json.Unmarshal`.

The TypeScript output is structural only. It supplies `types.ts` and an optional
type-only `index.ts`, with no runtime decoder or validator. TypeScript consumers
use `JSON.parse`/`JSON.stringify` and must validate untrusted runtime data in the
application. Do not claim executed cross-language equivalence from TypeScript
compilation alone; issue #71 owns the broader Go/JavaScript transport proof.

## devalue transport and custom backends

When the task is a Go ↔ JavaScript boundary on SvelteKit's devalue wire, or a
new projection of the same types, read
[references/devalue-and-grammar.md](references/devalue-and-grammar.md). It
covers the `devalue` runtime, the generator program that emits typed codecs
into a package you choose, the wire rules, the `typescript` backend for a
generator that already knows its roots and wants no schema files, and the
`grammar`/`typegrammar` entry point. Those are library packages, not CLI
flags.

## Minimal example

```go
// types.go
package contacts

import "github.com/tylergannon/polytype"

//go:generate go tool polytype --validate

// Person is a single contact extracted from the document.
type Person struct {
    // Full legal name, e.g. "Ada Lovelace".
    Name string `json:"name"`

    // Age in whole years at the time of writing.
    Age int `json:"age"`

    // Email address. Omit when not stated in the source text.
    Email polytype.Optional[string] `json:"email,omitzero"`

    // Required key; null means no phone number was supplied.
    Phone polytype.Nullable[string] `json:"phone"`
}
```

```go
// schema.go
//go:build jsonschema

package contacts

import (
    "encoding/json"
    "github.com/tylergannon/polytype"
)

// Stubs so the package compiles before generation; jsonschema_gen.go
// provides the real implementations.
func (Person) Schema() json.RawMessage     { panic("not implemented") }
func (Person) ValidateJSON(_ []byte) error { panic("not implemented") }

var _ = polytype.Declare(Person.Schema)
```

Run `go generate ./...`, then use it:

```go
schema := Person{}.Schema()                      // json.RawMessage for the tool definition
if err := Person{}.ValidateJSON(llmOutput); err != nil {
    // *jsonschema.ValidationError: InstanceLocation, ErrorKind, Causes
}
```

## Doc comments ARE the schema descriptions

Every field doc comment is copied verbatim into that property's `description`,
and the type's doc comment becomes the top-level schema description. The LLM
filling the fields reads these — so write them as instructions to the model,
not as notes to Go maintainers:

- State semantics, format, units, and valid ranges: "RFC3339 timestamp",
  "score from 0.0 to 1.0", "lowercase kebab-case slug".
- Say when to omit an optional field.
- Skip Go implementation trivia ("backed by sync.Map") — it wastes prompt
  tokens and confuses the model.

```go
// Bad:  getter for the ts field, set by the ingest worker
// Good: Time the event occurred, as an RFC3339 string, e.g. "2026-07-09T14:00:00Z".
Timestamp string `json:"timestamp"`
```

## Required vs optional

Ordinary fields and `polytype.Nullable[T]` fields are required.
`polytype.Optional[T]` fields are omitted from `required` and must use
`json:",omitzero"`. Optional rejects JSON null; Nullable accepts null. Both
preserve present zero and empty values through their `Present` and `Value`
fields. Validate before unmarshaling when missing-vs-null matters, because plain
`json.Unmarshal` cannot distinguish those states for Nullable.

For OpenAI strict Structured Outputs, every property must be required. Use
Nullable for OpenAI's documented required-plus-null pattern. Do not use
Optional in a strict schema because it deliberately removes the property from
`required`.

Wrappers must be complete direct named field types. V1 Optional follows the
ordinary renderer's scalar and named scalar, struct, pointer, array/slice,
supported-ref, and registered-interface paths. V1 Nullable supports scalars,
registered enums, structs, pointers to structs, and structs registered with
`.Ref()`.

## Beyond flat structs

Enums (string consts and iota+Stringer), discriminated unions over interfaces,
custom discriminators, free-function registration, shared `$ref`/`$defs` via
`.Ref()`, and the full CLI/flag reference live in
[references/registration-api.md](references/registration-api.md). Read it
when a type uses enums, interfaces, or you need non-default generation flags.
Unions are inferred, never declared: an interface with an unexported method is
sealed, and its variants are the same-package struct types declaring that
method directly (value receiver = value variant, pointer receiver = pointer
variant). Non-sealed interface fields fail generation. Discriminator values
are the concrete type names. The default discriminator property is `type` for
both JSON and YAML; declare another once per union with
`polytype.SealedUnion[I](name)` in the package that declares `I`. Generation
is JSON-only by default; `--formats=both` adds yaml/v4 entry points that
translate YAML into the JSON data model and reuse the JSON validator and
decoder. JSON Schema property names and `json` tags are canonical. Go `yaml`
struct tags are ignored and nested custom `UnmarshalYAML` hooks are bypassed;
custom `UnmarshalJSON` hooks remain authoritative. yaml/v4 does not pass decoder
options into `UnmarshalYAML`, so `yaml.WithKnownFields()` cannot enforce strict
fields inside a registered type; use generated `ValidateYAML` for schema-backed
unknown-property rejection. Decoding is transactional replacement, so omitted
YAML fields do not retain receiver values. Use `yaml.WithV4Defaults()` to match
`ValidateYAML` resolution.

By default, a struct type referenced from multiple places is inlined at every
call site; add `.Ref()` to its registration to render it once as a `"$ref"`
into `"$defs"` instead.

For concise, source-backed examples of optionality, enums, interface
discriminators, and shared `$defs`, read
[references/examples.md](references/examples.md). The snippets are generated
from compiling examples in this repository and checked for drift by the Go
test suite.

Known limitations (fail fast, don't fight them): no maps or recursive types;
registered interfaces support scalar `I`, `Optional[I]`, and direct
one-dimensional `[]I` fields, but not `Nullable[I]`, fixed arrays, nested
slices, named slice containers, or Optional/Nullable interface slices; external
package types are unsupported except `time.Time`.

For unions, marshal the containing struct value or pointer. Its generated
codec applies each field's discriminator to `I`, `Optional[I]`, and `[]I`.
Do not add a global discriminator marshaler to an implementation: the same Go
type can have different wire identities in different fields. Nil required
unions/slices, typed-nil implementations, and conflicting custom object
payloads are encoding errors. Production owner JSON method collisions are
rejected before generation writes output. Verify encode/validate/decode with
semantic equality for the shapes used by the consumer.

## Closeout checklist

- `go generate ./...` runs clean and a second run produces no diff.
- `go build ./...` and `go test ./...` pass.
- Generated `jsonschema/*.json`, `jsonschema_gen.go`, any requested
  TypeScript declarations, and any generated devalue codec file are committed.
- Field doc comments read as LLM-facing descriptions.
- A pre-commit hook or CI check guards against schema drift.
