# Adversarial review: warm analysis (round 01)

Date: 2026-10-14 (local)
Reviewer: Claude Fable 5.1, read-only except this artifact.

## Review target

- Branch `codex/warm-analysis` in worktree `/Users/tyler/.codex/worktrees/polytype-warm-analysis`.
- Authoritative task: skgo issue #311 ("Make SKGO development edits feel as fast as SvelteKit, with reusable warm analysis and reload") and the caller's restatement: Polytype should retain compatible parsed/type-checked analysis across ordinary captured-source edits without repeated Go metadata subprocesses, preserving constants, diagnostics, source/build configuration and coherent type identity; source-identity declarations and a retained `codegen.Worker` serve the Gimbal consumer. Repository instructions: `CLAUDE.md` (tests required for new functionality, `go test ./...` must pass) and the Session Worklog protocol.
- Working tree during review: at the start the implementation was uncommitted on top of `da484b4`; midway through the review another session committed it as `a1420d7 feat(codegen): retain captured analysis between development edits` and began a further uncommitted edit to `goload/view.go` / `goload/view_test.go` (normalizing trailing-slash directory markers in `PrepareView`/`Update`, plus `TestPreparedViewNormalizesDirectoryMembershipMarkers`). This review covers `a1420d7` (with `452d847` and `da484b4` as surrounding branch context) and notes the in-flight edit; the in-flight edit was not fully re-verified.
- Scope narrowing: none was requested; the caller's instructions were operating constraints only (read-only, artifact path).

## Evidence inspected

- Issue #311 body and both comments (via `gh issue view`).
- `git diff` of the implementation (README, `codegen/codegen.go`, `configuration.go`, `go.mod`, `goload/view.go`, `goload/view_test.go`, `internal/builder/cache.go`, `internal/builder/programmatic_cache.go`) and new files `codegen/worker.go`, `codegen/worker_test.go`, `internal/builder/programmatic_worker.go`; `ephemeral/worklog/warm-analysis.md`.
- Surrounding code: `goload/session.go` (`Session.load`, `deriveUniverse`, `supported`, `contextKey`), `goload/view.go` (`discover`, `key`, `updatedHeaders`, `updateSources`), `internal/builder/generated_source.go`, `internal/syntax/scan_result.go` (`LoadConfiguredPackageWithSource`, dependency loader), `codegen/source_identity_test.go`.
- Proof: `go test ./...` exit 0 (codegen 10.8s, internal/builder 8.9s, rest cached); `just lint` exit 0 and left the tree unchanged (the two modified goload files are the other session's in-flight edit, confirmed by diff content).
- Two scratch probe programs outside the repository (scratchpad `probe/` and `probe2/`, a module replacing `github.com/tylergannon/polytype` with this worktree), results reproduced below.

## Findings

### 1. issue — Leaf-package body edits re-type-check most of the local closure; retention is fragmented across universes

Evidence. `internal/builder/programmatic_worker.go:174-181`: the worker loads the target with `load(".")`, then `syntax.LoadConfiguredPackageWithSource` requests each dependency that needs source scanning through the same loader (`internal/syntax/scan_result.go:789`), one import path per call. `goload/session.go:286-323`: a package already imported from export data cannot be promoted to source in the same universe, so each dependency source request lands in a new universe (the documented "separate universe" rule). On a later edit the target's check is placed in whichever universe is "compatible" (`session.go:276-301`), frequently one where the dependencies are export-imported, and every dependency source load then bounces to a different universe.

Reproduction (probe2, 8-package import chain `p0 <- p1 <- ... <- p7`, `DeclareType` on `p7.T`, caller-owned `goload.Session`, `codegen.Worker`):

| edit | MetadataLoads | PackageChecks | PackageHits |
|---|---|---|---|
| cold | +1 | +8 | +0 |
| leaf body edit 1 (`p7` only) | +0 | +7 | +1 |
| leaf body edit 2 | +0 | +1 | +7 |
| leaf body edit 3 | +0 | +5 | +3 |
| middle `p4` body edit | +0 | +7 | +6 |
| root `p0` body edit | +0 | +8 | +7 |

Impact. The issue's primary goal is retaining compatible type-checked analysis across ordinary edits. A body edit to the leaf alone should cost one check and seven hits; it costs seven, then one, then five, depending on universe LRU order. On Gimbal's package counts this is O(closure) re-checking per edit rather than O(changed). Correctness holds (each check is coherent), but the "warm" claim is not delivered reliably, and `TestWorkerEditsRepairAndReplay` (`codegen/worker_test.go:82`) only asserts `PackageChecks` increased, so the test suite cannot detect this. The subprocess goal is met (MetadataLoads +0), so this is an efficiency gap, not a correctness defect. The Session design predates this change, but the Worker is the component that promises the retention, and it drives the loader in the pattern that causes fragmentation. A worker-side fix is available without changing Session semantics: request the local-module closure as source roots in one load (so one universe holds all local packages from source) instead of root-then-per-dependency.

### 2. issue — Any import, membership, or go.mod change discards all retained analysis and reruns full acquisition

Evidence. `programmatic_worker.go:113-118`: a structural snapshot change calls `w.Close()` then re-enters `Run`. `Close` (`:44-54`) zeroes the struct, removes `w.temp`, and drops `w.view`; when the Worker owns its session (`:319-323`) the session is dropped too. Re-initialization creates a fresh `os.MkdirTemp` root (`:263`), so every private file path changes; `goload.signature` (`session.go:546`) includes `CompiledGoFiles`, so no previously retained package is compatible even with a caller-owned session, and `acquireCacheFor` (`cache.go:92-121`) reruns `go list -deps -export`, `go env`, and re-hashes the generator binary.

Reproduction (probe, 4-package chain, caller-owned session): adding `import "strings"` to the leaf `model/row.go` yields `metadataLoads+1 checks+4 hits+0`, versus `checks+3 hits+1` for a field edit to the same file. The `go list` subprocess is not visible in session stats but is unconditional in `acquireCacheFor`.

Impact. Adding or removing an import is an ordinary development edit. The issue allows structural changes to "trigger authoritative discovery", so re-running `go list` is defensible, but discarding every type-check result and the whole private tree is an unrequested cost: a stable per-Worker private root (reuse `w.temp` across re-acquisition) would keep paths and therefore signatures stable, letting `deriveUniverse` retain unchanged packages. With a self-owned session the loss is total. Also the recursion `return w.Run(...)` after `Close` has no guard; it terminates because the re-acquired snapshot is compared against `nil`, but a capture error on the second pass is returned as a hard error rather than a bypass (see finding 3).

### 3. issue (verifiable bug) — A symlinked source file makes the Worker fail permanently instead of falling back

Evidence. `programmatic_worker.go:344-346` returns `fmt.Errorf("symlinked mutable source %s", name)` from `capture`, and `Run` (`:109-112`) propagates it as `true, err`, so `codegen.Generate` never reaches the ordinary path. The plain path has no such restriction.

Reproduction (probe): after a successful run, create `a/linked.go` as a symlink to a regular file in a captured package directory.

```
symlinked source (worker)    err=symlinked mutable source /var/folders/.../probe-4151854227/a/linked.go
symlinked source (plain Generate) err = <nil>
```

Impact. A project with one symlinked Go file (Bazel/Nix style layouts, vendored-by-link helpers) cannot generate through `codegen.Worker` at all, every call fails, and the README ("other output modes ... keep the ordinary Generate path") implies unsupported shapes degrade rather than error. The intended contract for "unsupported" shapes elsewhere in `Run` is `false, nil` (bypass). This should be a bypass, or the symlink target should be captured by content.

### 4. nitpick — `GOENV=off` drops persisted settings that are not in the captured environment list

Evidence. `cache.go:103` captures a fixed list via `go env -json`; `programmatic_worker.go:160-168` replays only that list into the private load and sets `GOENV=off` and `GOWORK=off`. Persisted `go env -w` values outside the list (`GOPROXY`, `GOPRIVATE`, `GONOSUMDB`, `GOSUMDB`, `GONOSUMCHECK`, `GOINSECURE`, `GOVCS`, `GOAUTH`, `CC`, `CXX`) are silently lost for the private tree.

Impact. Low today: modules are already in `GOMODCACHE` after acquisition and `go.mod` edits re-acquire in the original tree first, so `go list` in the private tree should not need the network. It becomes observable if `GOFLAGS` carries `-mod=mod` and a module is missing from the cache, where the private load could consult the public proxy for a private module. Either capture the full `go env -json` output or document the limitation.

### 5. nitpick — Diagnostics lose their error chain, and structural-change detection is implemented twice with different rules

Evidence. `programmatic_worker.go:492-507` `originalError` rewrites private paths but rebuilds the error with `fmt.Errorf("%s", message)`, flattening wrapped errors; a consumer cannot use `errors.As`/`errors.Is` on worker diagnostics although the same diagnostics from `Generate` keep their chain. `workerStructuralChange` (`:446-491`) and `goload.sourceHeader` (`goload/view.go:324-362`) answer the same question with different rules (the worker ignores build-constraint placement and relies on the comment list; the view hashes the constraint's prefix layout and treats `import "C"` specially). They agree in the tested cases, but divergence is a maintenance trap. The in-flight uncommitted edit to `goload/view.go` shows this surface is still moving.

Also noted, not counted as findings: `DeclareType` no longer validates the package path with `module.CheckImportPath` (`configuration.go:43-48`); `TestDeclareTypeRejectsInvalidIdentity` still passes because it only covers an empty path, so the weakened validation is untested but harmless (resolution reports the missing package). The persisted plan is only saved for the first run of a process (`:203-205`), which is documented in the worklog. `Stats()` on the Worker is not asserted by any test.

## Requirement coverage check

- Retain parsed/type-checked analysis across ordinary edits without metadata subprocesses: partially met. Metadata subprocesses are avoided for body/API edits (MetadataLoads +0), but analysis retention is unreliable (finding 1) and any import change throws everything away (finding 2).
- Preserve constants and diagnostics: met for the tested direction. `TestWorkerEditsRepairAndReplay` changes a typed transitive constant that alters an array length, requires the error on two consecutive attempts, verifies the last good output is retained, and verifies repair. `deriveUniverse` invalidates reverse importers conservatively.
- Preserve source/build configuration and coherent type identity: met with the limitations in finding 4. Workspaces, cgo, embed, vendored/mutable versioned dependencies, custom drivers, overlay/modfile/toolexec flags, and custom loader configuration bypass to the established path; local replacements are relocated without being promoted to workspace members (`TestWorkerLocalReplacementsAndModuleEdit`).
- Source-identity declarations and retained Worker for Gimbal: present (`DeclareType`, `codegen.Worker` with `Generate`/`Stats`/`Close`), README documents both.
- Repository instructions: unit tests for the new functionality exist and pass; `just lint` is clean; the worklog records decisions and corrections.

## Outcome

material findings remain
