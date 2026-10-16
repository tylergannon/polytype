# Independent review: Polytype generation results

Outcome: **no findings**.

Target: the complete current implementation in `/Users/tyler/.codex/worktrees/polytype-dev-generation/polytype`, based on `0fc3aadbbbce15689aebbd97287bc6640de298b1`, including tracked changes and new `codegen/result_test.go` and `internal/publication/` files. Reviewed against the accepted `skgo-project/ephemeral/plans/dev-generation-callback.md`, authorization to implement Polytype now while deferring Gimbal, and applicable repository instructions.

Applied agent-protocol, adversarial-review, Polytype and proof-of-work guidance. The review scope was not narrowed to predicted defects or selected files. Only this review artifact was written; implementation was not modified, no mutation testing was performed, and no services were started. Existing application/worker integration work remains deferred.

## Evidence inspected

Read the full implementation diff, new tests, publication primitive and worklog. Inspected surrounding `codegen` dispatch, Worker serialization/retention, `internal/builder` schema/Go publication, retained and persistent TypeScript replay, declaration output plans and CLI emission replay. Compared against the accepted plan's result definition, equal-write suppression, partial-failure behavior, existing-call compatibility, retained-cache boundary and production/tooling separation. The actual production Gimbal integration is outside this authorized slice; its absence is not treated as a Polytype defect.

Changed behavior is confined to tooling: additive result-bearing `GenResult`, `GenerateResult` and `Worker.GenerateResult` APIs; existing error-only entry points delegate without changing signatures. A per-call publication accumulator is carried through ordinary generation and retained/persistent TypeScript replay. Successfully completed writes/removals set it monotonically; later errors preserve earlier mutations. The shared publication helper compares actual bytes before atomic replacement, so unchanged outputs preserve mtimes. Schema repair checks actual schema bytes as well as sidecars, rather than trusting an unchanged checksum to establish intact output. Existing stale-codec and orphan-schema ownership checks remain in place before removal.

The existing Worker still owns its analysis and locking behavior, and its retained TypeScript-only eligibility remains intact. No general cache mode, watcher, supervisor, callback transport, output manifest, application tag or Gimbal dependency was added. New code is beneath generation/builder tooling rather than imported by Polytype's root marker/runtime package.

## Checks run

- `go test ./...` — passed, including the builder suite. Some packages reused Go's valid test cache.
- `go test ./codegen ./internal/publication -run 'TestGenerationOutputResults|TestOrdinarySchemaDevalueAndGoResults|TestCompletedMutationSurvivesLaterError' -count=1 -v` — passed uncached; all named tests executed, with both ordinary and retained Worker subtests. No skips in the focused run.
- `just build-tagged` — passed, covering example registrations outside the ordinary untagged test selection.
- `git diff --check` — passed.

## Load-bearing test assessment

`TestGenerationOutputResults` asserts literal expected booleans for initial publication, warm no-op, fresh Worker after Close, Force with equal bytes, body-only edits with equal output, missing/damaged output repair, source-shape changes, invalid-source failure and successful retry. It independently assigns an old mtime and checks it is preserved, so a writer that merely returns false while rewriting equal files fails. The emitted `"ready": boolean` expectation is literal rather than derived from the implementation.

Its partial-publication fixture removes types.ts and replaces index.ts with a directory. The first output is restored before the later publication fails, and the test requires both a non-nil error and true. Dropping the accumulated result on error, or inferring it solely from overall success, fails this test in both the retained and ordinary TypeScript paths. Subsequent successful repair and no-op are also asserted.

`TestOrdinarySchemaDevalueAndGoResults` selects schema, Go codecs, ordinary TypeScript and devalue together, bypassing the TypeScript-only fast path. It checks true on initial publication, false on repeats/Force-equal, and fixed mtimes for each output. It independently removes each named artifact and requires repair to report true. It corrupts actual schema bytes while preserving the existing checksum and requires repair, catching the old checksum-only assumption. It creates a checksum-owned obsolete schema and requires removal and true, followed by false.

`TestCompletedMutationSurvivesLaterError` directly exercises the shared publication accumulator: missing removal remains false, creation sets true, later failure cannot erase true, equal bytes remain false, and actual removal sets true. The higher-level partial-publication test separately establishes that this state is threaded back through the public APIs, so primitive-only coverage is not mistaken for integration coverage.

Existing golden/round-trip, generated-file ownership, schema/codec cleanup and Worker/session suites remain relevant regression coverage and passed in the full run. I did not alter source to demonstrate mutant failures; load-bearing conclusions are based on concrete exercised publication paths and assertions whose outcomes differ under the identified failures.

## Findings and proof boundary

No material correctness, performance, production-isolation or scope defect found. No optional expansion requested.

The focused tests execute the actual generation entry points against real temporary Go packages and filesystem failures. That is appropriate proof for this tooling slice. They do not prove SKGO replacement admission, the persistent Gimbal helper, a running Gimbal edit loop or actual Gimbal production-target exclusion; those accepted-plan observations remain deferred with the integration itself. This review does not claim release publication or runtime timing results.
