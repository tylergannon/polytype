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
  the implementation. Final independent review and CI pending.
