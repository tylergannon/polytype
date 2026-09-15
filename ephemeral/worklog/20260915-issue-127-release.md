# Issue #127: recursive codec release

- Scope: finite recursive Go types through the programmatic GoJSON, devalue,
  and TypeScript generators. Recursive JSON Schema and cyclic runtime values
  remain deferred, as authorized in the plan.
- Implementation: Claude Opus 4.6. Independent validation: GPT Sol.
- User clarified the original three-round cap was to prevent scope and proof
  growth and authorized fixing the remaining named-container discovery bug.
- Final production fix follows a named non-struct type into its underlying
  expression, preserving existing visited-type and error propagation behavior.
- Opus reports `go test -count=1 ./...` passing after the final fix.
- Delivery branch contains source/tests/docs, the 108-line plan, and this
  concise record. Original implementation/review history and raw logs are
  preserved on `codex/issue-127-plan` (implementation through `2958e67`).
- Clean delivery-worktree baseline: `go test ./...` passed before applying
  the implementation.
- Final GPT Sol independent review found no material defect. The round-3 named-container reproduction emitted both nested union discriminators after generation and runtime were run in separate Go invocations; the first combined invocation compiled stale code before generation.
- Independent baseline `go test ./...`, final uncached `go test -count=1 ./...`, `just build-tagged`, and diff whitespace check passed. Final review is `ephemeral/issue-127-final-validation.md`; draft PR and CI state are owned by the release manager.
