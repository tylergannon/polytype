# Issue 28 implementation plan: explicit field-value semantics

Status: reconstructed design plan; no feature implementation has begun.

Source: [GitHub issue #28](https://github.com/tylergannon/polytype/issues/28),
"Optional[T]: value-type optional fields with presence detection."

Decision brief:
[issue-28-optionality-decision-brief.md](issue-28-optionality-decision-brief.md)

Independent review:
[issue-28-fable-review.md](issue-28-fable-review.md).

## Proposed outcome

Represent the three JSON property contracts explicitly in Go:

| Go field | Required | Nullable | No-value representation |
|---|---:|---:|---|
| `T` | yes | no | none |
| `polytype.Optional[T]` | no | no | property absent |
| `polytype.Nullable[T]` | yes | yes | property present as `null` |

Decision: implement both wrappers and let authors choose the correct protocol
semantics. The user approved this direction after reviewing our analysis and
Fable's independent prescription.

Delete the legacy `jsonschema:"optional"` behavior rather than deprecating it.
This is an intentional breaking change: field semantics should live in the Go
type system, with no compatibility branch silently preserving an ambiguous
older mechanism.

Both wrappers should accept `T any`. Wrapper support during schema generation
is exactly the support the ordinary renderer already provides for `T`; neither
wrapper creates a second inner-type policy or turns unsupported types into
permissive schemas.

## Prerequisites

### Fix exported-field traversal

[GitHub issue #29](https://github.com/tylergannon/polytype/issues/29)
proves that `internal/syntax.skipField` currently skips exported fields and
traverses unexported fields. Fix it before adding generic traversal.

Required proof:

- the issue's exported/unexported visibility unit test;
- grouped field-name and skip-tag tests;
- a cross-package generated schema showing an exported remote field renders
  its real schema rather than `{}`;
- `go test ./...` green before Optional or Nullable changes begin.

The same implementation unit must also correct the adjacent registered-
interface inversion proven in
[this #29 follow-up](https://github.com/tylergannon/polytype/issues/29#issuecomment-4937428864).
Fixing exported-field traversal alone activates that dormant failure, so #29
must not merge with only the visibility assertion repaired.

### Fix omitted JSON-tag names

[GitHub issue #30](https://github.com/tylergannon/polytype/issues/30)
proves that `json:",omitzero"` currently renders a schema property named `""`
instead of falling back to the Go field name. Fix it before documenting that
tag as the Optional idiom.

### Repair fresh-clone generation

A fresh clone currently requires targeted generation before two example test
packages compile, while `go generate ./...` fails on an obsolete `-type` flag
inside a syntax fixture. Repair or separately track both problems before the
feature's final proof. Do not hide them by relying on ignored local artifacts.

## Previously validated AST and IR spike

A throwaway spike exercised decorated fields whose generic arguments were a
scalar, named struct, slice, pointer, inline struct, map, registered interface,
and ref target.

It established:

- a one-argument instantiation is a `*dst.IndexExpr`;
- its base is a `*dst.Ident` carrying the canonical import path even through an
  aliased import;
- `IndexExpr.Index` retains the original inner AST node without flattening;
- manually normalizing to the inner expression lets the existing renderer
  produce its ordinary property, object, array, pointer-object, inline-object,
  registered-union, and ref-target IR;
- unsupported maps retain their existing error;
- registered interfaces require extra generated-unmarshaler IR because current
  discovery only accepts a raw direct identifier.

The schema path is therefore a normalization problem, not a reason to rebuild
the IR around generics. Interface code generation is the explicit exception.

## Runtime contracts

### `Optional[T any]`

Add `optional.go` in the root package:

```go
type Optional[T any] struct {
	Present bool
	Value   T
}
```

Behavior:

- the zero Optional is absent;
- `IsZero()` returns `!Present`;
- a containing struct uses `json:",omitzero"` to omit absence;
- `MarshalJSON` encodes the concrete `Value` only when the field is present in
  its containing object;
- marshaling an absent Optional directly returns an error that points to the
  required `json:",omitzero"` field tag;
- `UnmarshalJSON` rejects input `null`;
- decoding uses a temporary `T` and assigns only after success;
- if a present value serializes as `null`, marshaling fails so `Present` cannot
  contradict the generated non-null schema;
- no constructors or accessor methods are required in the first change.

Once the field classifier recognizes Optional, generation must fail when its
JSON tag lacks `omitzero`. This is a cheap local check and prevents a real wire-
format bug: a supposedly absent struct-valued wrapper otherwise reaches its
marshaler instead of disappearing from the containing object. `omitempty`
alone is insufficient for a struct under `encoding/json` v1.

### `Nullable[T any]`

Add `nullable.go` in the root package:

```go
type Nullable[T any] struct {
	Present bool
	Value   T
}
```

Behavior:

- the zero Nullable represents explicit JSON null;
- `MarshalJSON` returns `null` when `Present` is false;
- non-null values marshal as ordinary `T`;
- `UnmarshalJSON(null)` clears `Value` and `Present`;
- non-null decoding uses a temporary and sets `Present` only after success;
- a present value that serializes as null fails instead of collapsing states;
- `IsZero()` returns false, defensively preventing `omitzero` from removing a
  schema-required property;
- the struct field must not rely on omission tags.

Do not make Nullable omission tags a hard generation error in v1. With the
contract above, `omitzero` is neutralized by `IsZero() == false`, and
`omitempty` does not omit struct values under `encoding/json` v1. Document
that the tags are misleading and prove the required property still emits;
reserve a lint or hard error for evidence of an actual bug.

## Usable examples

### Omittable field

```go
type Node struct {
	ID string `json:"id"`

	// Maximum retry attempts. Zero disables retries; omit to inherit a default.
	MaxRetries polytype.Optional[int] `json:"max_retries,omitzero"`
}
```

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "max_retries": {"type": "integer"}
  },
  "required": ["id"],
  "additionalProperties": false
}
```

Absent input leaves `Present == false`; `{"max_retries":0}` sets
`Present == true` and preserves zero.

### Strict-compatible nullable field

```go
type Node struct {
	ID string `json:"id"`

	// Maximum retry attempts. Null means inherit a default; zero disables retries.
	MaxRetries polytype.Nullable[int] `json:"max_retries"`
}
```

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "max_retries": {"type": ["integer", "null"]}
  },
  "required": ["id", "max_retries"],
  "additionalProperties": false
}
```

`{"max_retries":null}` leaves `Present == false`;
`{"max_retries":0}` sets `Present == true` and preserves zero.

### Composite values

```go
type RetryPolicy struct {
	Limit int `json:"limit"`
}

type Options struct {
	Tags      polytype.Optional[[]string]    `json:"tags,omitzero"`
	Policy    polytype.Nullable[RetryPolicy] `json:"policy"`
	PolicyPtr polytype.Nullable[*RetryPolicy] `json:"policy_ptr"`
}
```

The exact nullable schema encoding depends on the rendered inner node:

- directly typed uncomplicated scalar nodes use `"type":[T,"null"]`;
- inlined structs/objects use `anyOf` with a null branch;
- nullable arrays/slices, consts, enums, and explicit refs are deliberately
  rejected in v1 rather than supported merely because the IR could express
  them;
- nullable registered interfaces remain a separate decision.

## Syntax normalization

Add one `internal/syntax` helper that recognizes only canonical library types:

```text
github.com/tylergannon/polytype.Optional[T]
github.com/tylergannon/polytype.Nullable[T]
```

Return the original inner expression plus an enum describing required,
omittable, or nullable semantics. Exact package matching supports aliased
imports while excluding unrelated types named Optional or Nullable.

Teach `ScanResult.resolveTypeExpr` to recurse into the inner expression for
those two wrappers only. Other unsupported generic field types retain clear
errors. Implement this after issue #29 so tests prove exported generic fields
actually reach the new case.

Decision: v1 recognizes either wrapper only when it is the complete type of a
direct, named struct field. Reject wrapper roots, embedded wrappers,
alias/defined wrapper types, wrappers nested inside other wrappers, and
wrappers placed inside containers. An Optional field may still have a
container as its inner `T`; the wrapper itself is what must occupy the direct
field position.

## Builder and schema IR

Replace the conceptual `ObjectProp.Optional bool` boundary with explicit
property-value semantics, or otherwise separate these two independent facts:

1. Does the containing object's `required` array include this property?
2. Does the property's value schema accept null?

The mapping is:

- ordinary `T`: required, ordinary inner schema;
- `Optional[T]`: not required, ordinary inner schema;
- `Nullable[T]`: required, nullable inner schema.

Every path in `renderStructField` must consume the same normalized field:

- explicit refs;
- v1 enum configuration and auto-discovery;
- interface configuration;
- provider/template holes;
- ordinary render fallback;
- descriptions and property ordering.

Do not special-case Optional or Nullable separately inside each branch.

## Nullable schema transformation

Add an explicit null schema node and one transformation over an already
rendered inner schema.

For uncomplicated nodes with a direct `type`, the compact result is preferred:

```json
{"type":["integer","null"]}
```

For an inlined object, use a standards-correct union:

```json
{
  "anyOf": [
    {"type":"object","properties":{/* inlined fields */}},
    {"type":"null"}
  ]
}
```

Preserve descriptions, field order, and type identity. Do not implement the
compact form through byte replacement of serialized JSON; represent it in IR.

The JSON Schema standard permits type arrays for object and array as well as
scalars, but schema expressibility alone does not justify a nullable Go
contract. Narrow v1 to cases with a concrete protocol meaning:

- support nullable primitive/scalar values and inlined structs/objects;
- reject nullable arrays and slices; callers use an empty container for no
  elements;
- reject nullable const fields as semantically meaningless;
- defer nullable enums until a motivating protocol requires "one enum value or
  no value";
- reject combining Nullable with the explicit legacy `jsonschema:"ref=..."`
  escape hatch; ordinary nested structs are inlined and do not generate refs;
- decide nullable registered-interface unions separately at the generated-
  interface review boundary.

This is a generator support policy, not a Go generic constraint: the public
runtime type remains `Nullable[T any]`.

## Integer and float parity

The ordinary renderer already covers signed and unsigned integer widths and
both float widths, but its scanner/renderer lists are inconsistent for
`byte`, `rune`, and `uintptr`. Close those gaps for ordinary and wrapped fields
and render them as JSON Schema `integer`.

Finite floats use `number`. Preserve `encoding/json` errors for NaN and
positive or negative infinity.

## Registered-interface unmarshaler IR

Normalize a direct wrapper before `resolveLocalInterfaceProps` checks for a
registered interface. Continue rejecting interfaces nested in unsupported
locations such as slices of interfaces.

Extend `InterfaceProp` and `schemas.go.tmpl` with only the presence/null mode
needed for assignment:

- ordinary interface behavior remains byte-for-byte stable;
- Optional interface: nil raw bytes mean absent; null and invalid
  discriminators fail; a successful implementation assigns `.Value` and only
  then sets `.Present`;
- Nullable interface: raw null clears presence; a successful implementation
  assigns `.Value` then `.Present`; missing raw bytes remain distinguishable to
  the generated method even though schema validation is responsible for
  rejecting the missing required property.

On every error, leave the destination unmodified. Test value and pointer
implementations and every interface registration path retained by the repo.

## Delete the legacy optional tag

Remove all interpretation of `optional` from `jsonschema` struct tags:

- delete `StructField.Required` if unused;
- remove `JSONSchemaTag.Optional` and its parser branch;
- remove or rewrite legacy tests;
- migrate scalar and composite fixtures to the explicit wrapper types;
- preserve `ref`, provider, and other tag behavior;
- remove current-support claims from README, the installable skill,
  examples, and internal documentation;
- do not warn or keep a deprecation branch. A leftover old tag has no
  optionality effect and release notes must call that out as breaking.

## Examples and agent skill

Add a runnable `examples/optionality/` package with:

- ordinary, Optional, and Nullable fields;
- scalar zero, float, named scalar, zero struct, pointer, and Optional
  slice/array examples;
- rejected nullable-shape examples as focused generator tests rather than
  non-buildable runnable examples;
- registered-interface examples only for the modes approved at the interface
  review boundary;
- checked-in schema and Go accessors;
- runtime tests for missing, null, present-zero, and present-empty values;
- generated validation tests for all three semantics;
- an OpenAI-strict-compatible object that uses no omittable fields.

Update `skills/polytype/SKILL.md` and add an expanded reference so
another agent can use the feature without reading implementation code. Cover:

- the three semantic cases and a selection table;
- `json:",omitzero"` for Optional and its prohibition for Nullable;
- read, write, marshal, and unmarshal examples;
- scalar and supported composite inner types;
- explicit errors for deferred nullable array, enum, const, and ref shapes;
- registered-interface behavior approved at its review boundary;
- null invariants and error behavior;
- OpenAI strict requirements and the fact that one Optional field makes its
  containing object non-strict;
- closeout searches that remove the old tag from Go source.

Apply the same public contract to README and the installable skill.

## Implementation order

1. Fix issue #29, including its registered-interface follow-up, with independent
   regression and cross-package proof.
2. Fix issue #30's omitted JSON-tag name handling.
3. Repair the fresh-clone and repository-wide generation baseline.
4. Add runtime `Optional[T any]` and `Nullable[T any]` with exhaustive tests.
5. Add the canonical syntax classifier, direct-field placement checks, and AST
   tests.
6. Enforce `omitzero` on Optional fields.
7. Introduce explicit property-value semantics in builder IR.
8. Render ordinary, omittable, and nullable scalar/inlined-object schemas;
   reject the deliberately deferred nullable shapes with actionable errors.
9. If approved, extend registered-interface discovery and generated
   unmarshaling behind an
   explicit review boundary.
10. Delete the legacy tag and migrate fixtures.
11. Add examples, update the agent skill and public docs, regenerate, and run
    all closeout gates.

Keep steps 1 and 9 as explicit review boundaries. They contain the proven
traversal defect and the remaining generated-code risk.

## Proof of work

### Runtime tests

For Optional:

- zero is absent and `IsZero` is true;
- present scalar zero, zero struct, empty non-nil slice/map, array, pointer,
  named types, and custom JSON types round-trip;
- `omitzero` omits absence and emits present-zero;
- input null, invalid JSON, wrong type, and overflow fail without mutation;
- present nil or custom marshaled null fails;
- every integer width, both float widths, bool, string, and representative
  composite types compile and round-trip.

For Nullable:

- zero marshals to null and `IsZero` is false;
- null clears presence and value;
- present-zero and present-empty values round-trip;
- accidental `omitzero` still emits the required null property;
- invalid input fails without mutation;
- present nil or custom marshaled null fails;
- missing-key behavior is documented and separately caught by validation.

### Syntax and IR tests

- canonical wrappers work through normal and aliased imports;
- local or third-party same-named generics are not recognized;
- only direct named struct-field wrapper placement is accepted;
- inner scalar, named, inline struct, array/slice, pointer, map, interface, and
  ref AST nodes are returned unchanged;
- unsupported inner types keep the ordinary error;
- descriptions, field order, discriminator data, and type identity survive;
- direct wrapper mode maps to requiredness and nullability exactly once;
- byte, rune, and uintptr parity is covered.

### Schema encoding tests

- direct primitive nullable nodes use compact type arrays;
- inlined nullable objects use `anyOf` with null;
- nullable array/slice, const, enum, and explicit-ref combinations fail with
  actionable generator errors;
- registered-interface nullable behavior is tested only if separately approved;
- every nested object remains `additionalProperties:false` and fully required
  in the strict-compatible fixture;
- root objects never become anyOf;
- property order is unchanged.

### Generated interface tests

- ordinary generated code is unchanged;
- Optional absent/present/error transitions are correct;
- Nullable null/present/error transitions are correct only if that mode is
  separately approved;
- missing, null, malformed, and unknown discriminators do not partially
  mutate fields;
- generated code builds for value and pointer implementations.

### End-to-end fixtures

Add a successful fixture proving:

- ordinary, Optional, and Nullable coexist;
- missing Optional is valid and null Optional is invalid;
- present null Nullable is valid and missing Nullable is invalid;
- present zeros and empty composites preserve `Present`;
- the strict-compatible schema uses only required ordinary/Nullable fields;
- generated schema, accessors, validation, and unmarshaling compile and run.

Use separate negative fixtures or focused builder tests for unsupported inner
types so the successful fixture remains buildable.

### OpenAI compatibility proof

At minimum, validate generated strict schemas against the documented supported
subset. Prefer a live API probe or a repository-owned verifier for:

- compact nullable primitives;
- nullable inlined object via anyOf;
- nested object requiredness and `additionalProperties:false`;
- rejection of an omittable Optional field in strict mode.

Record model/API surface and date because supported subsets can change.

### Repository closeout

Run after implementation and generation repairs:

```bash
gofmt -w <changed Go files>
go generate ./...
JSONSCHEMA_NO_CHANGES=1 go generate ./...
if rg -n 'jsonschema:"[^"`]*optional' --glob '*.go' .; then exit 1; fi
git diff --check
go test ./...
```

The work is complete only when a fresh clone can generate, generation is
idempotent, generated artifacts are tracked where required, all examples and
the installable skill describe the same contract, and `go test ./...` is green.

## Deliberate non-goals

- No compatibility period for `jsonschema:"optional"`.
- No convenience constructors or accessor methods in the first change.
- No silent support for maps, channels, unregistered raw interfaces, or other
  ordinary-generator limitations.
- No accidental three-state nested-wrapper API.
- No claim that Nullable alone makes an arbitrary schema OpenAI-strict.
- No blanket promise that every schema IR node can be wrapped in Nullable.
