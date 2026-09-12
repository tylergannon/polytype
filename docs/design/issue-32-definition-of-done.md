# Issue 32 definition of done

Status: accepted direction; implementation in progress

Worktree: `/Users/tyler/src/polytype-issue-32`

Branch: `codex/issue-32-optional-nullable`

This is the controlling implementation checklist. Check an item only when its
listed acceptance behavior and command both pass.

## Outcome

The public package provides two generic value types:

```go
type Optional[T any] struct {
	Present bool
	Value   T
}

type Nullable[T any] struct {
	Present bool
	Value   T
}
```

Users can write:

```go
type Config struct {
	Name       string                   `json:"name"`
	MaxRetries polytype.Optional[int] `json:"max_retries,omitzero"`
	Timeout    polytype.Nullable[int] `json:"timeout"`
}
```

The generated schema and Go JSON behavior agree on these contracts:

| Field | Required | Accepts null | Go no-value state |
|---|---:|---:|---|
| ordinary `T` | yes | no | none |
| `Optional[T]` | no | no | `Present == false` |
| `Nullable[T]` | yes | yes | `Present == false` means null |

For the example above, `name` and `timeout` are required;
`max_retries` is not. `max_retries` has type `integer`; `timeout` has type
`["integer", "null"]`.

## Runtime acceptance

`Optional[T]`:

- the zero value is absent;
- `IsZero()` reports `!Present` so `json:",omitzero"` omits absence;
- present zero and empty values marshal normally;
- explicit JSON null is rejected;
- marshaling an absent standalone wrapper is rejected;
- `Present == true` is rejected if `Value` marshals as JSON null;
- failed decoding does not mutate the receiver.

`Nullable[T]`:

- the zero value marshals as JSON null;
- `IsZero()` always reports false so `omitzero` cannot erase a required key;
- a non-null value decodes with `Present == true`;
- null clears `Value` and sets `Present == false`;
- `Present == true` is rejected if `Value` marshals as JSON null;
- failed decoding does not mutate the receiver.

Plain `json.Unmarshal` cannot distinguish a missing Nullable key from a null
Nullable key. Generated schema validation enforces required-key presence.

## Generator acceptance

- Recognize wrappers by canonical package path, including import aliases.
- Recognize them only when the wrapper is the complete type of a direct named
  struct field.
- Reject wrapper roots, embedded wrappers, aliases/defined wrapper types,
  nested wrappers, wrappers inside containers, and other unsupported placement
  with an actionable generator error.
- Require `json:",omitzero"` on Optional fields.
- Optional changes requiredness only; its inner value uses ordinary rendering.
- Nullable changes nullability only; its property remains required.
- Nullable scalar values use compact type arrays such as
  `type: ["integer", "null"]`.
- Nullable supported structs and pointers to structs use `anyOf` with the full
  inlined object schema and a null branch.
- Ordinary neighboring fields and wrapper-free generated output remain
  unchanged.

V1 Optional supports every meaningful type already handled by the ordinary
renderer: scalar and named scalar values, structs, pointers, arrays/slices,
explicit supported refs, and registered interfaces. Existing unsupported types
remain unsupported.

V1 Nullable supports scalar and named scalar values plus supported structs and
pointers to structs. Nullable arrays/slices, consts, enums, registered
interfaces, explicit refs, providers, and templates fail generation clearly.

The legacy `jsonschema:"optional"` behavior and its documentation are removed
as the agreed breaking change. A leftover legacy tag is inert: it has no
optionality effect and is not itself a generation error. Existing fixtures and
examples stop relying on it.

The manual schema-building API remains unchanged.

For OpenAI strict Structured Outputs, follow the published schema rules rather
than requiring a live API probe: the root is an object, every property is
listed in `required`, and objects use `additionalProperties: false`.
`Nullable[T]` is the wrapper for OpenAI's documented required-plus-null union
pattern. A schema containing `Optional[T]` is intentionally not strict-compatible
because that property is omitted from `required`. Nullable does not by itself
guarantee that an otherwise unsupported schema shape is OpenAI-compatible.

## Implementation checklist

### 0. Establish the working baseline

- [x] Fetch current `origin/main`.
- [x] Create the issue worktree and branch from exact remote head
  `efab9952c5ea71fc7ff291e71fb13f3fd4bfafd0`.
- [x] Run `go test ./...` successfully before implementation.
- [x] Obtain Claude Fable consensus on this direction and proof contract.

Evidence:

- `ephemeral/reviews/2026071015-issue-32-dod-fable.md`
- `ephemeral/reviews/2026071015-issue-32-dod-fable-round2.md`

### 1. Implement the public runtime types

Files:

- `optionality.go`
- `optionality_test.go`

Work:

- [x] Add `Optional[T any]` with exported `Present` and `Value` fields.
- [x] Add `Nullable[T any]` with exported `Present` and `Value` fields.
- [x] Implement both `IsZero` methods.
- [x] Implement transactional marshal/unmarshal behavior from Runtime
  acceptance above.
- [x] Reject absent standalone Optional values, explicit null Optional input,
  and any `Present == true` value that marshals as null.
- [x] Prove absent, null, present-zero, present-empty, valid, and failed-decode
  states in focused unit tests.

Exit command:

```text
go test .
```

### 2. Classify direct wrapper fields

Primary code surfaces:

- `internal/syntax/node_wrappers.go`
- `internal/syntax/scan_result.go`
- focused syntax tests and fixtures

Work:

- [x] Identify `Optional` and `Nullable` only by canonical package path and
  type name.
- [x] Preserve recognition through an import alias.
- [x] Extract the single type argument and classify the direct field as
  ordinary, optional, or nullable.
- [x] Allow discovery to traverse the inner type of a recognized wrapper.
- [x] Reject wrong arity and unsupported placements with the source position
  and a useful reason.
- [x] Prove unrelated third-party types named Optional/Nullable are ordinary
  types, not wrappers.

Exit command:

```text
go test ./internal/syntax
```

### 3. Generate requiredness and nullable schemas

Primary code surfaces:

- `internal/builder/gen_schema.go`
- `internal/builder/model.go`
- builder fixtures, integration tests, and golden schemas

Work:

- [x] Send a wrapper's inner expression through ordinary rendering exactly
  once.
- [x] Exclude Optional properties from the containing object's `required`
  array without changing their inner schema.
- [x] Keep Nullable properties in `required`.
- [x] Emit compact scalar type unions with null.
- [x] Emit Nullable inlined objects as `anyOf` with complete object and null
  branches.
- [x] Require the `omitzero` JSON option on Optional fields.
- [x] Reject deliberately unsupported Nullable shapes with actionable errors.
- [x] Prove ordinary neighboring fields and wrapper-free generated output are
  unchanged.
- [x] Cover every integer and float variant for ordinary and wrapped fields.

Exit commands:

```text
go test ./internal/builder
go generate ./internal/builder/testfixtures/...
```

### 4. Complete supported non-scalar and decoder behavior

Primary code surfaces:

- `internal/builder/schemas.go.tmpl`
- registered-interface fixtures and generated code
- optionality example added in step 5

Work:

- [x] Support Optional structs, pointers, arrays/slices, supported refs, and
  registered interfaces through their real ordinary-renderer paths.
- [x] Support Nullable structs and pointers to structs.
- [x] Generate the per-field glue required to decode Optional registered
  interfaces through the discriminator decoder.
- [x] Preserve the destination on malformed input and unknown discriminators.
- [x] Reject null Optional interface input.
- [x] Keep Nullable registered interfaces unsupported in V1 with a clear
  generation error.

Exit command:

```text
go test ./internal/builder -run 'TestBasic/(test9-v1-interfaces-options|test12-optionality)'
```

### 5. Add the executable consumer proof

Required artifacts:

- `examples/optionality/types.go`
- `examples/optionality/schema.go`
- `examples/optionality/jsonschema_gen.go`
- `examples/optionality/jsonschema/*.json`
- `examples/optionality/cmd/proof/main.go`
- `examples/optionality/proof/expected.json`

Work:

- [x] Register the example through the public API and generate it with the real
  CLI using `--validate`.
- [x] Make the proof command execute every case listed under Behavioral proof.
- [x] Make the command compare observed behavior with explicit expectations and
  exit non-zero on any mismatch.
- [x] Print a deterministic JSON transcript containing schemas, raw input,
  validation result, decoded wrapper state, and re-marshaled output.
- [x] Commit the generated schemas, generated Go code, and expected transcript.
- [x] Prove unsupported shapes by invoking the real generator against committed
  negative fixtures.

Exit commands:

```text
go generate ./examples/optionality
go run ./examples/optionality/cmd/proof
git diff --exit-code -- examples/optionality
```

### Review checkpoint 1: working feature

- [x] Request independent third-party review of the actual runtime, generator,
  generated decoder, example, and proof diff.
- [x] Resolve every critical, bug, and material design finding before expanding
  documentation or preparing the PR.
- [x] Record accepted and rejected findings in the session worklog.

### 6. Remove the legacy mechanism and update public guidance

Work:

- [x] Remove `jsonschema:"optional"` from requiredness parsing.
- [x] Confirm leftover legacy tags are inert, not generation errors.
- [x] Remove all fixture/example reliance on the legacy tag.
- [x] Update README, examples, internal developer notes, and
  `skills/polytype/SKILL.md` to the agreed contract.
- [x] State plainly that missing Nullable and explicit null are indistinguishable
  through plain `json.Unmarshal`; generated validation enforces presence.
- [x] Document supported/rejected shapes and strict-schema implications.

Exit command:

```text
if rg -n 'jsonschema:"[^"`]*optional' --glob '*.go' .; then exit 1; fi
```

### 7. Run repository closeout

- [x] Commit the complete candidate before the clean-tree proof sequence.
- [x] Run repository-wide generation.
- [x] Confirm generation leaves the tree clean.
- [x] Run no-change generation mode.
- [x] Run the full unit suite after all changes.
- [x] Prove a fresh checkout generates and tests without local state.
- [ ] Push the branch and open the pull request with behavioral evidence first.
- [ ] Verify required CI against the exact pull-request head SHA.

Exit sequence:

```text
go generate ./...
git diff --exit-code
JSONSCHEMA_NO_CHANGES=1 go generate ./...
go test ./...
git diff --check
```

### Review checkpoint 2: exact-head release review

- [ ] Request independent review of the complete exact-head diff, uploaded
  proof, migration, documentation, and CI state.
- [ ] Resolve every critical, bug, and material design finding.
- [ ] Enable squash auto-merge when the exact head is proved and required CI is
  green or pending.
- [ ] Follow through until merged or report a concrete blocker.

## Behavioral proof

A committed `examples/optionality` package is generated by the real
`polytype` CLI with validation enabled. Its proof command compiles against
the generated code and exits non-zero on any mismatch.

The command demonstrates and records:

1. The exact generated property schemas and `required` array for ordinary,
   Optional, Nullable-scalar, and Nullable-object fields.
2. Missing Optional, present-zero Optional, and present-empty Optional decode
   to distinct wrapper states and round-trip distinctly.
3. Null Optional is rejected without partial mutation.
4. Null Nullable, present-zero Nullable, and present-object Nullable decode to
   distinct wrapper states and round-trip distinctly.
5. Generated validation accepts missing Optional and null Nullable, rejects
   null Optional and missing Nullable, and accepts the valid concrete cases.
6. Optional container, struct, pointer, and registered-interface values execute
   through their real generated paths, including interface discriminator errors
   and failure without partial mutation.
7. Unsupported placements and Nullable shapes fail through the real generator
   with the promised actionable diagnostics.

The generated schemas and deterministic proof transcript are committed and
linked in the pull request. They are produced by running:

```text
go generate ./examples/optionality
go run ./examples/optionality/cmd/proof
```

OpenAI compatibility guidance is documentation-derived: strict Structured
Outputs requires every property to be required, so consumers use
`Nullable[T]` for the documented required-plus-null pattern and do not use
`Optional[T]` in a strict schema. No live API call, credential, or provider
response is part of this feature's proof.

## Hygiene and delivery

- Focused unit and integration tests lock down the demonstrated behavior.
- Tests cover ordinary and wrapped fields for the generator's supported signed
  and unsigned integer widths and both float widths, with ordinary JSON
  rejection of NaN and infinities.
- `go test ./...` passes on the final worktree.
- Repository-wide generation is clean and deterministic.
- `JSONSCHEMA_NO_CHANGES=1 go generate ./...` passes.
- A fresh checkout can generate and test without local bootstrap state.
- README, examples, and the checked-in skill agree on the three
  contracts, selection guidance, `omitzero`, the missing-Nullable-key validation
  caveat, supported and rejected shapes, interface behavior, strict-schema
  implications, and usable examples.
- The tracked session worklog records decisions, review, proof, and final state.
- Required CI is green for the exact pull-request head SHA.
- The pull request leads with the behavioral proof, not the hygiene commands.

## Review checkpoints

1. After the runtime types and generator behavior work through the executable
   example, request independent code review of the actual diff and proof path.
2. After supported-shape coverage, legacy removal, documentation, and exact-head
   proof are complete, request independent release review before merge.

No additional review ceremony is required unless a reviewer finds a concrete
critical, bug, or material design problem.
