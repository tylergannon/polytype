# Adversarial review: warm analysis (round 02)

Date: 2026-10-14 (local)
Reviewer: Claude Fable 5.1, read-only except this artifact.

## Review target

- Branch `codex/warm-analysis`, HEAD `92efade fix: retain coherent source graphs across development edits` (committed by the implementing session while this review ran; the working tree was clean afterwards). Branch commits over `main`: `452d847`, `da484b4`, `a1420d7`, `92efade`.
- Authoritative sources unchanged from round 01: skgo issue #311 and comments, the caller's restated task (retain compatible parsed/type-checked analysis across ordinary captured-source edits without repeated Go metadata subprocesses; preserve constants, diagnostics, source/build configuration, coherent type identity; source-identity declarations and a retained `codegen.Worker` for Gimbal), `CLAUDE.md`, and the Session Worklog protocol.
- No scope narrowing was requested.

## Evidence inspected

- `git diff a1420d7..92efade` in full: `goload/session.go` (source-root signatures drop `ExportFile`; universe derivation now runs for any view-backed miss), `goload/view.go` (path normalization in `PrepareView`/`Update`), `internal/builder/cache.go` (expanded `go env` capture list), `internal/builder/programmatic_worker.go` (structural refresh retains `w.temp`, roots keyed by path hash, whole local closure loaded as one root set, symlink rejection removed, constraint layout in structural header, `workerPathError` with `Unwrap`), `codegen/worker_test.go` (`TestWorkerKeepsWholeLocalGraphAcrossLeafEdits`, `TestWorkerCapturesLinkedSourceBytes`), `goload/view_test.go`, worklog.
- Re-read `Run`, `initialize`, `capture`, `materialize`, `workerStructuralChange`, README worker/session sections, `codegen/worker.go` docs.
- Proof: `go test ./...` exit 0; `just lint` exit 0 and left the tree unchanged.
- Probes rerun from the session scratchpad (modules replacing `github.com/tylergannon/polytype` with this worktree), results below.

## Round 01 findings status

| # | Round 01 finding | Status |
|---|---|---|
| 1 | Leaf edits re-checked most of the closure | Fixed. 8-package chain: leaf body edits now `checks+1 hits+8` three times in a row; middle `p4` edit `checks+4` (itself plus three importers); root edit `checks+8`. Covered by `TestWorkerKeepsWholeLocalGraphAcrossLeafEdits`. |
| 2 | Import change discarded all analysis | Fixed. Adding `import "strings"` to the leaf: `metadataLoads+1 checks+1 hits+4`. Private root is now stable and the session is retained across structural refresh. Covered by the same test. |
| 3 | Symlinked source hard error | Fixed. Symlinked `.go` file generates and tracks target edits without rediscovery. Covered by `TestWorkerCapturesLinkedSourceBytes`. |
| 4 | `GOENV=off` dropped persisted settings | Fixed. `cache.go:103` now captures `GOPROXY`, `GOPRIVATE`, `GONOPROXY`, `GONOSUMDB`, `GOSUMDB`, `GOINSECURE`, `GOVCS`, `GOAUTH`, `CC`, `CXX`. |
| 5 | Flattened error chain; duplicated header logic | Error chain fixed (`workerPathError.Unwrap`). Header logic remains duplicated (see nitpick below). |

## Findings

### 1. issue — Retained analysis grows without bound on a caller-owned session

Evidence. `goload/session.go:290-296` (post-`92efade`): for a view-backed request (`discover != nil`) every incompatible load derives a fresh universe from the previous one and appends it to `s.universes`. Each derived universe shares unchanged package objects but keeps the superseded root's `types.Package`, `types.Info`, and syntax alive. `internal/builder/programmatic_worker.go:351-357`: `SetMaxUniverses(4)` is applied only when the Worker creates its own session; a caller-supplied `codegen.Options.Session` keeps the `Session` default of unlimited retention (`session.go:30-32`). The Worker tests and the Gimbal integration pass a caller session.

Reproduction (probe3: one dependency package plus a target file with 300 small functions, 60 successive body edits, `runtime.GC()` before each reading):

| session | edits | HeapAlloc | universe evictions |
|---|---|---|---|
| caller-owned, default | 20 | 32.3 MB | 0 |
| caller-owned, default | 40 | 61.8 MB | 0 |
| caller-owned, default | 60 | 91.3 MB | 0 |
| caller-owned, `SetMaxUniverses(4)` | 20 / 40 / 60 | 7.3 / 7.2 / 7.3 MB | 17 / 37 / 57 |

Growth is linear at roughly 1.5 MB per edit for this small fixture; a real application package retains proportionally more per edit.

Impact. Issue #311 states "LRU controls memory retention, never semantic validity" and requires evidence that "memory ... remain[s] bounded over sustained edits". A long-lived Gimbal development process that passes its own session (the documented way to share analysis with other generators) accumulates every edited revision until the process exits. Neither `codegen.Worker`'s doc comment (`codegen/worker.go:10-19`) nor the README Worker paragraph (`README.md:647-655`) tells the owner to bound the session; the README's `SetMaxUniverses` paragraph (`README.md:686-690`) describes the session as "an invocation reuse API, not an incremental development server", which is exactly what the Worker turns it into. Fix options: have the Worker apply a default bound to a caller session that has none (or document that the bound is the caller's responsibility and assert it in a test), and add a sustained-edit test that asserts evictions occur or heap stays flat.

### 2. nitpick — Structural-change detection still implemented twice

Evidence. `programmatic_worker.go:487-528` `workerStructuralChange` and `goload/view.go:324-362` `sourceHeader` now agree on `import "C"` and constraint-prefix layout, but remain separate implementations of the same decision (the worker includes a prefix slice when `//go:build` appears anywhere in the file, including inside a string or a trailing comment; the view includes it only for actual leading constraint lines). The disagreement only causes an extra re-acquisition, never a stale result, so this is maintenance risk rather than a defect.

### 3. nitpick — Cold run checks the whole local closure then hits it again

Evidence. Probe cold runs report `checks+8 hits+7` (8-package chain) and `checks+4 hits+3` (4-package chain): the whole-closure root load (`programmatic_worker.go:179-199`) checks every local package, then `LoadConfiguredPackageWithSource` requests the dependencies individually and hits each one. The hits are cheap, but the group load also type-checks local packages that no declaration needs source for; on a large application module this makes the first Worker run strictly more expensive than the ordinary path. Acceptable for a development server that amortizes it, but worth noting in the Worker docs.

## Requirement coverage check

- Retain parsed/type-checked analysis across ordinary edits without metadata subprocesses: met. Body, API, and import edits re-check only the changed package and its importers with no `go list` beyond the one documented structural rediscovery.
- Preserve constants and diagnostics: met; the typed-constant regression test and failed-edit retention are unchanged from round 01, and worker errors now unwrap.
- Preserve source/build configuration and coherent type identity: met; the expanded `go env` capture closes the round 01 gap, and `deriveUniverse` remains the single coherence boundary.
- Source-identity declarations and retained Worker: present and documented.
- Memory bounded over sustained edits: not met for a caller-owned session (finding 1).
- Repository instructions: new tests cover each round 01 fix, `go test ./...` and `just lint` pass, worklog records the correction and the compiler-latency experiment (kept under `ephemeral/`, not `docs/`).

## Outcome

material findings remain
