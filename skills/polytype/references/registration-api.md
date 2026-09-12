# Registration API and CLI Reference

Read this when a schema type needs more than flat structs: enums, unions,
custom discriminators, free functions — or when you need exact CLI flags.

## Enums

### Declare the enum on the type — `func (T) enum()`

Enum-ness is a property of the type. Add the marker method `func (T) enum() {}`
to the named type in ordinary (non-build-tagged) Go. Values are the typed
`const` declarations of that type in the same package, and every use of the
type — in every schema, codec, and TypeScript output — is an enum. There is no
field-level enum declaration:

```go
// types.go
type Status string

func (Status) enum() {}

const (
    StatusPending    Status = "pending"
    StatusInProgress Status = "in_progress"
    StatusCompleted  Status = "completed"
)

type Task struct {
    ID     string `json:"id"`
    Status Status `json:"status"`
}
```

```go
// schema.go (//go:build jsonschema)
var _ = polytype.Declare(Task.Schema)
```

Produces `"status": {"type": "string", "enum": ["pending", "in_progress", "completed"]}`.

The marker must be exactly `func (T) enum()` (value receiver, no parameters,
no results); anything else on a method named `enum`, or a marked type with
no typed constants, fails generation with a diagnostic naming the type. The
marker means value mode — a `String()` method on a marked type is ignored.

The generated file references every marked type in its package through its
first typed constant, as `var _ interface{ enum() } = StatusPending`, so the
marker is used from production code, its shape is checked by the compiler,
and `staticcheck` stays quiet with no lint directives. The right-hand side
must be a value of the marked type (a constant, not a pointer) because the
marker uses a value receiver. A package that declares a marked enum but never
runs generation (a shared enums package, say) needs one such line written by
hand per marked type.

### Integer (iota) enums — `StringerEnum`

A marked integer type emits its integers (`[0, 1, ...]`). `.StringerEnum` on
a field emits the **constant names** as string enum values
(`["LogDebug", "LogInfo", ...]`) for that field. Prefer the Stringer form for
LLMs — names carry meaning, integers don't:

```go
type LogLevel int

func (LogLevel) enum() {}

const (
    LogDebug LogLevel = iota
    LogInfo
    LogWarning
    LogError
)

var _ = polytype.Declare(Config.Schema).
    StringerEnum(polytype.Field[Config, LogLevel]("LogLevel"))
```

String mode generates codecs on the containing owner, composing with any
union fields. Marshal the owner value or pointer and decode into its pointer;
the same marked enum type still emits integers in any other field. Do not
add a global enum codec. Supported adapted fields are direct `E`, `Optional[E]`, and
`Nullable[E]`: absent/null wrappers bypass conversion, and present values use
constant identifiers. Unknown names/values, undeclared zero, ambiguous aliases,
custom enum JSON hooks, and other adapted containers are errors. Validate
external input before decoding for required-field and schema checks.
Registered enum fields cannot use `json:",string"`; generation rejects that
option before writing artifacts because its encoding differs from the schema.

Migration: `.Enum(field)`, `WithEnum(field)`, and the package-level
`NewEnumType[T]()` are removed. Add `func (T) enum() {}` next to the type and
delete those declarations; `.StringerEnum`/`WithStringerEnum` are unchanged.

## Discriminated unions (sealed interface fields)

A field whose type is a **sealed interface** becomes an `anyOf` union of the
interface's variants, discriminated by a `"type"` property whose value is the
concrete type name. An interface is sealed when its own body declares an
unexported method; its variants are inferred: every named struct type in the
same package that declares that method directly. The receiver of the sealing
method decides the variant kind: a value receiver is a value variant, a
pointer receiver is a pointer variant, and decoding constructs the variant
accordingly. Nothing is declared at the field. A direct one-dimensional slice
of the interface becomes an array with the union under `items.anyOf`. The
generator emits owner `MarshalJSON` and `UnmarshalJSON` by default. The default
discriminator property is `type`, JSON Schema property names are canonical,
and custom `UnmarshalJSON` hooks remain authoritative.

```go
// types.go
type PaymentMethod interface{ isPaymentMethod() } // sealed by the unexported method

type CreditCard struct {
    Number string `json:"number"`
    Expiry string `json:"expiry"`
}
func (CreditCard) isPaymentMethod() {}   // value variant, wire value "CreditCard"

type BankTransfer struct {
    AccountNumber string `json:"accountNumber"`
    RoutingNumber string `json:"routingNumber"`
}
func (*BankTransfer) isPaymentMethod() {} // pointer variant, wire value "BankTransfer"

type Payment struct {
    ID      string          `json:"id"`
    Methods []PaymentMethod `json:"methods"`
}
```

```go
// schema.go (//go:build jsonschema)
var _ = polytype.Declare(Payment.Schema)
```

Rejected with a diagnostic naming the type or field: a reachable interface
field whose interface is not sealed (non-sealed unions are unsupported; there
is no fallback), a sealing method obtained only by embedding another
interface, a type that satisfies the interface only through an embedded field
(excluded, distinct diagnostic), a direct candidate that does not implement
the complete interface, a sealed interface with zero variants, an embedded
interface payload, and a variant payload property that collides with the
discriminator property. Variants behind other build tags are not discovered.
Renaming a variant type changes its wire value and adding a qualifying
implementation changes membership: review schema diffs.

### Custom discriminator — `SealedUnion[I](name)`

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

Migration: `.Interface(...)`, `WithInterface`, `WithInterfaceImpls`,
`WithDiscriminator`, `Impl`, `Discriminator`, and `NewInterfaceImpl[I]` are
removed. Seal the interface with an unexported method declared directly on
every variant, delete the field-level declaration, and move a custom
discriminator property to one `SealedUnion[I](name)` declaration in the
interface's package.

The slice must be the direct field type. Fixed arrays, nested slices, named
slice containers, `Optional[[]I]`, and `Nullable[[]I]` are rejected during
generation. An `Optional[I]` scalar is supported; `Nullable[I]` is not.

## Full registration surface

`Declare(fn)` is the entry point; `fn` is a method expression (`T.Schema`) or
a free function taking `T` as its sole parameter. Chain options onto the
returned `*Declaration[T]`:

- `.Accessor(field, T.method)` / `.Method(field, T.method)` / `.Function(field, fn)`
  — provider options: supply a field's schema at runtime instead of deriving
  it statically (see [`examples/providers_rendering`](../../../examples/providers_rendering)).
- `.StringerEnum(field)` — emit an integer enum field's constant names.
  (Enum types themselves are declared with `func (T) enum()`, not here.)
- `.Ref()` — render this type as `"$ref"` wherever it's referenced (see below).
- `.RenderProviders()` — generate `RenderedSchema()` and run providers at
  runtime (advanced; rendered types get no `ValidateJSON` because their
  schemas depend on runtime values).

Package-level, not chained: `polytype.SealedUnion[I](name)` sets the
discriminator property of the sealed interface `I`, once, in `I`'s own
package.

`NewJSONSchemaMethod(T.Schema, ...opts)` / `NewJSONSchemaFunc(fn, ...opts)`
with their remaining `With*` options remain supported for source
compatibility.

Nested struct types are **inlined** into the parent schema (no `$ref`) by
default, so a shared Address struct appears in full wherever it is used —
unless that type is registered with `.Ref()`.

## Shared definitions (`$ref`/`$defs`) via `Ref`

Add `.Ref()` to a type's own registration to have it rendered as `"$ref":
"#/$defs/TypeName"` everywhere else it's referenced, instead of being inlined
at every call site. `$defs` are assembled per generated JSON file, keyed by
the type's bare name:

```go
// schema.go (//go:build jsonschema)
var _ = polytype.Declare(Shared.Schema).Ref()

var _ = polytype.Declare(Container.Schema) // references Shared
```

Notes:

- `.Ref()` only applies where `Shared` is referenced from *another*
  registered schema; `Shared`'s own top-level schema file is unaffected.
- Two distinct `.Ref()`'d types reachable in one generation run that share
  the same bare type name are a hard, generation-time error (`"AsRef
  definition name collision"`).
- Recursive/self-referencing `.Ref()`'d types are rejected, same as any
  other circular reference.

## Validation (`--validate`)

Opt in with `--validate` on generation and write a matching `ValidateJSON`
stub in the tagged file. Each registered type gets
`ValidateJSON([]byte) error`. Schemas are compiled once in `init()` using
`github.com/santhosh-tekuri/jsonschema/v6`. Failures return a
`*jsonschema.ValidationError` with `InstanceLocation` (path to the failing
field), `ErrorKind`, and nested `Causes`. Validation covers required fields,
types, unknown properties (rejected — `additionalProperties: false`), enum
membership, and nested structure. Validate LLM output *before* `json.Unmarshal`.
If a later generation command omits `--validate`, polytype refuses to remove an
existing generated method unless the command also passes `--force`.

## CLI reference

```bash
polytype                 # same as `gen` in the current package
polytype gen [flags]
  -pretty            # indent the .json output
  -target DIR        # package to process (default: cwd)
  -no-changes        # fail (writing nothing) if schemas or requested TypeScript output would change
  -force             # rewrite even when unchanged; incompatible with -no-changes
  --validate         # generate JSON validation methods
  --typescript DIR   # generate structural TypeScript declarations in DIR
  --typescript-barrel # also generate index.ts type-only exports; requires --typescript
```

Environment: `JSONSCHEMA_NO_CHANGES` (any non-empty value) is equivalent to
`-no-changes` — useful in hooks/CI without editing `//go:generate` lines. It
checks requested TypeScript artifacts as well as schemas.

When installed via the go.mod tool directive, invoke everything as
`go tool polytype ...`.

## Generated layout

```
mypackage/
├── types.go            # your types + //go:generate directive
├── schema.go           # your stubs + registrations (//go:build jsonschema)
├── jsonschema/         # generated schema files, one per registered type
│   └── Person.json
├── jsonschema_gen.go   # generated implementations (//go:build !jsonschema)
└── web/src/generated/  # requested structural TypeScript output
    ├── types.ts
    └── index.ts        # only with --typescript-barrel
```

Generate validation and TypeScript declarations together with
`--validate --typescript web/src/generated`; add `--typescript-barrel` when an
`index.ts` type-only export is useful. Sealed interface fields and
`.StringerEnum` select the containing Go struct's JSON codecs automatically. TypeScript output
does not include a runtime decoder or validator: applications must validate
untrusted TypeScript-side data, and Go consumers should call generated
`ValidateJSON` before `json.Unmarshal`. Pin the tool and imported package to the
same explicit module release.

## Limitations and debugging

Not supported: map types, channels, functions, inline interfaces, recursive or
circular type references (detected and rejected), unsupported registered-
interface containers (fixed arrays, nested/named/optional/nullable slices), and
external package types other than `time.Time` (rendered as a string with RFC3339
guidance). Max nesting depth 100.

If generation fails:

1. Every type referenced in `schema.go` must exist in the package's Go source.
2. Check the build tag is exactly `//go:build jsonschema` on `schema.go`.
3. Look for circular references between types.
4. Enum consts must be declared in the same package as the enum type, and
   the type must declare `func (T) enum() {}`.
