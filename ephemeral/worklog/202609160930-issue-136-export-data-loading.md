# Issue 136 — drop NeedDeps so dependency types come from export data

Branch: `claude/issue-136-export-data` (worktree). Base: `0263223` (main).

Goal: stop `syntax.Load` type-checking the whole dependency graph from source on
every load. Tracked in #136. Follows #133 (caching), #134 (typegrammar
projection) and #135 (projection tests).

- baseline on `0263223`, cold cache: `go test ./...` 36.8s wall / 182 CPU-s;
  `internal/builder` 26.2s wall / 97 CPU-s; `internal/schema` 0.24s.
- baseline confirms the #133 caching fix survived #134/#135: `internal/builder`
  reports `(cached)` on unchanged reruns, and no in-module fixture writes were
  reintroduced.

## Finding: the predicted hybrid was already in the architecture

#136 assumed this needed a hybrid — syntax for in-module deps (whose doc
comments become JSON Schema descriptions), export data for external ones.
Investigation showed the scanner already works that way:

- `ScanResult.deps` is **only** populated on demand
  (`internal/syntax/scan_result.go:726`), never eagerly seeded from the loaded
  graph.
- Nothing walks `pkg.Imports` (the `go/packages` dependency graph). Every
  `.Imports()` call site is **file-level AST imports** (`s.file.Imports`) used
  for prefix resolution, not the package graph.
- When the scanner needs a remote type it calls `loadDependency(pkgPath)` ->
  `LoadFrom(dir, pkgPath)` (`scan_result.go:744`), a fresh `decorator.Load`
  **with full syntax**. That is where dependency doc comments come from
  (`internal/syntax/enums.go:29` reads `typeSpec.Pkg().Syntax`).

So `packages.NeedDeps` forced the entire dependency graph to be parsed and
type-checked from source while nothing consumed it. Removing it lets
`go/packages` resolve imported types from export data, and the packages the
scanner actually inspects are still loaded from source on demand.

Change is one token: drop `packages.NeedDeps` from `PackageLoadNeeds`
(`internal/syntax/loader.go:15`).

## Proof

| | Before | After |
|---|---|---|
| `internal/builder` cold | 26.2s / 97 CPU-s | **12.0s / 30 CPU-s** |
| `go test ./...` cold | 36.8s / 182 CPU-s | **21.7s / 71 CPU-s** |

- 2.2x wall and 3.2x CPU on `internal/builder`; 1.7x wall and 2.6x CPU on the
  whole suite. Close to the ~3.4x per-load CPU figure predicted in #136.
- `go test ./...` green; `just build-tagged` exit 0.
- **AC3 (the real risk) verified directly**, not just inferred from green tests:
  descriptions sourced from a *dependency package's* doc comments still reach
  output. `indirectsubpkg`'s comments ("IntType is Foobarbax", "NamedNamedSliceType
  is another level of indirection from NamedSliceType") appear in
  `indirecttypes/jsonschema/*.golden`, and those goldens compare byte-for-byte
  under `TestBasic`'s compare-every-golden check from #133.
- All 57 goldens byte-identical (AC1), so the projection is unchanged.
