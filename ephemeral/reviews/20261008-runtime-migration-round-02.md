# Adversarial review: standalone devalue runtime migration (round 02)

Date: 2026-10-08 (local)
Branch: `codex/devalue-runtime-migration` at `62cb0c5` (worktree `/Users/tyler/.codex/worktrees/polytype-devalue-migration/polytype`), pushed; `origin/codex/devalue-runtime-migration` is the same commit. Tracked tree clean at the start of the review apart from one concurrent worklog edit noted below.
Reviewer: Claude Fable 5.1, read-only except this file.

## Review target

The three commits on top of `main` (`df6f4b6`) that remove `github.com/tylergannon/polytype/devalue` and point `devalue/codegen` and every fixture at the published `github.com/tylergannon/devalue/v5` v5.0.0:

- `b9d3739 feat: use the standalone devalue v5 runtime`
- `e77d243 docs: record runtime migration website validation`
- `62cb0c5 fix: retain generated fixture runtime dependencies` (the round-01 repairs)

Authoritative sources, unchanged from round 01:

- repository instructions (`AGENTS.md` / `CLAUDE.md`, session-worklog protocol);
- `/Users/tyler/src/devalue/ephemeral/polytype-migration.md` (Step 1 is the polytype scope; Step 2 is skgo and out of scope);
- `ephemeral/runtime-migration-validation.md` and `ephemeral/runtime-migration-pr.md` (the implementer's proof statements);
- `ephemeral/worklog/202610080205-devalue-runtime-migration.md`;
- `ephemeral/reviews/20261008-runtime-migration-round-01.md` (prior findings to re-check).

The caller's launch prompt set only operating constraints (artifact path, no other edits). No narrowing of scope was requested, so none was ignored.

## Evidence inspected

- Full `git diff main..HEAD` (74 files) and `git show --stat` of each commit.
- `devalue/codegen/generate.go`, `generate_test.go`, `cache_inputs_test.go`, `doc.go`; `internal/testutils/cache_inputs.go`; `codegen/codegen_test.go`.
- Fixture modules `codegen/testdata/{programmatic,recursive,recursive_declarations}` and `devalue/codegen/testdata/fixture` (`go.mod`, `go.sum`, test files carrying the blank runtime import, generated codec).
- Docs: `README.md`, `AGENTS.md`, `skills/polytype/SKILL.md`, `skills/polytype/references/devalue-and-grammar.md`, `website/src/content/docs/guides/devalue.md`, `website/src/content/docs/api/index.md`, `website/package.json`, root `package.json`, `.github/workflows/go.yml` and `website-pages.yml`, `.releaserc`.
- Published module `github.com/tylergannon/devalue/v5@v5.0.0` in the module cache (`go.mod`, `go doc` of the exported API, `stringify.go`/`parse.go`).
- `ephemeral/runtime-consumer/` (go.mod, main.go, generated codec, result.txt), `ephemeral/runtime-cli-install.txt`, `ephemeral/runtime-migration-website.txt`.

Proof I re-ran myself from the worktree:

| Check | Result |
| --- | --- |
| `go test ./...` | pass, no failures |
| `go test ./codegen -run TestRecursiveDevalueJSInterop -v -count=1` | PASS, not skipped (devalue 5.9.4 JS pin exercised) |
| `just lint` | exit 0 and no tracked Go file modified afterwards (round-01 finding 2 resolved; `grammar/grammar.go` rewrite is now committed in `62cb0c5`) |
| `just build-tagged` | exit 0 |
| `JSONSCHEMA_NO_CHANGES=1 go generate ./...` | exit 0, no drift |
| `go mod tidy -diff` in root, `programmatic`, `recursive`, `recursive_declarations`, `devalue/codegen/testdata/fixture`, `ephemeral/runtime-consumer` | all clean (round-01 findings 1 and 3 resolved) |
| `gomarkdoc` regeneration of the API index into scratch, same package list as `website/package.json` `prebuild` | byte-identical to the committed `website/src/content/docs/api/index.md` |
| `grep` for `polytype/devalue"`, `testdata/record`, `golden.json`, `dop251/goja`, `TestUpstreamVersion` outside `ephemeral/` | only the README migration note; no code, workflow, golden or website reference survives |
| `go doc github.com/tylergannon/devalue/v5` | `UpstreamVersion = "5.9.4"`, `NewObject`, `Stringify`/`StringifyWith`/`Parse`/`Uneval`/`UnevalWith`, `Undefined`, `Hole`, `Date`, `*Map`, `*Set`, `BigInt`, `RegExp`, `ArrayBuffer`, `*TypedArray`, `*DataView`, `*Boxed` all exist; `TypedArray` is referenced from both `stringify.go` and `parse.go`, so the docs' new claim that typed arrays and DataView are flat-format tagged forms is true for v5.0.0 |
| `ephemeral/runtime-consumer/result.txt` | `standalone-runtime-wire-and-roundtrip=ok`, `upstream=5.9.4`, exact wire `[{"name":1,"values":2},"Ada",[3,4],1,2]`; its `go.mod` requires `devalue/v5 v5.0.0` with no runtime replace and a local polytype replace, as the validation note states |
| `ephemeral/runtime-migration-website.txt` | Astro build completes, 16 pages built, `Checked 20 HTML pages; all internal links resolve.` |
| Commit types vs `.releaserc` | `feat:` on `b9d3739` yields a minor release via commit-analyzer, matching the plan's "released as a minor" decision; no `BREAKING CHANGE` footer, which is correct given the explicit decision not to bump the major |

Plan Step 1 checklist against the committed tree: every item verified in round 01 still holds. `devaluePackagePath` is `github.com/tylergannon/devalue/v5` (`devalue/codegen/generate.go:114`); the second `TrackFixtureDependencies` call is at `devalue/codegen/generate_test.go:29`; `cache_inputs_test.go:21-23` now asserts the literal path with a blank import (round-01 nitpick 4 resolved); the worklog is renamed with its `HHMM` component (round-01 nitpick 5 resolved); `website/pnpm-lock.yaml` is not tracked and no longer present.

## Round-01 findings status

| # | Finding | Status |
| --- | --- | --- |
| 1 | `programmatic` / `recursive_declarations` `go.mod` untidy; tidy would drop the runtime require | Fixed in `62cb0c5`: blank `_ "github.com/tylergannon/devalue/v5"` imports with a one-line comment in `codegen/testdata/programmatic/model/roundtrip_test.go:5` and `codegen/testdata/recursive_declarations/devalue_test.go:5`; requirement now in the direct block; `go mod tidy -diff` clean |
| 2 | `just lint` not idempotent (`grammar/grammar.go` modernize drift) | Fixed in `62cb0c5`: the goimports grouping and `strings.CutPrefix` rewrite are committed; lint leaves the tree clean |
| 3 | `devalue/codegen/testdata/fixture/go.sum` untidy | Fixed: 18 transitive sums added; tidy clean |
| 4 | Reflection-derived runtime path in `cache_inputs_test.go` | Fixed: literal string plus blank import |
| 5 | Worklog name missing `HHMM` | Fixed: `202610080205-devalue-runtime-migration.md` |

## Findings

No material findings remain. The nitpicks below are the only items left.

### 1. nitpick — the README still carries a migration notice inside the reference section

`README.md:676-678` opens "The runtime: `devalue/v5`" with "The bundled `github.com/tylergannon/polytype/devalue` runtime has been removed. Update runtime imports to ... and regenerate typed codecs." This is the one remaining mention of the old path outside `ephemeral/`. It is appropriate for the release that ships the break, but it is a changelog sentence living in evergreen reference prose; a reader a year from now will not know what "bundled runtime" refers to. Suggested: move it to the release notes (the `feat:` commit body or the PR description, which `ephemeral/runtime-migration-pr.md` already contains) and keep the README section purely descriptive. Not applied.

### 2. nitpick — an uncommitted worklog correction appeared during this review

`git status` showed `ephemeral/worklog/202610080205-devalue-runtime-migration.md` modified (a new `correction:` line about the literal-path assertion and the superseded decision to discard grammar drift). Nothing I ran writes to that file, so it came from the implementing session concurrently. The content is correct and belongs in the branch; the session-worklog protocol requires it to be committed with the final branch state before closeout. Not a defect in the migration itself.

### 3. nitpick — the website proof was produced outside the CI path

`ephemeral/runtime-migration-website.txt` shows the dependencies were installed with pnpm against the npm-locked `website/` (the `ERR_PNPM_IGNORED_BUILDS` error and the "lockfile has changed" warning), after which the Astro build and link check succeeded. CI (`website-pages.yml`) runs `npm ci` then `npm run check`, which is the path that actually gates the site, and that path was not reproduced locally. The risk is low: the committed `package-lock.json` is unchanged by this branch, the only website edits are a markdown guide, the regenerated API index and the `prebuild` package list, and I reproduced the gomarkdoc `prebuild` step byte-for-byte. Worth running `npm ci && npm run check` once (or letting the first CI run stand as proof) rather than relying on the pnpm transcript.

## Observations outside the findings

- `internal/builder/testfixtures/go.mod` and `go.sum` are not tidy (`go mod tidy -diff` wants to drop the direct `santhosh-tekuri/jsonschema/v6` require and most sums). That module is untouched by this branch (`git diff main..HEAD` is empty for it), so it is pre-existing and out of scope here; noted so the next `just update-deps` run does not surprise anyone.
- Root `go.sum` still lists goja, regexp2, sourcemap and pprof. That is correct: `devalue/v5`'s own `go.mod` requires them, and `go mod tidy -diff` on the root is clean.
- `codegen/codegen_test.go` has no `TrackFixtureDependencies` call for either module. That is pre-existing (it never tracked the polytype replace either) and the fixtures pin `devalue/v5` at an immutable version, so the migration does not widen any cache-staleness exposure.
- The proxy install proof (`ephemeral/runtime-cli-install.txt`) was taken at `e77d243`, one commit before `62cb0c5`. The later commit changes only fixture modules, a test, `grammar/grammar.go` formatting and ephemeral files, none of which affect the CLI binary's module graph, so the proof still describes the shipped CLI. Final publication proof is correctly deferred to the release tag.
- Plan Step 2 (skgo) remains out of scope and is not claimed.

## Outcome

only nitpicks remain
