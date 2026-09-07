---
title: Enums
description: Generate JSON Schema enum values from Go constants.
---

## Declare an enum on the type

Enum-ness is a property of the type, not of any field that uses it. A named
type declares itself as an enum with the marker method `func (T) enum() {}`
in ordinary, non-build-tagged Go. Its values are the typed constants declared
in the same package. Every use of the type, in every generated schema, Go
codec, and TypeScript output, is then an enum; no field-level declaration is
needed and none exists.

```go
type Status string

func (Status) enum() {}

const (
    StatusPending Status = "pending"
    StatusDone    Status = "done"
)

type Task struct {
    Status  Status   `json:"status"`
    History []Status `json:"history"`
}

// schema.go (//go:build jsonschema)
var _ = polytype.Declare(Task.Schema)
```

The marker must be exactly `func (T) enum()`: a value receiver, no
parameters, no results. A pointer receiver or any other signature on a method
named `enum` is a generation error naming the type, as is a marked type with
no typed constants. The marker means value mode: a `String()` method on a
marked type is ignored.

The generated file references every marked type in its package through its
first typed constant, as `var _ interface{ enum() } = StatusPending`, so the
marker is used from production code, its shape is checked by the compiler,
and `staticcheck` stays quiet with no lint directives. The right-hand side
must be a value of the marked type (a constant, not a pointer) because the
marker uses a value receiver. A package that declares a marked enum but never
runs generation (a shared enums package, say) needs one such line written by
hand per marked type.
Only marked types declared in the package generation runs against receive the
generated value-mode `MarshalJSON`/`UnmarshalJSON` pair, so a marked enum
imported from another package is not membership-guarded on the wire.

## Integer and iota constants

A marked integer type emits its raw numeric constant values. To emit the
constant names such as `LogDebug` and `LogInfo` as strings instead, add
`.StringerEnum(field)` to the containing schema declaration. It does not emit
the return values of `String()`.

```go
type LogLevel int

func (LogLevel) enum() {}

const (
    LogDebug LogLevel = iota
    LogInfo
    LogError
)

type Config struct {
    LogLevel LogLevel `json:"logLevel"`
}

var _ = polytype.Declare(Config.Schema).
    StringerEnum(Config{}.LogLevel)
```

`.StringerEnum` also works on an unmarked integer type; the marker is only
needed for the fields that should carry integer values.

## Encode and decode string mode

Generation adds one value `MarshalJSON` and pointer `UnmarshalJSON` to the
containing owner. These methods compose string-mode enum fields with any union
fields. They use constant identifiers, so `LogInfo` becomes `"LogInfo"` even
when a `String()` method returns different text. String mode is per field:
another field of the same marked type remains numeric, guarded only by the
type-level value-mode codec described above.

Supported adapted fields are direct integer-backed `E`, `Optional[E]`, and
`Nullable[E]`. Optional absence is omitted; Nullable null remains null. Present
values must match a declared constant. Decode errors leave the owner unchanged.
Validate external JSON first to enforce required fields and schema membership.

Unknown wire names and undeclared Go values are errors, including zero when
there is no zero-valued constant. Duplicate underlying values with different
names are ambiguous and rejected before generation writes files. Keep one
canonical constant name per value for string mode, or retain numeric mode.
Custom JSON hooks on adapted enum types and adapted pointers/slices or other
containers are rejected; move conversion to a supported named owner field.
String-mode enum fields cannot use `json:",string"`; generation rejects that
option before writing artifacts because its encoding differs from the schema.

In the example below, `ApplicationConfig` uses string mode while `Task`
intentionally uses numeric mode for the same enum types. Renaming constants
changes the string-mode wire contract and requires compatibility review.

## Migration

`Declare(T.Schema).Enum(field)`, `WithEnum(field)`, and the package-level
`NewEnumType[T]()` are removed. Add `func (T) enum() {}` next to the enum
type and delete those declarations. `.StringerEnum` and `WithStringerEnum`
are unchanged.

See the compiling [`examples/stringer_enums`](https://github.com/tylergannon/polytype/tree/main/examples/stringer_enums)
package for a complete example.
