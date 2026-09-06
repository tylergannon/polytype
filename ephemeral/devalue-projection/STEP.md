# Next step

Milestone 1 (#106) still owes four items from the brief: the `.StringerEnum`
coexistence test, the regenerates-cleanly test, the `docs/spec/v1.md` row
amendment, and the foreign-enum sentence in the enum guide. This step takes
the first, because it is the one that can fail: if the owner codec and the new
type-level codec interfere, generation or round-tripping breaks, and nothing
committed so far would notice.

## Step

Prove that an integer enum used in string mode in one field and numeric mode
in another gets both codecs and that they do not collide, using the fixture
that already has exactly that shape: `internal/builder/testfixtures/
v1_enums_stringmode` (regenerated and run as test10 by `TestBasic`), where
`Paint.C` is `WithStringerEnum(Paint{}.C)` and `Paint.Numeric` is the same
`Color` type left in numeric mode.

Extend that fixture's `codec_test.go` with one test asserting, on a `Paint`
value whose fields all hold members:

- `json.Marshal` emits the constant *name* for `c` (the owner codec's mapped
  string) and the constant *value* for `numeric` (the type-level codec) in the
  same document — the two modes coexist on one type in one struct;
- that document round-trips through `json.Unmarshal` and `json.Marshal`
  byte-identically;
- `json.Marshal` of a `Paint` whose `Numeric` holds a non-member `Color`
  fails with an error naming `Color` and the offending value, while the same
  non-member in `C` is rejected by the owner codec — assert whatever error the
  existing owner path actually produces, do not change it;
- `colorStringCalls` is still 0 afterwards, so neither codec started routing
  through `String()`.

Regenerate the fixture's `jsonschema_gen.go.golden` if and only if the
generated output actually changed; a green run with no golden churn is the
expected outcome, since the type-level codec was already emitted.

Nothing else changes: no template or generator edits unless the test fails,
no regenerates-cleanly test, no spec or website edits.

## Demonstrated by

`go test ./internal/builder/... -run 'TestBasic'` (regenerates test10 and runs
the fixture module's own tests), then the gates: `go test ./...`,
`go vet ./...`, `just build-tagged`.
