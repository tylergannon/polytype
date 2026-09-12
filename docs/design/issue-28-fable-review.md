# Issue 28 independent review (Fable)

Status: independent prescription; no implementation performed.
Date: 2026-07-10. Reviewer: Claude Fable 5.

Inputs: [decision brief](issue-28-optionality-decision-brief.md),
[implementation plan](issue-28-optional-plan.md),
[issue #28](https://github.com/tylergannon/polytype/issues/28),
[issue #29](https://github.com/tylergannon/polytype/issues/29),
a fresh read of the repository at commit `79576b1`, and primary external
sources fetched 2026-07-10.

Method note: every internal claim below cites a file and line I read
directly. External claims cite the source that states them. Facts I could
not verify are labeled UNVERIFIED and carried into the proof gates rather
than assumed.

## 1. Executive answers to the ten questions

1. **Ship both** `Optional[T any]` and `Nullable[T any]`. The two wire
   contracts are semantically disjoint and both have first-class consumers:
   Nullable is the only strict-compatible form for OpenAI, and Anthropic's
   new strict tool-use also derives requiredness from nullability, while
   Optional remains correct for non-strict schemas and human-facing
   configs. Neither is a "better default"; the docs should steer by target
   (strict LLM output → `Nullable`, everything else → prefer plain `T`,
   then `Optional`).
2. **Keep the names and `{Present, Value}` fields.** `Nullable` beats
   `StrictOptional` for the reason the brief gives. Go's own precedent
   (`database/sql.Null[T]{V, Valid}`, Go 1.22+) confirms the wrapper-struct
   shape; `Present`/`Value` are clearer names than `Valid`/`V`.
3. **Compact `"type": [T, "null"]` for plain scalar nodes only in v1**;
   `anyOf` everywhere else. This must be an implementation boundary inside
   the nullable transform, not a public type constraint. The standard
   permits compact arrays for object/array too, but OpenAI acceptance of
   those forms is unverified and the hand-rolled marshalers make them the
   costly case for no proven benefit.
4. **`anyOf` is mandatory for refs** (OpenAI rejects sibling keywords next
   to `$ref` — see §3.3), **mandatory-in-practice for existing unions**
   (flatten a null branch in), and **recommended for enums/consts** pending
   a live probe (§3.2 explains why the "obvious" enum encoding is a trap).
5. **Yes to the presence-mode classifier**, with a sharper boundary than
   the brief draws: classify exactly once at the struct-field seam,
   *before* any of `renderStructField`'s branches run, and restrict v1
   wrapper recognition to direct struct-field type position. Requiredness
   stays on `ObjectProp`; nullability lives entirely in the value schema
   via one transform. Two small IR additions are unavoidable (§5).
6. Runtime invariants: as proposed, plus two additions — marshaling an
   absent `Optional` should **return an error** (it fires exactly when the
   author forgot `omitzero`), and the **generator should enforce the tag
   contract** (`omitzero` required on Optional fields, forbidden on
   Nullable fields) so the failure is caught at generation time, not in
   production (§4.3).
7. Strict-compatibility checking should be a **separate, later generator
   option**; do not block issue #28 on it. But the encodings chosen now
   must keep a pure `T`+`Nullable` object inside the verified strict
   subset, and the docs must state the pre-existing root-union caveat (§7).
8. The plan's ten-step order is sound. Two scope corrections: step 1
   (issue #29) is larger than its issue text implies because the reversed
   filter has been masking further dormant bugs that will activate with
   the fix (§6.1), and tag validation deserves its own early step (§8).
9. The brief missed a set of concrete failure modes, the two most
   important being the OpenAI-docs-vs-validator enum conflict (§3.2) and
   the `json:",omitzero"` empty-property-name bug (§6.3). Full list in §6.
10. Proof gates: endorse the plan's list; add a validator divergence test,
    a recorded live OpenAI probe matrix, tag-enforcement tests, and a
    fresh-clone gate (§9).

## 2. Verified facts versus inference

### 2.1 External facts (all verified against primary sources, 2026-07-10)

JSON Schema (2020-12 spec + json-schema.org reference):

- `required` asserts key presence only; a present `null` satisfies
  `required` but must still satisfy the property's value schema
  ([object reference](https://json-schema.org/understanding-json-schema/reference/object),
  [2020-12 validation §6.5.3](https://json-schema.org/draft/2020-12/json-schema-validation)).
- `null` is a type, not absence
  ([null reference](https://json-schema.org/understanding-json-schema/reference/null)).
- `type` accepts an array of any of the seven type names, including
  `object` and `array`; valid if the instance matches any
  ([validation §6.1.1](https://json-schema.org/draft/2020-12/json-schema-validation)).
  The 2020-12 core spec itself uses `{"type": ["string","null"]}` as its
  motivating example.
- `anyOf`: valid against one or more subschemas
  ([combining reference](https://json-schema.org/understanding-json-schema/reference/combining#anyof)).
- **Keywords apply conjunctively and independently** (2020-12 core §10.1
  "Keyword Independence"). Therefore `{"type":["string","null"],
  "enum":["a","b"]}` **rejects `null`**: `null` passes `type` but fails
  `enum`. To admit null you must add `null` to the enum array or
  restructure with `anyOf`. This is the load-bearing fact behind §3.2.
- In 2020-12, keywords may appear alongside `$ref` and they apply
  (core §8.2.3.1) — unlike draft-07, where siblings were ignored.
- `github.com/santhosh-tekuri/jsonschema/v6` (repo pins v6.0.2,
  `go.mod:7`) supports drafts 4/6/7/2019-09/2020-12 and **defaults to
  2020-12 when `$schema` is absent** (`draft.go:124` in the module).
  Generated schemas carry no `$schema`, so `ValidateJSON` validates under
  strict 2020-12 semantics.

OpenAI Structured Outputs
([supported schemas](https://developers.openai.com/api/docs/guides/structured-outputs#supported-schemas);
the old platform.openai.com URL 301-redirects here):

- "To use Structured Outputs, all fields or function parameters must be
  specified as `required`."
- "…it is possible to emulate an optional parameter by using a union type
  with `null`", canonically shown as `"type": ["string", "null"]`.
- Supported types: String, Number, Boolean, Integer, Object, Array, Enum,
  anyOf. `oneOf` appears nowhere on the page. `allOf`, `not`,
  `if/then/else`, `dependent*` are explicitly unsupported. `const` is not
  in the supported list but is implied by the limits text ("…and const
  values") — explicit support UNVERIFIED.
- "Root objects must not be `anyOf` and must be an object."
- "`additionalProperties: false` must always be set in objects."
- Every `anyOf` branch must itself comply with the subset.
- Nullable recursion is shown as
  `"anyOf": [{"$ref": "#/$defs/linked_list_node"}, {"type": "null"}]` —
  so non-object branches like `{"type":"null"}` are sanctioned.
- Limits: ≤5000 properties, ≤10 nesting levels, ≤120k chars of names +
  enum/const values, ≤1000 enum values total; key order of output follows
  schema property order.
- Function calling with `strict: true` states the same two constraints
  ("all fields required", `additionalProperties: false`), and neither page
  claims the subsets differ between response-format and function modes.
- **OpenAI rejects sibling keywords next to `$ref`** (e.g. a
  `description`) even though 2020-12 permits them. This is observed,
  widely-reported API validation behavior, not doc text
  ([community report](https://community.openai.com/t/allow-annotation-only-keywords-like-description-title-and-examples-for-ref-keywords-when-using-json-structured-output/1374904));
  treat as true but re-verify in the live probe.

Anthropic (for calibration, briefly): current strict tool-use /
structured-outputs requires `additionalProperties: false`, derives
requiredness as "all non-nullable properties become required", supports
`anyOf` and type arrays **with a documented cap of 16 parameters using
anyOf/type arrays**, and does not support recursive schemas. Non-strict
tool schemas remain plain JSON Schema where optional = absent from
`required`. So `Optional` has a real LLM consumer too, and heavy
`Nullable` usage has a documented ceiling on one major provider.

Go standard library (verified against go1.26.5 docs + empirical runs):

- `omitzero` (Go 1.24+): field omitted when zero; **an `IsZero() bool`
  method, if present, is authoritative in both directions** — a
  non-zero-valued struct with `IsZero()==true` is omitted, and a
  zero-valued struct with `IsZero()==false` is emitted. Empirically
  confirmed.
- `omitempty` never omits struct-typed fields (structs are not in the
  closed "empty" list). `Nullable.IsZero() == false` is therefore a
  defense against `omitzero` only; `omitempty` on a Nullable field is
  already inert under encoding/json v1.
- `UnmarshalJSON` **is** called for JSON `null` ("Unmarshal calls that
  value's Unmarshaler.UnmarshalJSON method, including when the input is a
  JSON null" — current doc wording; the older "no-op by convention"
  sentence was removed in Go 1.24). It is **never** called for absent
  keys (empirically confirmed). So `Optional` can reject null, `Nullable`
  can accept it, and absence is representable only by the zero value —
  the proposed contracts are implementable exactly as specified.
- `encoding/json/v2` is still experimental (GOEXPERIMENT-gated) in Go
  1.26; v1 semantics above are unaffected. Note for the future: v2
  redefines `omitempty` in JSON terms (a struct marshaling to `null`
  or `{}` would be omitted), which makes "never rely on omitempty for
  Nullable" worth stating in the docs now.
- One-type-argument generic instantiation is `*ast.IndexExpr` /
  `*dst.IndexExpr`; two or more is `IndexListExpr` (go/ast docs +
  empirical parse; dave/dst v0.27.3 mirrors this).

### 2.2 Internal facts (verified firsthand in this checkout)

- `internal/syntax/scan_result.go:587-590` — `skipField` returns true when
  **no** name is lowercase, i.e. it skips exported fields and traverses
  unexported ones. Issue #29 is real, exactly as filed.
- `internal/syntax/scan_result.go:498-565` — `resolveTypeExpr` has no
  `*dst.IndexExpr` case; a generic field type falls to the default
  "unhandled expression" error (loud, not silent). The `*dst.SliceExpr`
  case at line 504 is dead code (`SliceExpr` is the slice *expression*
  `a[1:2]`, never a type; `ArrayType` covers `[]T`).
- `internal/syntax/scan_result.go:523-530` — a second inversion, adjacent
  to #29: for a local ident that is not a `LocalNamedTypes` entry and not
  a constant, `if _, ok := r.Interfaces[expr.Name]; !ok { return nil }`
  silently accepts **truly undeclared names** and then errors with
  "undeclared local type" precisely when the name **is** a registered
  interface. See §6.1 for why #29's fix detonates this.
- **Spike re-verification (fresh, this session):** using the repo's own
  loader (`syntax.Load` → `decorator.Load`) on a throwaway fixture with an
  aliased import, all of `Optional[int]`, `Optional[Inner]`,
  `Optional[[]string]`, `Optional[*Inner]` arrived as `*dst.IndexExpr`
  whose `X` is a `*dst.Ident` with `Name="Optional"` and `Path` set to the
  **canonical package path despite the alias**, and whose `Index` retained
  the ordinary inner AST shape (`*dst.Ident`, `*dst.ArrayType`,
  `*dst.StarExpr`). The plan's central AST premise holds. (Fixture and
  test were deleted after the run.)
- `internal/builder/model.go:31-35` — `ObjectProp` carries one
  `Optional bool`; `model.go:181-196` derives `required` from it.
  `model.go:197` — every object already emits
  `"additionalProperties":false`.
- `internal/builder/model.go:52-58, 224-265` — `PropertyNode.Typ` is a
  single string marshaled directly; `Enum []T` is generically typed and
  cannot hold a JSON null. Type arrays and null-bearing enums both require
  IR changes, not just a flag.
- `internal/builder/model.go:72-76, 358-381` — `UnionTypeNode.Options` is
  `[]ObjectNode` with discriminator prepending. It **cannot host a
  `{"type":"null"}` branch**; a general-purpose anyOf node is needed.
- `internal/builder/model.go:89-91` — `RefNode` marshals a bare
  `{"$ref":…}` with no siblings (fortunately compatible with OpenAI's
  observed `$ref` strictness).
- `internal/builder/gen_schema.go:1041-1184` — `renderStructField` has
  five schema sources in priority order: `jsonschema:"ref=…"` tag →
  field-level enum config (**requires the raw field type to be a direct
  `*dst.Ident`**, line 1060) → field-level interface config → provider
  template hole → `renderSchema` fallback. Requiredness is applied once at
  the end (`Optional: !f.Required()`, line 1180). This is exactly the seam
  where one normalization must happen.
- `internal/builder/gen_schema.go:1246-1339` — `resolveLocalInterfaceProps`
  also requires a direct `*dst.Ident` (line 1254) and its `dst.Inspect`
  sweep (lines 1312-1321) errors on a registered interface **anywhere
  else** — so today, `Optional[MyIface]` would be rejected as "unsupported
  location", confirming the brief's claim that interfaces are the
  exceptional path.
- `internal/builder/gen_schema.go:693-702` — an external, unscanned
  package type renders as a **silent empty `{}` schema**. This is the
  permissive fallback that issue #29 currently masks at scale.
- `internal/builder/schemas.go.tmpl:124-147` — the generated wrapper
  shadows each interface field as `json.RawMessage` with the original
  struct tag and assigns the decoded value directly. An absent key yields
  nil raw bytes, which the generated `__jsonUnmarshal__…` func feeds to
  `json.Unmarshal(nil, …)` — i.e. **today, an absent registered-interface
  property is a hard unmarshal error**, consistent with it being
  schema-required. Wrapper modes must branch on nil/`null`/other bytes as
  the brief specifies.
- `node_wrappers.go:640-664` + `internal/common/tags.go` — two independent
  parsers currently interpret the legacy `optional` tag
  (`StructField.Required()` and `ParseJSONSchemaTag`); deletion must cover
  both. The tag is used in three Go fixture/example files
  (`examples/test_options/types.go`,
  `internal/builder/test_run/test4-structs/struct_types.go`,
  `internal/builder/testfixtures/structs/struct_types.go`) plus README and the
  installable skill.
- `node_wrappers.go:648-655` — `PropNames()` returns
  `tag.Options[0]` as the JSON name, and the structtag fork puts the name
  at `Options[0]`. For `json:",omitzero"` (name defaulted), `Options[0]`
  is the **empty string**, so the property renders with an empty name.
  See §6.3.
- Baseline debt confirmed: `examples/.gitignore:1` ignores
  `jsonschema_gen.go`, so a fresh clone cannot compile
  `examples/structs` and `examples/providers_rendering` until generation
  runs (`go test ./...` passes in this checkout only because artifacts
  already exist locally);
  `internal/syntax/testfixtures/testapp0_simple/simple_struct.go:4` still
  invokes the removed `-type` CLI flag, so repo-wide `go generate ./...`
  fails.

### 2.3 Inferences (labeled)

- OpenAI accepts `{"type":["object","null"], …}` / `["array","null"]`
  compact forms — plausible from the general "union type with null"
  wording, **UNVERIFIED**, and not needed if v1 reserves compact form for
  scalars.
- OpenAI accepts a `description` as a sibling of `anyOf` — UNVERIFIED
  (their examples don't show it; they do reject siblings of `$ref`).
  Matters for where nullable-field descriptions go; probe it.
- OpenAI accepts a literal `null` inside an `enum` array — UNVERIFIED
  either way; their docs only show the (standards-invalid) enum-without-
  null pattern.

## 3. Challenges to the framing

### 3.1 The framing is right about the decision, and option 3 survives adversarial reading

I tried to break the ship-both hypothesis three ways and failed:

- *"Only Nullable, since the project targets LLM structured output."*
  Fails: Anthropic non-strict tools and any validate-and-retry workflow
  are first-class users of key-absence, and issue #28's original
  motivating case (attractor's `MaxRetriesSet` flags) is an Optional use
  case. Killing Optional would strand the issue that started this.
- *"Only Optional, keep it simple."* Fails harder: it leaves the project
  with no strict-compatible way to express "no value" at all, which the
  verified OpenAI rules make disqualifying for the project's stated
  purpose.
- *"One type with a mode knob"* (e.g. `Optional[T]` plus a tag or option
  choosing the wire form). Fails on the brief's own best insight: the two
  contracts differ in **runtime decode semantics** (null rejected vs. null
  accepted; zero value means absent vs. means null), not just schema
  shape. A mode knob would put wire semantics back outside the type
  system, recreating the exact defect of `jsonschema:"optional"`.

So: ship both. One presentational challenge stands: the brief's table
column "OpenAI strict potential: no" for `Optional` is correct but
understated in the docs plan — the practical rule worth stating verbatim
is *one Optional field anywhere in the tree disqualifies the whole schema
from strict mode* (each nested object must list every property as
required).

### 3.2 The sharpest hidden risk: OpenAI's documented nullable-enum pattern is standards-invalid, and the repo validates with a standards validator

This deserves more prominence than the brief gives it. Two verified facts
collide:

1. OpenAI's own strict examples write
   `{"type": ["string","null"], "enum": ["F","C"]}` — with a description
   ("Null otherwise") that expects the model to emit `null` — i.e. their
   sanctioned nullable-enum form does **not** add null to the enum.
2. Under 2020-12 semantics (keyword independence), that schema **rejects**
   `null`, and santhosh-tekuri v6 — the engine inside every generated
   `ValidateJSON` — defaults to 2020-12.

Consequence: if the generator copies OpenAI's documented pattern verbatim,
`ValidateJSON` will reject exactly the outputs OpenAI's constrained
decoding is expected to produce; the repo's validation feature and its
LLM-compatibility feature would contradict each other on every nullable
enum. The brief's "requires explicit local-validator and OpenAI proof" is
correct but undersells it: **there is no encoding that is simultaneously
(a) OpenAI-documented-verbatim and (b) standards-valid.** The generator
must pick a form that is standards-valid and *probe* it against OpenAI:

- Primary recommendation: `{"anyOf": [{"type":"string","enum":[…]},
  {"type":"null"}]}` — unambiguous under 2020-12, uses only
  doc-sanctioned constructs (anyOf, enum, `{"type":"null"}` branch), and
  matches the pattern OpenAI already shows for nullable `$ref`.
- Fallback if the live probe rejects it:
  `{"type":["string","null"], "enum":["F","C", null]}` — also
  standards-valid; OpenAI acceptance of a null enum member is UNVERIFIED.
- Never emit the docs' own invalid pattern while `--validate` exists.

The same reasoning applies to `const` (`ConstNode`,
`prependDiscriminator`'s discriminator consts are inside union branches
and stay non-nullable — only a *field-level* nullable const needs the
anyOf treatment).

### 3.3 Refs: anyOf is not merely "preferable", it is forced

The brief presents anyOf-for-refs as the correct choice; the verified
OpenAI behavior makes it the *only* choice twice over: a `$ref` cannot
carry a sibling `"type"` (rejected by OpenAI even though 2020-12 allows
it), and even under pure 2020-12 a sibling `type: [T,"null"]` would be
conjunctive with the ref target's own `"type":"object"`, so null could
never validate. Additionally, this constrains **descriptions**: a nullable
ref field's doc comment cannot ride on the `$ref` object; it must sit on
the `anyOf` wrapper (standards-fine; OpenAI acceptance UNVERIFIED — probe
it, with a documented fallback of dropping the description into the ref
target or omitting it).

### 3.4 The three-state trap is real; close it mechanically

The brief rightly refuses a three-state field. Make that a *generator
error*, not documentation: the classifier must reject
`Optional[Nullable[T]]`, `Nullable[Optional[T]]`, and self-nesting, with
an error naming the missing contract. Otherwise the type system the
feature is selling will quietly mint the fourth semantics anyway.

### 3.5 Wrapper position: restrict v1 to direct struct-field types

The brief's classifier normalizes "direct fields" but the plan doesn't say
what happens to `[]Optional[T]`, map values, embedded wrappers, a named
type defined *as* a wrapper (`type MyOpt polytype.Optional[int]`), an
alias of one, or a wrapper as a registered schema root. Semantics for most
of these are incoherent (an array element cannot be "absent";
`Optional` outside an object property has no `required` array to be
excluded from). Recommendation: v1 recognizes the canonical wrappers
**only as the complete type of a named struct field**; every other
position — including embedded fields and top-level registrations — gets a
specific error. `Nullable` inside slices (`[]Nullable[int]` → items
`["integer","null"]`) is coherent and may be a deliberate later
extension; excluding it now keeps the classifier exact and the tests
finite. Note the traversal layer (`resolveTypeExpr`) must still handle
`IndexExpr` *generally enough* to keep dependency resolution sound: for
the two canonical wrappers, recurse into `Index`; for any other generic,
keep a clear error.

## 4. Public API prescription

### 4.1 Types

Exactly as the plan specifies (`optional.go`, `nullable.go`, root
package, `T any`, `{Present bool; Value T}`), with these confirmations
and deltas:

- `Optional.IsZero() = !Present` + `json:",omitzero"` is the correct and
  now-verified mechanism (IsZero is authoritative for omitzero).
- `Nullable.IsZero() = false` is a sound defense and, per §2.1, the only
  one needed under encoding/json v1 (omitempty is inert for structs).
- **Delta — `Optional.MarshalJSON` when `!Present` should return an
  error**, not marshal the zero `Value`. It executes only when the
  containing field lacks `omitzero` (or the wrapper is marshaled
  standalone), which is precisely a bug worth failing loudly on; the
  error text should say "add json:\",omitzero\"". The plan's wording
  ("encodes the concrete Value only when the field is present") leaves
  this case undefined.
- Both wrappers: decode into a temporary and assign only on success;
  reject a present value that itself marshals to `null` (e.g.
  `Nullable[*T]{Present: true, Value: nil}`) so `Present` can never
  contradict the schema. Endorsed as written.
- No constructors/accessors in v1: acceptable. Flag for the release notes
  that `sql.Null[T]`-style ergonomics (`Get() (T, bool)`, `Of(v)`) are an
  additive follow-up, so their absence isn't litigated in this change.

### 4.2 Nesting and placement errors (new, per §3.4/§3.5)

Generation-time errors, each with a position and a fix-it message:
nested wrappers; wrappers anywhere but a direct named struct field;
wrapper as registered root type; wrapper via alias/defined type
(unsupported in v1, say so); `Optional`/`Nullable` combined with a
provider/template-hole field (semantics undefined — reject in v1).

### 4.3 Tag contract enforcement (new)

The generator sees the tags; use that:

- `Optional[T]` field whose json tag lacks `omitzero` → **error** (without
  it, absence marshals as present-zero and the wire contract is silently
  wrong).
- `Nullable[T]` field carrying `omitzero` or `omitempty` → error or
  warning (inert or actively harmful; `IsZero()=false` already defends,
  but the tag signals author confusion).
- Fix `PropNames()` for the empty-name case (§6.3) *before* documenting
  `json:",omitzero"` anywhere.

## 5. Schema IR prescription

Keep the public manual API (`DataType`, `JSONSchema`, `UnionSchemaEl`)
untouched — nothing in this feature requires changing it (`json_schema.go`
serves hand-written schemas; the generated IR in `internal/builder` is a
separate world).

In `internal/syntax`: one classifier, `FieldValueMode` +
`FieldValueSemantics{Inner dst.Expr, Mode}`, exactly as the brief sketches,
matching only the two canonical import-path/name pairs (my spike re-run
confirms alias-proof canonical `Ident.Path` is available). Both
`resolveTypeExpr` and the builder consume it; nothing else pattern-matches
generics.

In `internal/builder`, the minimal honest change set:

1. `ObjectProp`: replace `Optional bool` with `Required bool` (or keep the
   name and invert carefully — but the rename makes the required-array
   derivation read correctly). Requiredness is fact #1 and lives here.
2. Nullability is fact #2 and lives in the value schema, produced by one
   transform `makeNullable(JSONSchema) (JSONSchema, error)` applied in
   `renderStructField` *after* the five-way schema-source selection and
   *before* prop assembly — so refs, enums, interfaces, and the ordinary
   fallback all get it identically, and providers are rejected up-front.
3. New nodes, because the survey in §2.2 shows nothing existing can carry
   the shapes:
   - `NullNode` → `{"type":"null"}`.
   - `AnyOfNode{Desc string; Alternatives []JSONSchema}` → general anyOf
     with an optional description sibling. `UnionTypeNode` must remain
     untouched (its discriminator machinery and generated-code coupling
     are load-bearing); `makeNullable(UnionTypeNode)` builds an
     `AnyOfNode` by splicing the union's rendered options plus `NullNode`
     — flattening exactly as the brief asks, without nested unions.
   - `PropertyNode` gains type-array capability (e.g. `Nullable bool`
     consulted in `MarshalJSON` to emit `["T","null"]`, or
     `Types []string`). It is a marshaling change in one method either
     way; do it in IR, never by string surgery on serialized JSON
     (endorsing the plan's prohibition).
4. Transform dispatch (v1): plain scalar `PropertyNode` without
   enum/const → compact type array; `PropertyNode` with enum or
   `ConstNode` → `AnyOfNode` per §3.2; `RefNode` → `AnyOfNode` (§3.3);
   `UnionTypeNode` → flattened `AnyOfNode`; `ObjectNode`/`ArrayNode` →
   `AnyOfNode` (conservative v1 boundary; compact form is
   standards-legal and may be adopted later behind the same transform);
   `TemplateHoleNode` → error (already rejected upstream); anything else →
   error, never `{}`.
5. Descriptions: field comment goes on the outermost node the field
   renders to — for nullable-wrapped fields, that is `AnyOfNode.Desc` —
   preserving the existing behavior that comments become descriptions.
   Property order, discriminators, and type identities pass through the
   transform untouched (add golden tests asserting byte-stable output for
   a fixture with zero wrapper usage).

Generated interface unmarshaling: extend `InterfaceProp` and
`schemas.go.tmpl` with the mode; the raw-bytes truth table in the brief
(nil / `null` / other × ordinary / Optional / Nullable) is correct and
complete, including keeping "missing Nullable key" distinguishable in
generated code even though schema validation owns rejecting it — with the
honest caveat recorded in §6.5. Ordinary interface output must be
byte-identical (golden test).

## 6. Hidden risks the brief missed or understated

### 6.1 Issue #29's blast radius: fixing the filter detonates dormant code

`skipField`'s inversion means exported named fields are essentially never
traversed today, so most of `resolveTypeExpr` is dormant. Fixing #29
activates it, and at least two things detonate:

- The second inversion at `scan_result.go:523-530` (§2.2): once exported
  fields are traversed, a struct field typed as a **legacy registered
  interface** will hit the "undeclared local type" error (the condition is
  backwards — registered interfaces error, truly undeclared names pass
  silently). The #29 fix must correct this branch in the same change or
  the interface examples break.
- **Every external type referenced by an exported field starts being
  loaded and scanned** — including `time.Time`, which `renderSchema`
  special-cases but `resolveTypeExpr` does not. Post-fix, `time` (and
  transitively anything) gets `packages.Load`-ed and scanned for markers.
  At best this is slow; at worst it errors on stdlib shapes the scanner
  has never met. The fix needs a traversal-level exclusion for
  renderer-special-cased types (and the canonical wrapper package itself),
  plus fixtures covering an exported `time.Time` field and an exported
  cross-package field.

Step 1's "independent regression and cross-package proof" should therefore
be scoped as *the traversal-activation change*, not a one-line filter fix.
The plan's instinct to make it a review boundary is right.

### 6.2 The dead `SliceExpr` case

`scan_result.go:504` handles `*dst.SliceExpr`, which is a slice
*expression*, not a type — unreachable in type position. Harmless, but
remove or comment it during step 1 so the traversal switch reads truthfully
while it's being extended with `IndexExpr`.

### 6.3 `json:",omitzero"` produces an empty property name

`PropNames()` (`node_wrappers.go:648-655`) takes `tag.Options[0]` as the
JSON name; the structtag fork splits the whole value on commas, so a
name-omitted tag like `json:",omitzero"` yields `Options[0] == ""` and the
schema gets a property named `""` (encoding/json itself would use the Go
field name). Since `omitzero` is about to become the documented idiom for
every Optional field, this collides head-on: either fix `PropNames()` to
fall back to the Go field name when the tag name is empty, or have the tag
validator (§4.3) reject name-omitted json tags. Fix before any Optional
example ships.

### 6.4 `Skip()`/`PropNames()` divergence from `skipField`

`StructField.Skip()` (`node_wrappers.go:688`) has the *correct* visibility
logic while `syntax.skipField` has the inverted copy — two parallel
implementations of the same policy, one buggy. Step 1 should consolidate
to one function so the class of bug can't recur.

### 6.5 "Schema validation supplies that guarantee" — only when enabled

Validation is opt-in (`--validate`, commit `a29a831`), and
`ValidateJSON` exists only for non-rendered types. The Nullable contract's
missing-key-vs-null distinction is therefore *not* guaranteed for users
who skip `--validate` or use rendered/template types — plain
`encoding/json` will leave a missing Nullable key as
`Present == false`, indistinguishable from explicit null. The docs and
skill must state this plainly ("without --validate, a missing required
Nullable key decodes identically to null"), and the strict-fixture proof
should run with validation on.

### 6.6 Anthropic's anyOf/type-array budget

Anthropic strict tool-use documents a cap of 16 parameters using
anyOf/type arrays. A schema that makes every field Nullable can hit a
provider ceiling that has nothing to do with OpenAI. One sentence in the
skill's selection table ("prefer plain `T` where a value is always
required — nullable unions are not free on every provider") covers it.

### 6.7 Absent-interface bytes already error today

Per §2.2, the generated unmarshaler currently hard-errors on an absent
registered-interface key (nil RawMessage → `json.Unmarshal(nil, …)`). The
Optional-interface mode changes this from an incidental error into a
defined "absent" state — meaning the generated code's *error text* for
ordinary interfaces is part of today's observable behavior. Golden-test it
before touching the template.

## 7. Strict-compatibility checking (Q7)

Defer. The correct v1 boundary is: (a) encodings chosen so that an object
built from only `T` and `Nullable[T]` fields — with supported inner types
— lands inside the verified OpenAI subset (root object, all-required,
additionalProperties:false everywhere, anyOf-only composition, ≤10
nesting levels is the author's concern); (b) documentation stating the two
pre-existing caveats the checker would eventually own: a registered
interface as the *root* type violates "root must not be anyOf", and any
Optional field anywhere disqualifies strict mode. A later
`--strict-check` flag (or `WithStrictSchema()` marker emitting a
generation-time report) is the right home; building it now would widen
issue #28 for no decision-relevant gain.

## 8. Implementation order and review boundaries (Q8)

Endorse the plan's ten steps with these amendments:

1. **Step 1 (issue #29) absorbs §6.1/§6.2/§6.4**: the ident-branch
   inversion fix, the special-case traversal exclusions (time.Time,
   wrapper package), the skip-logic consolidation, and the dead-case
   cleanup, with the cross-package and exported-`time.Time` fixtures.
   Keep it a hard review boundary; it is the highest-uncertainty change in
   the sequence despite looking like a one-liner.
2. Step 2 (generation baseline) should decide the examples-artifact policy
   explicitly: either stop gitignoring `examples/*/jsonschema_gen.go`
   (they are documentation and `go:embed` inputs; checking them in is what
   makes fresh-clone tests honest) or add a test bootstrap that generates
   before building examples. Also fix or delete the `-type` invocation in
   `testapp0_simple`. A fresh-clone CI job is the only durable proof here.
3. Insert **tag validation + `PropNames` empty-name fix (§4.3, §6.3)**
   as its own small step between the runtime types (step 3) and the
   classifier (step 4) — it is independently testable and every later
   fixture depends on the tag idiom being safe.
4. Steps 4–6 as planned (classifier → IR → ordinary/omittable/nullable
   rendering), with the §5 node inventory.
5. Step 7 (enums/consts/refs/unions) keeps its review boundary and gains
   the **live OpenAI probe** (§9) as an explicit exit criterion, because
   §3.2 cannot be settled from documentation.
6. Step 8 (interfaces) keeps its boundary; require the ordinary-path
   golden test (§6.7) before the template is touched.
7. Steps 9–10 as planned. The closeout `rg` gate in the plan already
   covers the legacy-tag sweep; add `internal/common/tags.go` and
   `StructField.Required()` to the deletion checklist explicitly since
   they are *two* parsers (§2.2).

## 9. Proof gates (Q10)

Everything in the plan's proof-of-work section stands. Additions, each
traceable to a verified fact above:

1. **Validator-divergence test** (from §3.2): assert that
   `{"type":["string","null"],"enum":["a","b"]}` *rejects* null under
   santhosh-tekuri v6 defaults. This test documents *why* the generator
   does not emit OpenAI's doc pattern; if the library's default draft ever
   changes semantics, it fails loudly.
2. **Local validator matrix** for every emitted nullable encoding × {null,
   valid value, invalid value, absent-key-in-required-object}: compact
   scalar; anyOf ref; anyOf enum (and the enum+null fallback form); anyOf
   const; flattened union; nullable `time.Time`.
3. **Recorded live OpenAI probe** (model id, endpoint, date, raw error
   bodies checked into the repo) covering: compact nullable scalar;
   `anyOf[$ref, null]`; `anyOf[{enum branch}, null]`;
   `enum:[…, null]` with type array; description as sibling of `anyOf`;
   description as sibling of `$ref` (expected: rejected — confirms §3.3);
   `{"type":["object","null"]}` (expected: informational — settles the
   §2.3 unknown); an Optional-bearing object under `strict:true`
   (expected: rejected — proves the docs' claim); a pure `T`+`Nullable`
   object (expected: accepted). Probes are point-in-time; the recording
   requirement is what makes the claim honest as subsets drift.
4. **Tag-enforcement tests**: Optional-without-omitzero errors;
   Nullable-with-omitzero errors/warns; `json:",omitzero"` name handling;
   nested-wrapper and misplaced-wrapper errors with positions.
5. **Traversal-activation regression suite** for step 1: the issue's
   visibility test, grouped names, skip tags, cross-package exported
   field renders real schema (not `{}`), exported `time.Time` field,
   exported legacy-interface field (guards the §6.1 inversion), and a
   fixture proving undeclared local names now error instead of passing
   silently.
6. **Byte-stability golden test**: a wrapper-free fixture's generated
   schemas and generated Go are byte-identical before and after the
   feature lands (the strongest cheap proof that normalization touched
   only what it claims).
7. **Fresh-clone gate in CI**: clean checkout → `go generate ./...` →
   `JSONSCHEMA_NO_CHANGES=1 go generate ./...` → `go test ./...`, per the
   plan's closeout block — as a workflow, not a manual claim.

## 10. Summary of disagreements with the brief and plan

All are refinements; none reverses a brief decision:

1. Nullable-enum encoding: the brief's "either include null in the value
   constraint or use anyOf" becomes **anyOf primary, enum+null fallback,
   OpenAI-doc-verbatim form prohibited** (§3.2).
2. Refs: anyOf upgraded from "use" to "forced, twice over", with the
   description-placement consequence (§3.3).
3. `Optional.MarshalJSON` on absent values: error, don't emit zero (§4.1).
4. Add generation-time tag enforcement and wrapper-position errors as
   API surface, not just documentation (§4.2, §4.3).
5. Step 1 rescoped as traversal activation with two additional latent
   bugs in scope (§6.1); step 2 must pick an examples-artifact policy
   (§8).
6. `PropNames` empty-name bug must be fixed before the omitzero idiom is
   documented (§6.3).
7. The missing-key guarantee for Nullable is conditional on `--validate`;
   say so in every public description of the contract (§6.5).
