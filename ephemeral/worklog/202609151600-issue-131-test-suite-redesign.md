# Issue 131 — slow unit tests: batched-load test suite redesign

Branch: `claude/slow-unit-tests-a036d9` (worktree). Base: `8b1ac72` (main at start).

Goal (user): make the suite fast and cacheable. Plan lives in
`ephemeral/issue-131-plan.md`. User-chosen execution order this session:
step 1 (`RunLoaded` split) → step 3 (convert inline-fixture tests) →
step 2 (convert `TestBasic`) → step 4 (`t.Parallel()`).

- decision (user): do step 3 now "while the mechanism is fresh", steps 1-2 after.
  Step 1 is a hard prerequisite for 3 (batched generation calls `RunLoaded`),
  so the real order is 1 → 3 → 2 → 4. Flagged to the user: step 3 builds
  fixture-registry infra for *inline* fixtures while step 2 builds it for the
  *committed* fixtures, so step 2 may fold into whatever 3 establishes.

## Investigation findings carried in from before compaction

- Only `internal/builder` misses the test cache in steady state (~30s wall /
  ~138s CPU of a ~33-42s suite).
- CPU root cause: `syntax.Load` uses `NeedDeps|NeedSyntax|NeedTypes`, so every
  load type-checks the whole dep graph from source (~572ms CPU/load × ~122
  loads). One batched load costs the same for 1, 13 or 63 packages.
- `t.Parallel()` on the existing serial tests is worthless under CPU
  saturation (30s → 30-33s wall, CPU 138 → 293s).

## Correction found this session: `test_run/` is not the only cache poisoner

Captured with
`go test -c -o $SP/builder.test ./internal/builder && (cd internal/builder && $SP/builder.test -test.testlogfile=$SP/builder.testlog)`:

- 91,436 recorded inputs; **1,658 under `testfixtures/` vs 364 under `test_run/`**.
- The 18 `os.MkdirTemp(filepath.Join(cwd, "testfixtures"), ...)` helpers create
  **74 randomly-named fixture dirs inside the module root**, write Go files,
  read them back (recorded as `open`), then `RemoveAll` them in `t.Cleanup`.
- `computeTestInputsID` re-hashes every recorded path at lookup time; these
  paths do not exist on the next run, so the ID can never match. Unconditional
  miss, independent of the 2s `errFileTooNew` window.
- Verified: after a full run `ls -d internal/builder/testfixtures/*_[0-9]*`
  matches nothing.
- **Consequence: step 2 alone does not restore caching.** Step 3 is required.

## Correction found this session: shared-graph mutation is a non-risk here

Checked before relying on batched loads:

- No AST mutation in `internal/builder` (no `.Decls`/`.Body` writes, no `dst.Apply`).
- `applyTransforms` is a no-op: `RegisterTransform` (`internal/builder/pipeline.go:13`)
  is never called in production, so `registeredTransforms` is always empty.
- `internal/utils/rewriter` is a standalone `main` dev tool, not imported by the
  builder. Earlier flagging of it in the plan was wrong.
- No package-level mutable caches in `syntax` or `builder`; `DefaultPackageCfg`
  is copied per load.
- `ScanResult` is constructed fresh per `LoadPackage` with its own maps; the
  mutations that exist (e.g. `programmatic.go:86` `iface.Discriminator = ...`)
  write into that per-call struct, not the shared graph.

Verification command for the converted suite (order-dependent leakage is what
would expose a miss here): `go test -race -count=2 -shuffle=on ./internal/builder`.

## Step 3 scope (measured)

80 test funcs, 18 `MkdirTemp` helpers, 74 fixture dirs, 60 `Run(...)` calls.

Must NOT be converted to a shared pre-load — each reads back what the previous
`Run` wrote to disk, so a pre-loaded graph would be a stale syntax tree:

- `declaration_file_test.go`: `TestSwitchingSchemaOutputReplacesGeneratedFile` (4),
  `TestEntrypointlessRootIsLoweredWhateverItsType` (2),
  `TestEntrypointlessDeclarationRejectsSchemaOnlyOptions` (2)
- `schema_prune_test.go`: `TestGenerationPrunesOrphanedOwnedSchemas` (4)
- `sealed_union_test.go`: `TestSealedUnionMembershipDrift` (3)
- `enum_type_codec_test.go`: `TestGeneratedEnumCodecsRegenerateCleanly` (2)
- `validation_removal_test.go`: `TestGenerationRefusesToSilentlyRemoveValidationMethods` (2),
  `TestForceExplicitlyAllowsValidationMethodRemoval` (2)

Plus: keep isolated loads for tests asserting load *failures*.

Also changes: 10 `dirNames` assertions lose `go.mod` from their expected lists.

Diagnostics are NOT a risk: assertions use relative names (`"types.go"`,
`"schema.go:7:9"`) or build paths from the dir variable.

## Refinement: step 3 splits into 3a (caching) and 3b (CPU)

Found while starting the work: the two goals are separable, and only 3b carries
the risk.

- **3a — fixtures out of the module root** into `t.TempDir()` + generated
  `go.mod`. Restores caching (the issue's actual complaint). Mechanical: no
  registry, no inversion of control, and the 8 sequential-`Run` tests need no
  special handling because each still loads fresh. Does not reduce CPU.
- **3b — batch the loads**. Reduces CPU; carries the inversion of control, the
  8-test exclusion list and the `dirNames` updates.

The pattern for 3a already existed in-tree — `writeTypeGrammarFixture`
(`typegrammar_adapter_test.go:412`) and `writeMultiFileFixture`
(`validate_free_func_test.go:16`) both already use `t.TempDir()` + a written
`go.mod` with a `replace` to the repo root. So 3a is applying a proven in-repo
pattern to 18 more sites, not inventing one.

Note for 3b: export data (`~693 → ~205` CPU-ms per load) reaches a similar CPU
endpoint with far less test churn and helps real CLI users too. Worth comparing
before committing to 3b.

## Log

- baseline: `go test ./...` green, exit 0. Everything except `internal/builder`
  reported `(cached)`, confirming it is the sole miss.
- step 1 DONE: `RunLoaded` split in `internal/builder/builder.go`. `Run` keeps
  the `syntax.Load` then delegates; `RunLoaded(pkg, args)` holds the rest.
  Both validate `--typescript-barrel` because both are entry points.
  Proof: `go build ./...`, `go vet`, gofmt clean, and
  `go test ./internal/builder/ ./polytype/ ./grammar/` green (32.2s/5.7s/4.1s).
- step 3a in progress. Added `internal/builder/fixture_module_test.go` with
  `newFixtureModule` / `writeFixturePackage` / `newFixture` and the
  `fixtureModulePath` constant. The constant keeps the spelling
  `example.com/typegrammarfixture` because `declaration_file_test.go:175`
  asserts a diagnostic quoting it.
- 3a exemplars converted by hand and passing:
  - `enum_marker_test.go` `writeEnumMarkerFixture` (single-package shape)
  - `asref_collision_test.go` `writeAsRefCollisionFixture` (two-package shape;
    the dep moved to a `dep/` subdir imported as `fixtureModulePath+"/dep"`,
    replacing an import path built from the random MkdirTemp basename)
- 3a bulk conversion delegated to three parallel subagents over disjoint files,
  with the exemplars as the spec and a hard rule that no assertion may change.
- 3a DONE. All 18 in-module fixture sites eliminated; `grep MkdirTemp
  internal/builder/*_test.go` returns nothing. One agent correctly refused to
  convert `TestSealedUnionDeclarationOutsideInterfacePackageIsRejected` and
  reported it instead of guessing -- same lockstep `baseImport` case as
  owner_codec, converted by hand afterwards.
  - `named_container_test.go` changed a `PackagePath` value; that is an *input*
    to `LoadProgrammatic`, not an assertion, and must name the loaded package.

## Architectural finding (user): stop optimising redundant work

The user pushed back on making "tons of redundant software run fast". Checked
it against the code and they are right:

- `typescript` already tests projections from hand-built `typegrammar` values:
  3 of its 4 test files do ZERO package loads and the package runs in 4.2s.
  `internal/builder` does ~122 loads and runs in 30s.
- Cause: there are two pipelines. JSON Schema + Go codecs render from the
  builder's own model (`gen_schema.go`, 2587 lines; `model.go`, 510 lines),
  which never references `typegrammar`. Only TypeScript/devalue consume the IR.
- So every JSON Schema assertion must drag a full dependency type-check behind
  it. That is the redundancy, and it is architectural.
- **Opened issue #132** to project JSON Schema and Go codecs from `typegrammar`.
  Blocked on the strict golden harness from step 2 (AC1 = 351 committed outputs
  byte-identical, which a hardcoded file list cannot support).
- decision: **3b is dropped permanently.** Churning ~70 tests to speed up code
  #132 would delete is wasted work.

## Step 2 DONE

Design: copy `testfixtures/` into one temp module -> ONE `decorator.Load` ->
`builder.RunLoaded` per fixture in parallel -> compare EVERY `*.golden` ->
one `go mod tidy` + `vet` + `test` + `doc` as the acceptance layer.

- Repo: single nested `internal/builder/testfixtures/go.mod`; deleted 13
  per-fixture `go.mod`/`go.sum`, 13 `gen/` programs, 13 `//go:generate` lines,
  and 227 tracked files under `test_run/`. Updated `AGENTS.md` test-structure
  section (`CLAUDE.md` is a symlink to it).
- correction: my first draft added `require.Empty(t, pkg.Errors)`, which the
  original never asserted. Two fixtures legitimately carry type errors under
  the jsonschema tag (`indirecttypes` declares methods on pointer-underlying
  types -- the shape the builder must diagnose; `providers` has overlapping
  declarations between types.go and schema.go). Removed it and documented why.
  The old test never surfaced this because it only checked the generator
  subprocess's exit code.
- proof (stale golden): the compare-every-golden check caught
  `providers/jsonschema/Example.json.golden` on its first real run -- 0 bytes,
  no generated counterpart, fixture emits `Example.json.tmpl`. Removed.
- coverage: `test_run/` tracked 56 generated artifacts vs 52 goldens, so 5
  `jsonschema_gen.go` files had no golden. Seeded those 5 goldens from the OLD
  pipeline's `test_run` output, then required the NEW in-process pipeline to
  reproduce them -- byte-identical across all 57 goldens. That is a real
  cross-validation of the rewrite, not a self-consistency check.
  - decision: `.json.sum` sidecars get no golden. They are 16-hex content
    hashes of an artifact that already has one; a golden there is churn.
- idempotence uses a SECOND batched load rather than reusing the first. The
  property is "generating over existing output changes nothing", and
  regeneration reads prior artifacts off disk; reusing a graph captured before
  any output existed would test something weaker.
- closure bug fixed as a side effect: the rewrite gives each subtest its own
  `t` (the old `CodegenTest` closed over the parent's).

### Proof

- `TestBasic`: 6.6s -> **2.3s**, all 13 fixtures + idempotence + acceptance.
- `go test ./...` green.
- **`internal/builder` now caches**: three consecutive runs all `(cached)`.
  This was the issue's headline complaint.
- testlog re-capture: transient in-module recorded inputs went
  **1,658 testfixtures + 364 test_run -> 0 and 0**.
- `go test -race -count=2 -shuffle=on ./internal/builder` green (219.8s under
  race instrumentation at count=2). No races and no order-dependence, which
  confirms the shared-graph analysis empirically: 13 fixtures generating in
  parallel off one loaded graph do not interfere.

## Correction: issue #132 was opened with the wrong scope

The user asked how to test IR generation separately from the projections. I
looked for an injectable seam, found that JSON Schema does not consume
`typegrammar`, and wrote #132 proposing to migrate JSON Schema onto
`typegrammar` -- a large refactor of the CLI's primary output path that the
user never asked for. I never checked whether the builder already had a seam.

It does. `internal/builder/model.go` already IS a standalone IR (`ObjectNode`,
`PropertyNode[T]`, `ArrayNode`, `UnionTypeNode`, `RefNode`, `RootSchema`,
`NullableObjectNode`, `NullableUnionNode`, `TemplateHoleNode`) and each
implements `MarshalJSON`. Verified `ObjectNode.MarshalJSON` reads only its own
fields and recurses into `prop.Schema.MarshalJSON()` -- no `SchemaBuilder`, no
package graph, no filesystem.

So:

- **JSON Schema projection is testable from hand-built nodes TODAY**, with zero
  production changes.
- **Go codec projection needs one small extraction**: `RenderGoCode` builds a
  `schemaTemplateData` but reaches into `s.customTypes`, `s.enumFields` and
  `s.Scan.GetPackage(...)` while doing so, so it is not pure. Split it the way
  `Run`/`RunLoaded` was split: build the data, then render the data.

#132 rewritten to describe that, with an explicit non-goal stating the
`typegrammar` migration is NOT proposed, plus a comment recording the error.

## Remaining

- Step 4 (`t.Parallel()` reconsideration) not started; revisit after #132 work
  since it changes the CPU picture.
- 3b dropped permanently.
- Nothing committed yet; branch `claude/slow-unit-tests-a036d9`.
- 3a `owner_codec_test.go` DONE (delegated), verified by diff rather than by
  the agent's report: every changed `require.` line is a *removal* of fixture
  plumbing (`os.WriteFile`/`MkdirAll`/`Cleanup`), zero additions.
  - decision: ONE legitimate exception to "no assertion may change".
    `baseImport` changed value from
    `"github.com/.../testfixtures/"+filepath.Base(targetDir)` to
    `fixtureModulePath`, and it is interpolated into two `require.Contains`
    calls (`owner_codec_test.go:266-267`). The assertions check generated
    import *aliases* (`events2`, `json1`); the path must track whatever module
    the fixture is really in, so moving both sides in lockstep preserves the
    property under test. Not a weakening: the alias names must still be
    generated correctly for it to pass.

## Session 2026-09-15 (continued): branch `claude/test-suite-speed`

Step 4 (`t.Parallel()`) taken up after all of #131/#132/#136 landed on main.

### Committed

- `82372fa` test: run internal/builder tests in parallel (71 insertions;
  10.9s -> 5.3s contended)
- `683d456` test: run codegen tests in parallel and stop mutating process env.
  `t.Setenv` is forbidden alongside `t.Parallel`, so `testutils.RunCommandEnv`
  was added and `TestGenWithDeclarationFilesPresent` now passes `TARGET`/`OUT`
  to the child directly. **No `t.Setenv`, `t.Chdir`, `os.Setenv` or `os.Chdir`
  remains anywhere in the tree outside testfixtures** -- verified by grep.
- `83d4d92` test: load a table's fixture cases in one `packages.Load`.
  Adds `fixtureCase`/`loadedCase`/`loadFixtureCases` to
  `internal/builder/fixture_module_test.go` and `builderFor` to
  `lowering_helpers_test.go`; converts
  `TestSealedUnionDiagnosticsNameTheType` as the exemplar.
- `3cbc0cb` test: run grammar tests in parallel. 5.07s -> 0.60s.
- `1fa035a` test: build the polytype CLI once per package. Four command tests
  used `go run .` (a full relink each) and two more built their own copy:
  six links per run, now one, via `TestMain`. 3.51s -> 2.01s.

### Measurement discipline

Per-package times from a parallel `go test ./...` run are contention-inflated
and must not be compared against each other. Everything above is measured with
the package run in isolation, on an otherwise idle machine.

**Corrected claim:** the earlier reading that `internal/syntax` costs ~6.4s was
contention. Isolated it is 1.75s, and the sum of its individual test times is
0.53s, so parallelising it is not worth doing.

### Negative result: codegen

Adding `t.Parallel()` to the eight fixture subtests in
`codegen/codegen_test.go` made the package **slower** (4.44s -> ~5.1s,
consistent over three runs). Each subtest spawns `go run`, which is already an
internally parallel build, so running eight at once oversubscribes the CPU.
Reverted. The real fix there is to build each `cmd/*` generator once instead of
`go run`-ing it per subtest, which is the same shape as issue #137.

### Delegated (isolated worktrees, mechanical batching conversions)

Note for future delegation: agent worktrees were provisioned from `ecddb2b`,
NOT from this branch's tip, so each agent had to fast-forward to
`claude/test-suite-speed` before it could see the exemplar.
