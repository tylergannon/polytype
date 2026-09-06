# Next step

Milestone 1 (#106, enum encode-side membership) — nothing implemented yet.
Baseline `go test ./...` passes on 85c8cb0.

## Step

Emit type-level JSON codecs for locally declared `enum()`-marked types.

- Carry each enum-marked type's members into the template. `EnumMarkers`
  (`internal/builder/gen_schema.go`, `schemaTemplateData`) already holds
  exactly the right set — every type in the generation target package
  declaring `func (T) enum()` — with its first constant. Extend `EnumMarker`
  with the type's underlying kind (string or integer) and its member
  constant names, taken from the same resolution the schema rendering uses
  (`discoverEnum` / `syntax.ResolveEnum`).
- In `internal/builder/schemas.go.tmpl`, emit for each marker a value-receiver
  `MarshalJSON` and a pointer-receiver `UnmarshalJSON` that accept only
  members and otherwise return an error naming the type and the value.
  Value mode: the wire form is the constant's own value (string or number),
  not its Go identifier.
- Leave `.StringerEnum` owner codecs untouched.

Scope stops here: no fixture/consumer round-trip proof, no regeneration-clean
test, no coexistence test, no spec or enum-guide amendment. Those are later
steps of this same milestone.

## Demonstrated by

A new test in `internal/builder` that generates a fixture package with a
string enum and an integer enum (reuse `writeEnumCodecFixture` from
`enum_codec_test.go`), then asserts the generated `jsonschema_gen.go`
contains both methods for both types and that the emitted rejection path
names the type. Plus `go test ./...`, `go vet ./...`, `just build-tagged`.
