# Issue #131 — Test suite redesign plan

Branch: `claude/slow-unit-tests-a036d9`. Worktree:
`/Users/tyler/src/polytype/.claude/worktrees/slow-unit-tests-a036d9`.

Status at time of writing: investigation and prototype complete, **no production
changes committed**. Worktree clean, `go test ./...` green (33 s).

## 1. What the measurements actually showed

Baseline on this 10-CPU Mac: `go test ./...` = 33–42 s wall. `internal/builder`
is the long pole at ~30 s wall / ~138 s CPU. Everything else is cached or fast.

Corrections to the diagnosis in issue #131:

| Issue #131 claim | What measurement showed |
|---|---|
| Parallelizing the 59 serial tests is the top win | **No gain.** `t.Parallel()` on all of them: 30 s → 30–33 s wall, CPU 138 s → 293 s (kernel 47 s → 186 s). The box is already saturated; contention, not scheduling, is the limit. |
| Seven packages never cache | **Only `internal/builder` misses in steady state.** `grammar`, `internal/syntax`, `typescript`, `polytype`, `devalue/codegen` cached on three consecutive plain reruns. Their observed misses came from alternating run configurations (a different `GODEBUG`, the measurement wrapper's `GOROOT` symlink), which overwrite the `testID → testlog` entry. |
| `go generate`/`go run` is the main cost | **It is the minority.** `TestBasic` (all the `go generate` work) = 5.5 s wall / 39 s CPU. Every *other* builder test = 25 s wall / 100 s CPU. |

### Why `internal/builder` never caches (confirmed)

`cmd/go` refuses to save a test result when an in-module file the test read was
modified in the last 2 s (`modTimeCutoff`, `errFileTooNew` in
`cmd/go/internal/test/test.go`). Observed directly:

```
testcache: .../internal/builder: input file .../test_run/test12-optionality/go.mod: file used as input is too new
```

`TestBasic` rewrites the 227 tracked files under `internal/builder/test_run/`
right up to the end of the run. It is timing-dependent: CI *did* cache it on the
second pass of the same job; locally it never does. Note `cmd/go` ignores
`open`/`stat` of paths **outside** the module root, so `$TMPDIR` churn is
irrelevant — only in-repo writes matter.

**Correction (measured after the first draft): `test_run/` is not the only
poisoner, and it is not even the larger one.** Captured with
`go test -c -o $SP/builder.test ./internal/builder && (cd internal/builder &&
$SP/builder.test -test.testlogfile=$SP/builder.testlog)` — 91,436 recorded
inputs, of which **1,658 are under `testfixtures/` vs 364 under `test_run/`**.

The 18 `os.MkdirTemp(filepath.Join(cwd, "testfixtures"), ...)` helpers create
**74 distinct randomly-named fixture directories inside the module root**, write
Go files into them, read them back (recorded as `open`), and then delete them in
`t.Cleanup`. That is an unconditional cache miss, by two independent mechanisms:

1. Files are read milliseconds after being written → `errFileTooNew` at save
   time, same as `test_run/`.
2. Worse, and decisive: `computeTestInputsID` re-hashes every recorded input
   path at *lookup* time. These paths were hashed with content at record time
   and **do not exist at all on the next run** (random name + `RemoveAll`
   cleanup), so the recomputed ID can never equal the stored one.

Verified: after a full run, `ls -d internal/builder/testfixtures/*_[0-9]*`
matches nothing — every recorded path is gone.

**Consequence: step 2 alone does not make `internal/builder` cacheable.** It
removes one of two poisoners. Caching requires step 3, which moves these
fixtures out of the module root into the shared temp module.

### Where the CPU actually goes

`syntax.Load` uses `NeedDeps|NeedSyntax|NeedTypes`, so every load parses and
type-checks the entire dependency graph **from source**, stdlib included.
Per load of `examples/structs`, warm:

| | Wall | In-process CPU | `go` subprocess CPU |
|---|---|---|---|
| Current `syntax.Load` | 184 ms | **572 ms** | 121 ms |
| Same without `dst` | 184 ms | 559 ms | 119 ms |
| Deps from export data | 79 ms | 6 ms | 199 ms |

- `dst` decoration is **not** a significant cost (~13 ms).
- `go list`/`go env` is **not** the cost: 3 small calls, ~120 ms CPU per load.
- The cost is type-checking shared deps from source, ~122 times in `builder`
  ≈ 83 of its 100 non-`TestBasic` CPU-seconds.

### The key lever: one load is one load, regardless of package count

Batched `decorator.Load` into a single module:

| Packages in ONE load | Wall | CPU |
|---|---|---|
| 1 | 188 ms | 733 ms |
| 13 | 187 ms | 642 ms |
| 63 | 241 ms | 791 ms |

Cost is entirely the shared dependencies. This only works if fixtures live in
**one module**.

## 2. Prototype result (built, measured, then reverted)

Design: copy fixtures into one temp module → ONE `decorator.Load` → generate
in-process per fixture (parallel) → compare goldens → ONE
`go mod tidy` + `go vet ./...` + `go test ./...` as the e2e.

| Same 13 fixtures | Wall | CPU |
|---|---|---|
| Current `TestBasic` | 6.6 s | 49 s |
| Prototype | **2.3 s** | **5.6 s** |

Phases: materialize 20 ms · one load 214 ms · generate 13 + goldens 117 ms ·
e2e compile/vet/test 1.6 s.

Validated during the prototype:
- **No import rewriting needed.** Module path
  `github.com/tylergannon/polytype/internal/builder/testfixtures` makes every
  existing cross-fixture import (`.../enums/enumsremote`,
  `.../v1_enums_stringmode/palette`, `.../indirecttypes/indirectsubpkg`,
  `.../traversal/remoteenum`, `.../traversal/remotestruct`) resolve unchanged.
- **The repo `go.sum` covers the fixtures**; copy it in. One `go mod tidy` after
  generation (~30 ms) covers imports generation adds (`jsonschema/v6`).
- **No data race** (`-race`) with 13 fixtures generating in parallel off one
  shared loaded graph.
- Per-fixture generator config is trivial: `Pretty: true` always, plus
  `Validate: true` for `enums`, `optionality`, `union_codec`,
  `v1_enums_stringmode`, `v1_interfaces_options`.

## 3. Target design

Two layers, as proposed by the user:

1. **Unit layer (fast, golden-file based).** All fixtures materialized into one
   temp module, loaded once, generated in-process via the API, outputs compared
   against `.golden` files. No subprocesses.
2. **Acceptance/e2e layer (once).** After generation, a single
   `go mod tidy` + `go vet ./...` + `go test ./...` in that one temp module
   proves the generated code compiles and its runtime tests pass.

### Required production change (small)

Split `internal/builder/builder.go`:

```go
func Run(args BuilderArgs) error        // keeps the syntax.Load, then calls:
func RunLoaded(pkg *decorator.Package, args BuilderArgs) error  // rest of today's body
```

`args.TargetDir` is only used for the load, so the split is clean. This was
prototyped and worked unmodified.

### Repo changes

- Replace the 13 per-fixture `go.mod`/`go.sum` under
  `internal/builder/testfixtures/*/` with **one** nested module at
  `internal/builder/testfixtures/go.mod`:
  - `module github.com/tylergannon/polytype/internal/builder/testfixtures`
  - `replace github.com/tylergannon/polytype => ../../..`
  - Nested module stays excluded from the root `./...` pattern (same property
    the per-fixture modules provide today).
- At test time, copy that module to `t.TempDir()` and rewrite the `replace` to
  the absolute repo path. Nothing is written inside the repo module → the
  `errFileTooNew` cache poisoning disappears.
- Delete: 13 `gen/main.go` programs, their `//go:generate go run ./gen` lines,
  and the 227 tracked files under `internal/builder/test_run/`.

### Projected effect on `internal/builder`

| | Now | Projected |
|---|---|---|
| Wall | 30 s | ~5 s |
| CPU | 138 s | ~25 s |
| Unchanged rerun | always reruns | cached (0 s) |

## 4. Suggested execution order

1. **`RunLoaded` split** (production change, no behavior change). Self-contained.
2. **Convert `TestBasic`** to the single-module/one-load/in-process design plus
   the single e2e `go test`. Provable on its own: 6.6 s → 2.3 s, 49 → 5.6 CPU-s.
   Delete `test_run/`, the `gen/` programs and per-fixture `go.mod`s here.
   **Does not restore caching on its own** — see the correction in §1; the 74
   in-module `MkdirTemp` fixture dirs are the other half.
3. **Convert the inline-fixture tests. Split into 3a and 3b** — they fix
   different problems and have very different risk:

   - **3a — move fixtures out of the module root** into `t.TempDir()` plus a
     generated `go.mod`. This is what restores **caching**, which is the
     issue's actual complaint. Genuinely mechanical: no registry, no inversion
     of control, and the 8 sequential-`Run` tests need no special handling
     because each still loads fresh. The pattern already existed in-tree
     (`writeTypeGrammarFixture`, `writeMultiFileFixture`), so it is proven,
     not invented. Does **not** reduce CPU.
   - **3b — batch the loads** into one shared module + one `decorator.Load`.
     This is what reduces **CPU**, and it carries all the risk in §5: the
     inversion of control, the 8-test exclusion list, the `dirNames` updates.

   Doing 3a first delivers the headline benefit at low risk and leaves 3b as a
   separate, optional optimization. Note 3b is not the only way to get the CPU
   back — the export-data option below reaches a similar endpoint (~693 → ~205
   CPU-ms per load) with far less test churn, and helps real CLI users too.

   Remaining after both: ~83
   CPU-seconds live and is the bulk of the work. Give them a registry so each
   test's fixture source is materialized into the same shared module under a
   stable name before a single batched load; tests then fetch their package by
   name instead of calling `syntax.Load` themselves.
4. **Re-measure, then reconsider `t.Parallel()`.** It was worthless under CPU
   saturation but should pay once total work drops.
5. **Apply the same pattern to the other packages** if worthwhile — after
   `builder` is fixed the long poles are `codegen` (17 s), `polytype` (9 s),
   `grammar` (7.5 s), all sharing the per-test-load pattern.

Optional, separate issue: dependency loading from **export data** instead of
source (measured ~3× per load: 680 → 205 CPU-ms). Benefits the CLI for real
users too, but it is a production behavior change and the builder needs syntax
(doc comments) for in-module deps, so it needs a hybrid. Not needed if batching
lands.

## 5. Risks / open questions

- **Inline-fixture conversion is broad.** 80 test funcs, 18 `MkdirTemp` helpers,
  74 fixture dirs, 60 `Run(...)` calls. Measured risks, sharpest first:

  1. **8 tests call `Run` 2–4 times on the same directory**, each run reading
     back what the previous one wrote (`TestSwitchingSchemaOutputReplacesGeneratedFile`
     ×4, `TestGenerationPrunesOrphanedOwnedSchemas` ×4, `TestSealedUnionMembershipDrift`
     ×3, `TestGeneratedEnumCodecsRegenerateCleanly`, both `validation_removal`
     tests, two more in `declaration_file_test.go`). `RunLoaded` against a
     pre-loaded graph would see a **stale syntax tree predating the generated
     file**. These must keep fresh isolated loads. Converting them mechanically
     is a silent-correctness trap — a test can still pass for the wrong reason.
  2. **10 `dirNames` assertions change** when the per-fixture `go.mod`
     disappears (e.g. `["go.mod","polytype_gen.go","schema.go","types.go"]` at
     `declaration_file_test.go:127`). Mechanical, but these are exactly the
     assertions that catch orphaned-artifact bugs; updating them carelessly
     erodes real coverage.
  3. **Inversion of control, no partial credit.** Fixtures must exist *before*
     the one shared load, so every write-then-load helper splits into
     register-then-assert. Leaving some unconverted keeps their per-test loads
     and buys nothing.
  4. **Shared-graph mutation at 5× the prototype's scale** — 60 `Run` calls
     applying transforms against one graph, vs 13 `RunLoaded` calls proven
     race-free.
  5. **The payoff is not knowable up front**: how much of the ~83 CPU-s is
     recoverable depends on how many tests land in category 1.

  Diagnostics were a *smaller* risk than first drafted: assertions check
  relative names (`"types.go"`, `"schema.go:7:9"`) or build paths from the dir
  variable, so stable names don't break them.
- **Keep isolated loads** for tests that assert load *failures* (unloadable dir,
  "no packages found") and for the 8 sequential-`Run` tests above.
- **`go generate` coverage.** The CLI path loses its `builder`-level coverage.
  Confirm `polytype/` CLI tests and `examples_regenerate_test.go` cover it; keep
  exactly one fixture going through `go generate` end to end.
- **Shared-graph mutation.** No race was detected, but the builder must not
  mutate shared dependency packages when many fixtures share one load. Watch
  `internal/utils/rewriter` usage.
- **Measurements are warm-cache on macOS.** Cold CI (Linux, `internal/builder`
  = 108 s there) will differ in absolute terms; one module instead of 13 should
  still win.
- **Decision needed:** are the files under `internal/builder/test_run/`
  committed on purpose (reviewable generated output)? The plan makes them
  temp-only. If that review value matters, keep it via goldens instead.

## 6. Bugs found while investigating (fix alongside)

- **Stale golden:** `internal/builder/testfixtures/providers/jsonschema/Example.json.golden`
  has no generated counterpart — that fixture emits `Example.json.tmpl` under
  `WithRenderProviders()`. `TestBasic` never noticed because it only checks a
  hardcoded file list; the prototype's "compare every `.golden`" caught it
  immediately. Argues for the stricter check.
- **`TestBasic` closure bug:** `CodegenTest` closes over the *parent* `t`
  (`internal/builder/basic_test.go:29`), so a failing parallel subtest calls
  `FailNow` on the parent from the wrong goroutine and panics
  (`test executed panic(nil) or runtime.Goexit`), killing the binary before
  cleanup instead of reporting a normal failure. This also leaks fixture dirs
  and leaves modified `test_run` files behind.

## 7. Reproduction notes

- Cache reasons: `GODEBUG=gocachetest=1 go test ./<pkg>`. **Do not use it on
  `internal/builder`** — `GODEBUG` leaks into `TestBasic`'s nested `go`
  commands, whose stderr must be empty, so the test panics.
- Test inputs a package records: `go test -c -o /tmp/x.test ./pkg && (cd pkg &&
  /tmp/x.test -test.testlogfile=/tmp/x.log)`.
- Subprocess vs in-process CPU split: `syscall.Getrusage` with `RUSAGE_SELF` vs
  `RUSAGE_CHILDREN` around the phase under test.
- `go/packages` invocation log: set `packages.Config.Logf`.
