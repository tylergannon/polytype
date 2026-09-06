# Next step

Per the manager's step-size direction (2026-09-06): a step is a whole
milestone unless the brief splits it. Milestone 1 (#106) is unsplit and has
three items left. This step is all of them plus the commit.

## Step: finish #106 and commit

1. **Regenerates-cleanly test.** The generated type-level enum codecs live in
   a `!jsonschema` file, so the scanner — which loads with the `jsonschema`
   tag — must not see them as production `MarshalJSON`/`UnmarshalJSON` and
   trip the collision rejection added earlier this milestone
   (`syntax.FindProductionJSONMethods`, used at
   `internal/builder/gen_schema.go:449`/`:504`/`:1127`). Add one test that
   generates an enum-bearing fixture package, then generates the *same
   directory* a second time with the first run's `jsonschema_gen.go` still on
   disk, and asserts the second run succeeds and produces byte-identical
   output. Use whatever generation entry point the existing builder tests
   already drive; do not add a helper for one caller. Verify the test bites
   by temporarily making the scanner load without the tag (or by hand-adding
   the same methods to a tagged-visible file) and seeing it fail.

2. **Spec row.** In `docs/spec/v1.md`, amend the row at line 34 (marked value
   -mode enums, currently "Standard Go JSON") to name the generated
   type-level codec and its membership guarantee, and add one sentence to the
   amendment log at the top of the file. One row, one sentence, nothing more.

3. **Enum guide sentence.** In
   `website/src/content/docs/features/enums.md`, one sentence saying that
   only enum types declared in the generation target package receive the
   generated codec; enum types from other packages are not guarded.

4. **Commit** on this branch with a message naming `Closes #106`. Do not
   push, do not open a PR. Update
   `ephemeral/worklog/202609061434-issues-104-107.md` with decisions,
   corrections and friction only.

Nothing from #104, #105 or #107 is in scope, including reading their research
notes.

## Demonstrated by

`go test ./internal/builder/... -run 'TestBasic'` and the new regeneration
test by name, then the gates: `go test ./...`, `go vet ./...`,
`just build-tagged`, and `git log -1` showing the milestone commit.
