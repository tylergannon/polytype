# Next step

Milestone 1 (#106) is partly done: commit 24abd7a emits the type-level
`MarshalJSON`/`UnmarshalJSON` for locally declared `enum()`-marked types and
proves null rejection, zero-member round-trip, and non-member rejection by
calling the methods directly in `internal/builder/testfixtures/enums/
enum_codec_test.go` (mirrored in `test_run/test3-enums`).

Still open in this milestone, in later steps: the `.StringerEnum` coexistence
test, the regenerates-cleanly test, the `docs/spec/v1.md` row amendment, and
the foreign-enum sentence in `website/src/content/docs/features/enums.md`.

## Step

Prove the codec through the enclosing struct, which is what a consumer of a
generated package actually does — the current tests only call the methods.

In `internal/builder/testfixtures/enums` (and its `test_run/test3-enums`
copy, which `TestBasic` regenerates and runs):

- Add a struct with a required `EnumType` field. `EnumType` has no zero-value
  member, so its Go zero value is a non-member — that is what makes the
  first assertion below possible without inventing a new enum.
- Extend `enum_codec_test.go` with one test asserting, on that struct:
  - `json.Marshal` of the zero value fails and the error text names the
    field's enum type and the offending value;
  - `json.Marshal` of a member succeeds and emits the constant's own value;
  - a document decoded with `json.Unmarshal` and re-encoded with
    `json.Marshal` is byte-identical to the input;
  - `json.Unmarshal` of a document whose field holds a non-member fails.

Nothing else changes: no template edits, no new generator behavior, no
`.StringerEnum` work, no docs. If the assertions pass without touching
`schemas.go.tmpl` or `gen_schema.go`, that is the expected outcome — the step
exists to close the brief's proof line, not to change code.

## Demonstrated by

`go test ./internal/builder/... -run 'TestBasic|TestEnumCodec'`, then the
gates: `go test ./...`, `go vet ./...`, `just build-tagged`.
