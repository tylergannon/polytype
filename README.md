# polytype 🧩

**One Go type. Many typed projections.**

polytype is a type projection tool. It reads your Go types at `go generate`
time, lowers them into one closed, validated type grammar, and projects that
grammar into the other type systems your program has to speak:

| Projection | What you get | How |
|---|---|---|
| **JSON Schema** | Deterministic schemas for LLM tool calls and structured output, embedded in Go | `go tool polytype` (always on) |
| **Validation** | `ValidateJSON` / `ValidateYAML` methods backed by the schemas | `--validate`, `--formats=both` |
| **Go JSON codecs** | Membership-checked enums, discriminated sealed unions, YAML input | Inferred from your types; no flag |
| **TypeScript** | Structural `types.ts` declarations for the same shapes | `--typescript DIR`, or the `typescript` package |
| **devalue transport** | Go encoders/decoders for the [devalue](https://github.com/sveltejs/devalue) wire format SvelteKit uses, plus a Go port of the runtime | `devalue` and `devalue/codegen` packages |
| **Your own backend** | Load a package, lower any roots, walk the grammar | `grammar` and `typegrammar` packages |

The Go type is the single source of truth. Every projection is derived
statically from the AST, never from runtime reflection, and every projection
refuses a shape it cannot represent faithfully instead of widening to `any`.

<p align="center">
  <img src="gopher-front.svg" alt="Gopher mascot" width="200" height="auto">
</p>

- **Docs**: https://go-gen-jsonschema.tylergannon.com
- **LLM/agent-friendly docs**: [llms.txt](llms.txt)
- **Agent skill**: [skills/polytype](skills/polytype/SKILL.md)

## 🚀 Quick Start

### Using an AI coding agent?

Install the agent skill first — it teaches Claude Code, Cursor, Codex, and
friends the full workflow (setup, registration API, doc-comment conventions,
git-hook integration):

```bash
npx skills add tylergannon/polytype
```

Then just ask your agent to "add polytype to this project."

### Setting up by hand

1. **Add the tool to your module** (Go 1.27+; pins the version in go.mod so
   every contributor and CI runs the same binary):

   ```bash
   go get -tool github.com/tylergannon/polytype/polytype@latest
   ```

2. **Add a generate directive** next to your types (include `--validate` for
   generated validation; add `--formats=both` when inputs may be YAML):

   ```go
   //go:generate go tool polytype --validate --formats=both
   ```

3. **Write the registration file and generate** (the file is shown under
   "How it works" below):

   ```bash
   go generate ./...
   go mod tidy   # records dependencies added by validation or opted-in YAML decoding
   ```

4. **Use the generated methods:**

   ```go
   schema := Person{}.Schema()          // json.RawMessage — drop into your tool definition
   err := Person{}.ValidateJSON(data)   // or ValidateYAML for YAML input
   ```

Commit everything the generator writes: `jsonschema_gen.go` and the
`jsonschema/` directory (schemas plus `.json.sum` checksums).

## 🔍 Why this tool

- **Deterministic property ordering** — properties are emitted in struct field
  order, so you control schema layout precisely. Property order influences LLM
  output quality; most generators iterate maps and produce random order.
- **Schemas can't drift** — change a struct, run `go generate`, done. Wire it
  into a pre-commit hook or CI (see below) and drift becomes impossible.
- **LLM-optimized defaults** — `additionalProperties: false`, ordinary and
  nullable fields required, `Optional[T]` fields optional, and doc comments
  become `description` fields.
- **Built-in validation** — opt-in `ValidateJSON()` and, with
  `--formats=both`, `ValidateYAML()` methods backed by schemas compiled once at
  startup.
- **Optional YAML input** — `--formats=both` adds yaml/v4 entry points that
  translate YAML into the schema's JSON data model, then reuse the JSON
  validator and decoder.

## ⚙️ How it works

The tool keeps registration code out of your production build with a pair of
mutually exclusive build-tagged files:

| File | Build tag | Who writes it | Contents |
|---|---|---|---|
| `schema.go` | `//go:build jsonschema` | You | Panic stubs + marker registrations; compiled only during generation |
| `jsonschema_gen.go` | `//go:build !jsonschema` | Generated | Real schema, validation, and selected codec methods over an embedded `jsonschema/` directory |

Your package compiles at every stage — before generation (stubs) and after
(generated implementations).

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

    // Phone number. Emit null when the source explicitly has no phone number.
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

// Stubs so the package compiles before generation.
func (Person) Schema() json.RawMessage     { panic("not implemented") }
func (Person) ValidateJSON(_ []byte) error { panic("not implemented") }
func (Person) ValidateYAML(_ []byte) error { panic("not implemented") }

var _ = polytype.Declare(Person.Schema)
```

`go generate ./...` produces `jsonschema/Person.json`:

```json
{
  "type": "object",
  "description": "Person is a single contact extracted from the document.",
  "properties": {
    "name": {"type": "string", "description": "Full legal name, e.g. \"Ada Lovelace\"."},
    "age": {"type": "integer", "description": "Age in whole years at the time of writing."},
    "email": {"type": "string", "description": "Email address. Omit when not stated in the source text."},
    "phone": {"type": ["string", "null"], "description": "Phone number. Emit null when the source explicitly has no phone number."}
  },
  "required": ["name", "age", "phone"],
  "additionalProperties": false
}
```

## ✍️ Doc comments become descriptions

Field and type doc comments are copied into the schema's `description` fields —
the text the LLM reads when filling in values. Write them as instructions to
the model: formats, units, ranges, when to omit. To keep a developer-facing
comment out of the schema, supply a `description` struct tag instead:

```go
type User struct {
    // Developer notes stay here and never reach the LLM.
    Username string `json:"username" description:"The user's unique handle, lowercase, no spaces."`
}
```

## 🏷️ Struct tag reference

| Tag | Effect |
|---|---|
| `json:"name"` | Property name (standard Go semantics) |
| `json:",omitzero"` | Required on `Optional[T]`; omits the wrapper's absent zero value |
| `description:"..."` | Overrides the doc comment as the property description |
| `jsonschema:"ref=definitions/T"` | Emit a `$ref` instead of inlining (you must define the referenced schema yourself) |

Use `polytype.Optional[T]` when a property may be absent and must not be
null. Use `polytype.Nullable[T]` when the property is required but may be
null. Both wrappers expose `Present` and `Value`; present zero and empty values
remain distinguishable from absence/null. Plain `json.Unmarshal` cannot tell a
missing Nullable key from an explicit null, so call generated `ValidateJSON`
before decoding when required-key presence matters.

For OpenAI strict Structured Outputs, every property must be required. Use
`Nullable[T]` for OpenAI's documented required-plus-null pattern; a schema with
`Optional[T]` is not strict-compatible because that property is not required.
See the [Structured Outputs guide](https://developers.openai.com/api/docs/guides/structured-outputs#all-fields-must-be-required).

V1 `Optional` supports scalar and named scalar values, structs, pointers,
arrays/slices, explicit supported refs, and registered interfaces. V1 `Nullable`
supports scalars, registered enums, structs, pointers to structs, and structs
registered with `.Ref()`. Wrappers must be the complete type of a direct named
field; aliases, nesting, embedding, and unsupported Nullable shapes fail
generation.

Migration note: `jsonschema:"optional"` is no longer honored. Replace it with
`polytype.Optional[T]` and add `json:",omitzero"`; otherwise the field is
required when schemas are regenerated.

By default nested struct types are **inlined** at every use site — no `$defs`,
no `$ref` — which is what LLM APIs handle best.

## 🎯 Enums

Enum-ness is a property of the type. A named type declares itself as an enum
with the marker method `func (T) enum()` in ordinary (non-build-tagged) Go;
its values are the typed `const` declarations in the same package, and every
use of the type in every generated schema, codec, and TypeScript output is an
enum. No field-level declaration is needed. Integer/iota enums emit their
integer values by default; `StringerEnum` on a field emits the constant
*names* as string values instead — far more meaningful to an LLM than raw
integers.

```go
type Status string

func (Status) enum() {}

const (
    StatusPending    Status = "pending"
    StatusInProgress Status = "in_progress"
    StatusCompleted  Status = "completed"
)

type LogLevel int

const (
    LogDebug LogLevel = iota
    LogInfo
    LogError
)

type Task struct {
    Status   Status   `json:"status"`   // ["pending", "in_progress", "completed"]
    LogLevel LogLevel `json:"logLevel"`
}
```

```go
// schema.go (//go:build jsonschema)
var _ = polytype.Declare(Task.Schema).
    StringerEnum(Task{}.LogLevel) // ["LogDebug", "LogInfo", "LogError"]
```

The marker must be `func (T) enum()` exactly: a pointer receiver, parameters,
or results are a generation error naming the type, as is a marked type with
no typed constants. The marker means value mode; a `String()` method on a
marked type is ignored. `.StringerEnum` on a field of a marked integer type
still selects name mode for that field.

The generated file references every marked type in its package through its
first typed constant, as `var _ interface{ enum() } = StatusPending`, so the
marker is used from production code, its shape is checked by the compiler,
and `staticcheck` stays quiet with no lint directives. The right-hand side
must be a value of the marked type (a constant, not a pointer) because the
marker uses a value receiver. A package that declares a marked enum but never
runs generation (a shared enums package, say) needs one such line written by
hand per marked type.

Every marked enum type declared in the generation target package receives a
type-level `MarshalJSON` (value receiver) and `UnmarshalJSON` (pointer
receiver) that enforce membership in both directions: encoding a value that is
not a declared constant, or decoding one, fails with an error naming the enum
type and the offending value, and `null` is rejected. A marked enum imported
from another package is not guarded this way. A production `MarshalJSON` or
`UnmarshalJSON` already declared on a marked type is a generation error before
anything is written.

String-mode fields additionally receive codecs on the containing struct. Both
`json.Marshal(Task{...})` and decoding into `*Task` use the registered constant
names for that field; other fields of the same type stay numeric. One owner
codec composes enum and union adapters.

Registered enum fields cannot use the `json:",string"` option. The generator
rejects that option before writing files because it disagrees with both the
numeric schema representation and the generated string-mode adapter.

Integer string mode supports direct `E`, `Optional[E]`, and `Nullable[E]`
fields. Absent Optional and null Nullable values bypass conversion. Unknown
names, undeclared values (including an undeclared zero), ambiguous aliases,
custom enum JSON hooks, and unsupported adapted containers are rejected.
Validate external JSON before decoding to enforce required fields and schema
membership. See [the enum guide](website/src/content/docs/features/enums.md).

Migration: `Declare(Task.Schema).Enum(Task{}.Status)`, `WithEnum(...)`, and
the package-level `NewEnumType[Status]()` are removed. Add
`func (Status) enum() {}` next to the type and delete the field-level and
package-level declarations; `.StringerEnum` / `WithStringerEnum` are
unchanged.

## 🔄 Union types (sealed interfaces)

A field whose type is a **sealed interface** becomes an `anyOf` union of the
interface's variants, discriminated by a `"type"` property whose value is the
concrete type name. An interface is sealed when its own body declares an
unexported method; its variants are inferred: every named struct type in the
same package that declares that method directly. The receiver of the sealing
method decides the variant kind: a value receiver is a value variant, a
pointer receiver is a pointer variant, and decoding constructs the variant
accordingly. Nothing is declared at the field. A direct one-dimensional slice
of the interface becomes an array with the union under `items.anyOf`. Generation defaults to JSON-only. Pass `--formats=both` to add
`UnmarshalYAML(*yaml.Node)` adapters. yaml/v4 parses the document, the adapter
translates it into JSON, and the existing JSON decoder performs union dispatch
for scalar values (including `Optional[I]`) and every slice element.

```go
// PaymentMethod is sealed by its unexported method.
type PaymentMethod interface{ isPaymentMethod() }

type CreditCard struct {
    CardNumber string `json:"cardNumber"`
    Expiry     string `json:"expiry"`
}
func (CreditCard) isPaymentMethod() {}   // value variant, wire value "CreditCard"

type BankTransfer struct {
    AccountNumber string `json:"accountNumber"`
    RoutingNumber string `json:"routingNumber"`
}
func (*BankTransfer) isPaymentMethod() {} // pointer variant, wire value "BankTransfer"

type Payment struct {
    Amount  float64         `json:"amount"`
    Methods []PaymentMethod `json:"methods"`
}
```

```go
// schema.go (//go:build jsonschema)
var _ = polytype.Declare(Payment.Schema)
```

**Unsupported forms fail generation with a diagnostic naming the type or
field**: a reachable interface field whose interface is not sealed (there is
no explicit fallback and no support for non-sealed unions); an interface that
gets its unexported method only by embedding another interface; a type that
satisfies the interface only through an embedded field (it inherits the
sealing method and is excluded); a type that declares the sealing method but
does not implement the complete interface; a sealed interface with zero
variants; an embedded interface payload; and a variant with a payload
property that collides with the discriminator property. Variants behind
other build tags are not discovered: the scanner loads with the `jsonschema`
tag.

### Custom discriminator: `SealedUnion[I](name)`

The discriminator property is a property of the union, never of a field. A
union has one codec, so it has one discriminator. The default `"type"` needs
no declaration; to use another property, declare it once, in the build-tagged
file of the package that declares the interface:

```go
//go:build jsonschema

var _ = polytype.SealedUnion[PaymentMethod]("kind")
```

Every use of `PaymentMethod` in every generated schema, codec, and TypeScript
output now discriminates on `"kind"`; nothing changes at any field, and the
values are still the concrete type names. The argument must be a string
literal naming a nonempty property. A declaration in another package, a
duplicate declaration, a declaration for a non-sealed interface or a
non-interface type, a non-literal argument, an invalid property name, or a
variant payload property colliding with the declared name is a generation
error naming the interface.

**Wire-contract hazard:** the discriminator value is the concrete type name,
so renaming a variant type changes its wire value, and adding a qualifying
implementation changes membership. Review generated schema diffs
accordingly.

Opt into YAML alongside the default JSON unmarshaler in the generation
directive:

```go
//go:generate go tool polytype --formats=both
```

With the default discriminator, ordinary YAML can be decoded directly:

```go
import yaml "go.yaml.in/yaml/v4"

var payment Payment
err := yaml.Load([]byte(`
amount: 42
methods:
  - type: CreditCard
    cardNumber: "4111111111111111"
    expiry: "12/30"
`), &payment, yaml.WithV4Defaults())
```

YAML uses the JSON Schema property names. Go `yaml` struct tags are ignored,
and nested custom `UnmarshalYAML` hooks are bypassed; JSON tags and custom
`UnmarshalJSON` hooks remain authoritative. The generator owns
`UnmarshalYAML` on registered types. YAML constructs that cannot be represented
by JSON are rejected. Run `go mod tidy` after YAML-enabled generation to record
the yaml/v4 dependency. Because yaml/v4 does not pass decoder options into
`UnmarshalYAML`, `yaml.WithKnownFields()` cannot enforce strict fields inside a
registered type; use the generated `ValidateYAML` method for schema-backed
unknown-property rejection. Decoding is transactional replacement: omitted YAML
fields do not retain values already present in the receiver. Decode with
`yaml.WithV4Defaults()` to use the same scalar resolution as `ValidateYAML`.

Migration: `Declare(T.Schema).Interface(field, Discriminator(...), Impl(...))`,
`WithInterface`, `WithInterfaceImpls`, `WithDiscriminator`, `Impl`,
`Discriminator`, and the package-level `NewInterfaceImpl[I](...)` are removed.
Give the interface an unexported method, declare it directly on every
variant, and delete the field-level declaration. Wire values are now always
the concrete type names; a custom discriminator property moves from the
field to one `SealedUnion[I](name)` declaration in the interface's package.

The generator emits a value-receiver `MarshalJSON` and pointer-receiver
`UnmarshalJSON` on the containing struct. Encoding the owner adds each union
field's registered discriminator; decoding preserves its registered value or
pointer implementation. This works for `I`, `Optional[I]`, and direct `[]I`
fields. Marshaling a concrete implementation by itself uses its normal Go
encoding because discriminator configuration belongs to the field.

Generated owner codecs reject nil required unions,
typed-nil implementations, and nil required union slices. An allocated empty
slice encodes as `[]`; an absent Optional is omitted. Custom concrete
`MarshalJSON` hooks must return an object with a missing or matching string
discriminator. Conflicting payloads are errors. Production owner JSON methods,
including promoted methods that would interfere with generated codecs, are
rejected before output is written. Validate external input before decoding.


Only direct one-dimensional slices are supported. Fixed arrays, nested slices,
named slice containers, `Optional[[]I]`, and `Nullable[[]I]` fail generation.
An `Optional[I]` scalar is supported; `Nullable[I]` is not.

## TypeScript declarations

Pass `--typescript <directory>` to generate structural TypeScript declarations
in `<directory>/types.ts` alongside the ordinary JSON Schema and Go outputs.
Relative directories are resolved from the directory where the command is
invoked; absolute paths also work.

```go
//go:generate go tool polytype --typescript web/src/generated
```

Add `--typescript-barrel` to also generate `index.ts` with explicit type-only
exports from `./types.js`:

```ts
import type { Person } from "./generated/index.js";
```

The generator only replaces files carrying its generated-file header. It will
not overwrite an application-owned `types.ts` or `index.ts`. If a later run
omits `--typescript-barrel`, it removes a previously generated `index.ts` while
preserving an application-owned one. `--no-changes` checks these requested
artifacts for missing or stale content as well as checking JSON Schemas.

These declarations describe the admitted JSON structure for static TypeScript
checking. They do not provide runtime decoding or validation on the JSON wire;
[issue #71](https://github.com/tylergannon/polytype/issues/71) tracks that
proof, and the [devalue codecs](#-go--javascript-transport-with-devalue) are
the typed, runtime-checked Go/JavaScript transport polytype does ship. Time values remain strings and numeric fields become
`number`; TypeScript does not enforce Go integer ranges or JSON Schema formats.
Enum literals that cannot be represented exactly are rejected. Unsupported
static shapes, including runtime schema providers and custom JSON/text codecs,
fail with a diagnostic. Generation itself has no Node, npm, or JavaScript-engine
dependency.

### From a Go program

A generator that already knows which types it wants, such as a SvelteKit
tool that has picked its remote-function argument and result types, does not
need the CLI, a `//go:build jsonschema` file, or the schema outputs. The
backend behind `--typescript` is the `typescript` package:

```go
pkg, err := grammar.Load("./orders")
scope := pkg.Types().Scope()
defs, _, err := pkg.Lower([]grammar.Root{{Type: scope.Lookup("Order").Type()}})
result, err := typescript.Generate(defs, typescript.Options{Barrel: true})
for _, file := range result.Files { // types.ts, index.ts
    os.WriteFile(filepath.Join("web/src/generated", file.Name), file.Content, 0o644)
}
```

The bytes are identical to the CLI's for the same roots. `result.Names` maps
each definition to the identifier it was declared under, which is the Go
name unless a reserved word, an illegal identifier, or a same-named type in
another package forced a rename; a tool writing
`import type { Order } from './types'` reads it from there. Nothing is
written into the package that declares the types. See
[Driving polytype from another generator](llms.txt) for both paths side by
side.

### Adopt TypeScript declarations with Go JSON codecs

Pin an explicit module release that contains both capabilities; the generator
and the imported marker/runtime package must use that same release:

```bash
go get -tool github.com/tylergannon/polytype/polytype@v1.0.0-rc.10
```

The `devalue`, `devalue/codegen`, and `grammar` packages require `v1.0.0-rc.10` or newer; the `typescript` package requires the release after it. The combined TypeScript and codec surface requires `v1.0.0-rc.8` or newer: `v1.0.0-rc.4` includes
TypeScript declarations but predates generated owner codecs, and releases before
`v1.0.0-rc.7` predate the marker-based enum and sealed-union registration. Pin
the version explicitly.

Generate validation and TypeScript declarations together. The field
registrations shown above automatically select the containing struct's enum and
union JSON codecs; there is no separate codec flag:

```go
//go:generate go tool polytype --validate --typescript web/src/generated --typescript-barrel
```

Run `go generate ./...`, then `go mod tidy`, and commit `schema.go`,
`jsonschema_gen.go`, the complete `jsonschema/` directory, and the generated
`types.ts` plus optional `index.ts`. On the Go boundary, use `json.Marshal` on
the containing struct. For incoming bytes, validate before decoding:

```go
if err := (Envelope{}).ValidateJSON(data); err != nil {
    return err
}
var value Envelope
if err := json.Unmarshal(data, &value); err != nil {
    return err
}
```

TypeScript consumers import the generated declarations and use the platform's
`JSON.parse`/`JSON.stringify`. Type annotations do not validate runtime data, so
validate untrusted values with an application-owned validator before treating
them as a generated type. The generated Go validation method remains the final
check before Go decoding. A project that requires generated TypeScript runtime
decoders or validators on the JSON wire still needs a separate implementation;
this generator does not emit them. For a checked Go/JavaScript boundary, use
the devalue codecs below on the Go side with `devalue.parse`/`stringify` in
the browser.

## 🧬 Go ↔ JavaScript transport with devalue

[devalue](https://github.com/sveltejs/devalue) is the structured-value wire
format SvelteKit uses for `load` data and remote functions. polytype ships a
Go port of its flat `stringify`/`parse` pair and a code generator that emits
strict, typed Go codecs for your types on that wire, so a Go service can
produce exactly what `devalue.parse` expects in the browser and consume what
`devalue.stringify` sends back.

### The runtime: `devalue`

```go
import "github.com/tylergannon/polytype/devalue"

s, err := devalue.Stringify(devalue.NewObject("name", "Ada", "tags", []any{"a", "b"}))
// [{"name":1,"tags":2},"Ada",[3,4],"a","b"]

v, err := devalue.Parse(s, nil)   // *devalue.Object; numbers are float64, null is nil
```

The value model is `*devalue.Object` (ordered properties; `map[string]any` is
accepted on encode with sorted keys), `[]any`, `string`, `float64` and the
other Go numeric kinds, `bool`, `nil`, `devalue.Undefined`, `devalue.Hole`,
and the tagged forms `Date`, `*Map`, `*Set`, `BigInt`, `RegExp`, `ArrayBuffer`
and `*Boxed`. `StringifyWith(v, reducers)` and the `revivers` argument to
`Parse` are devalue's custom-type hooks. Output is byte-identical to devalue
5.9 for every shape the port implements; typed arrays, `URL` and `Temporal`
values are not implemented and parse to an error.

### Typed codecs: `devalue/codegen`

Generation is a small Go program you own, typically run from a
`//go:generate` directive. Load the package, choose roots, lower them, emit:

```go
package main

import (
    "go/token"
    "go/types"
    "os"

    "github.com/tylergannon/polytype/devalue/codegen"
    "github.com/tylergannon/polytype/grammar"
)

func main() {
    pkg, err := grammar.Load("./model")          // same loader the CLI uses
    if err != nil { panic(err) }
    scope := pkg.Types().Scope()
    defs, roots, err := pkg.Lower([]grammar.Root{
        {Type: scope.Lookup("Envelope").Type()},
        {Type: types.NewSlice(scope.Lookup("Envelope").Type()),
         Position: token.Position{Filename: "gen.go", Line: 1}},
    })
    if err != nil { panic(err) }
    src, err := codegen.Generate(defs, roots, codegen.Options{
        PackageName: "codec",
        ImportPath:  "example.com/app/codec",
    })
    if err != nil { panic(err) }
    os.WriteFile("codec/codec_gen.go", src, 0o644)
}
```

Roots need no `Declare` marker and may be anonymous types such as
`[]Envelope`; `Position` is only used in diagnostics. The emitted file lives
in a package of your choosing and uses only exported fields. For every
definition `T` reachable from the roots it declares:

```go
func EncodeT(v model.T) (any, error)      // Go value → devalue value model
func DecodeT(raw any) (model.T, error)    // strict; rejects anything the grammar does not admit
func StringifyT(v model.T) (string, error)
func ParseT(s string) (model.T, error)
```

Anonymous roots get the same four functions named `Root0`, `Root1`, … by
index. Run `go mod tidy` afterwards; the generated file imports the `devalue`
runtime.

Wire rules worth knowing before you ship:

- Every Go numeric kind is a JavaScript `number`; integers beyond 2^53 lose
  precision. There is no BigInt.
- `time.Time` is the same string `encoding/json` writes, never a `Date`.
- Absent `Optional` is no property; absent `Nullable` is `null`; a nil
  required slice is `[]`; a nil pointer where a value is required is an
  encode error.
- Enums honor their mode (values, or constant names under `.StringerEnum`)
  and reject non-members on both sides.
- Unions are one object whose discriminator property holds the concrete type
  name, exactly as in the JSON Schema and TypeScript projections.
- Decoders reject missing or unknown properties, wrong kinds, `undefined`,
  wrong array lengths, unknown tags, and every devalue tagged form. Errors
  carry a JSON-pointer-style path such as `/events/1/kind`.
- An anonymous struct is admitted as a field's own type but not under a
  slice, array, pointer, or as a root.

See [`devalue/codegen/testdata/fixture`](devalue/codegen/testdata/fixture)
for a complete module covering every node kind, and the package godoc for
the full contract.

## 🧱 Build your own projection: `grammar` and `typegrammar`

Every backend in this repository (schema, TypeScript, devalue) consumes the
same closed grammar in the `typegrammar` package: `Scalar`, `Time`, `Enum`,
`Object`, `Pointer`, `Slice`, `Array`, `Ref`, with field values `Required`,
`Optional`, `Nullable`, `Union`, `OptionalUnion`, and `UnionSlice`. Objects
are closed and ordered, references form a DAG, and `Validate` is the
admission boundary; there is no `any`, map, or open-union node.

The `grammar` package is the public entry point to that lowering. `Load(dir)`
reads a package the way the CLI does, `Types()` exposes its type-checked
scope, and `Lower(roots)` returns the validated definition graph plus one
node per root, refusing unsupported shapes with the same diagnostics the CLI
prints. Write a backend by switching over the node kinds (always with a
`default` case; the set may grow in minor versions) and you have a fourth
projection of the same types.

## 🛡️ Validation

Pass `--validate` to generation, add a matching `ValidateJSON` stub to the
tagged file, and every registered type gets `ValidateJSON([]byte) error`. With `--formats=both`, it
also gets `ValidateYAML([]byte) error`. Both methods validate the same JSON data
model and schemas are compiled once in `init()` via
[santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema).

```go
if err := (Person{}).ValidateJSON(llmOutput); err != nil {
    // *jsonschema.ValidationError with structured details:
    //   err.InstanceLocation — path to the failing field
    //   err.ErrorKind        — what went wrong
    //   err.Causes           — nested validation errors
    return err
}
var p Person
json.Unmarshal(llmOutput, &p)
```

Validation catches missing required fields, wrong types, unknown properties,
invalid enum values, and bad nested structure — before you unmarshal. Types
using `RenderProviders()` are excluded (their schemas depend on runtime
values).

## 🔁 Keeping schemas in sync (hooks & CI)

Generation supports a check mode that fails — writing nothing — when
regeneration would change any schema or requested TypeScript artifact:
`-no-changes`, or the env var
`JSONSCHEMA_NO_CHANGES=1` (which flows through `go generate` without editing
directives).

```yaml
# lefthook.yml — fail the commit on schema drift
pre-commit:
  commands:
    polytype-check:
      glob: "*.go"
      run: JSONSCHEMA_NO_CHANGES=1 go generate ./...
```

```yaml
# GitHub Actions — same guarantee in CI
- name: Check generated schemas are current
  run: JSONSCHEMA_NO_CHANGES=1 go generate ./...
```

Prefer auto-regenerating in the hook instead of failing? See
[the agent skill's hooks guide](skills/polytype/references/hooks-and-ci.md)
for the auto-stage variant and trade-offs.

## 📖 Registration API

`Declare(fn)` is the entry point. `fn` is a method expression (`T.Schema`) or a
free function taking `T` as its sole parameter; both infer `T`. Chain options
onto the returned `*Declaration[T]`:

| Chain method | Purpose |
|---|---|
| `.Accessor(field, T.method)` | Provider is a struct method taking only the receiver |
| `.Method(field, T.method)` | Provider is a struct method also taking the field's own value |
| `.Function(field, fn)` | Provider is a free function taking the field's own value |
| `.StringerEnum(field)` | Field is an enum compared via `fmt.Stringer` |
| `.Ref()` | Render this type as `"$ref"` wherever it's referenced |
| `.RenderProviders()` | Generate `RenderedSchema()` and run providers at runtime |

```go
var _ = polytype.Declare(Person.Schema)

var _ = polytype.Declare(Task.Schema).
    StringerEnum(Task{}.LogLevel)
```

Enum types and sealed unions are not declared here at all: a type with
`func (T) enum()` is an enum everywhere it appears, and an interface with an
unexported method is a union of the same-package structs that declare it.
The only per-union setting is its discriminator property, declared once with
`polytype.SealedUnion[I](name)` in the interface's own package.

These markers are no-ops at runtime — the generator reads them from the AST of
your build-tagged `schema.go`.

`NewJSONSchemaMethod`/`NewJSONSchemaFunc` with their remaining `With*`
options remain supported for source compatibility; each carries a `Deprecated:` godoc comment naming its
fluent equivalent. `NewJSONSchemaBuilder[T](fn)` (registers a no-argument
schema accessor stub) has no fluent form yet and is unaffected.

## 💻 CLI reference

```
polytype [gen] [options]     # generate (default subcommand)
  -target DIR          package to process (default: current directory)
  -pretty              pretty-print the .json output
  -no-changes          fail, writing nothing, if schemas or requested TypeScript output would change
  -force               rewrite even when unchanged (incompatible with -no-changes)
  --validate           generate validation methods for the selected formats
  --formats MODE       decoding and validation: json (default) or both
  --typescript DIR     generate structural TypeScript declarations in DIR
  --typescript-barrel  also generate index.ts type-only exports (requires --typescript)
```

Environment: `JSONSCHEMA_NO_CHANGES` (any non-empty value) ≡ `-no-changes`.

## 🏗️ Manual schema construction

When a statically generated schema won't cut it, build one with the helper
types from the `jsonschema` subpackage (see
[jsonschema/json_schema.go](jsonschema/json_schema.go)):

```go
import "github.com/tylergannon/polytype/jsonschema"

schema := &jsonschema.JSONSchema{
    Type:        jsonschema.Object,
    Description: "A user object",
    Properties: map[string]json.Marshaler{
        "username": jsonschema.StringSchema("User's username"),
        "age":      jsonschema.IntSchema("User's age"),
    },
    Strict: true, // all properties required + additionalProperties: false
}
```

Helpers: `StringSchema`, `BoolSchema`, `IntSchema`, `ArraySchema`,
`EnumSchema`, `ConstSchema`, `RefSchemaEl`, `UnionSchemaEl`.

`JSONSchema`'s map-based `Properties` marshal in alphabetical key order. When
you need properties emitted in a specific order (the whole point for LLM
prompting), use `ObjectSchema` and add fields with `AddProperty` /
`AddRequiredProperty` — it preserves insertion order.

## ⚠️ Limitations

- No map types, channels, functions, or inline interfaces
- No circular/recursive type references (detected and rejected)
- Registered interfaces support scalar fields and direct `[]I` fields, but not
  fixed arrays, nested/named slices, or Optional/Nullable interface slices
- External package types unsupported, except `time.Time` (rendered as a string
  with RFC3339 guidance)
- Max nesting depth: 100

## 🛠️ Development

```bash
git clone https://github.com/tylergannon/polytype.git
cd polytype
go build ./polytype
go test ./...
just lint    # task runner is `just`
```

All tests are plain `go test`. Two of them consult Node tooling when it is
present and skip otherwise: the TypeScript backend compiles its edge-case
output with `tsc`, and the devalue runtime compares against goldens recorded
from devalue 5.9 by `devalue/testdata/record`. `npm ci` at the repository root
installs both pinned packages.
