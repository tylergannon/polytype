# Adversarial review: standalone devalue runtime migration (round 01)

Date: 2026-10-08 (local)
Branch: `codex/devalue-runtime-migration` (worktree `/Users/tyler/.codex/worktrees/polytype-devalue-migration/polytype`), uncommitted working tree on top of `df6f4b6`.
Reviewer: Claude Fable 5.1, read-only except this file.

## Review target

The uncommitted migration that removes `github.com/tylergannon/polytype/devalue` and points `devalue/codegen` and every fixture at the published `github.com/tylergannon/devalue/v5` v5.0.0, as specified by:

- repository instructions (`AGENTS.md` / `CLAUDE.md`, session-worklog protocol);
- `/Users/tyler/src/devalue/ephemeral/polytype-migration.md` (Step 1 is the polytype scope; Step 2 is skgo and out of scope here);
- `ephemeral/runtime-migration-validation.md` (the implementer's proof statement);
- `ephemeral/worklog/20261008-devalue-runtime-migration.md`.

The caller's launch prompt set only operating constraints (artifact path, no other edits). No narrowing of scope was requested, so none was ignored.

## Evidence inspected

- Full `git diff` and `git status` of the working tree (58 tracked files, 4 untracked paths at start).
- `devalue/codegen/generate.go`, `generate_test.go`, `cache_inputs_test.go`, `doc.go`; `internal/testutils/cache_inputs.go`; `codegen/codegen_test.go` (fixture harness, `TestRecursiveDevalueJSInterop`).
- Fixture modules: `codegen/testdata/{programmatic,recursive,recursive_declarations}` and `devalue/codegen/testdata/fixture` (`go.mod`, `go.sum`, generated codec, tests).
- Docs: `README.md`, `AGENTS.md`, `skills/polytype/SKILL.md`, `skills/polytype/references/devalue-and-grammar.md`, `website/src/content/docs/guides/devalue.md`, `website/src/content/docs/api/index.md`, `website/package.json`, root `package.json`, `.github/workflows/*.yml`.
- `ephemeral/runtime-consumer/` (scratch consumer) and the published module's `go.mod` in the module cache.

Proof I re-ran myself (all from the worktree):

| Check | Result |
| --- | --- |
| `go test ./...` | pass, no failures |
| `go test ./codegen -run TestRecursiveDevalueJSInterop -v -count=1` | PASS (not skipped; JS devalue 5.9.4 pin exercised) |
| `just lint` | exit 0, but `modernize -fix` rewrote `grammar/grammar.go` (see finding 2); I reverted that file with `git checkout` |
| `just build-tagged` | exit 0 |
| `JSONSCHEMA_NO_CHANGES=1 go generate ./...` | exit 0, no drift |
| `gomarkdoc` regeneration of `website/src/content/docs/api/index.md` into scratch | identical to committed file apart from the Starlight frontmatter the `-e` embed preserves |
| `go run .` in `ephemeral/runtime-consumer` | prints `standalone-runtime-wire-and-roundtrip=ok`, `upstream=5.9.4`, exact wire `[{"name":1,"values":2},"Ada",[3,4],1,2]` |
| `go mod tidy -diff` root | clean (goja and friends remain in `go.sum` only because `devalue/v5`'s own `go.mod` requires them; correct) |
| `go mod tidy -diff` in each fixture module | `programmatic` and `recursive_declarations` NOT tidy (finding 1); `recursive` tidy; `devalue/codegen/testdata/fixture` `go.sum` is missing transitive test sums (nitpick) |
| `grep` for `polytype/devalue"` outside `devalue/codegen` | only the README migration note and two historical `ephemeral/` design notes remain; no code, golden, workflow or doc reference survives |

Plan Step 1 checklist against the tree: `go get devalue/v5@v5.0.0` done; `devaluePackagePath` updated; recursive fixture and `devalue/codegen` fixture regenerated and their `codec_test.go` repointed; second `TrackFixtureDependencies` call added; runtime files and `devalue/testdata/` deleted (`devalue/` now holds only `codegen`); goja dropped from `go.mod`; root JS devalue pin kept and justified by `TestRecursiveDevalueJSInterop` (the validation doc correctly records that the plan's "recorder only" statement was wrong); the seven listed `ephemeral/` notes deleted and `ephemeral/devalue-projection/` kept; README, AGENTS.md, skill references and website guide rewritten; `../devalue` removed from the gomarkdoc `prebuild` list and the API index regenerated. `.github/workflows/website-pages.yml` still triggers on `devalue/**`, which continues to match `devalue/codegen`; no workflow referenced the deleted recorder or goja.

## Findings

### 1. issue — `programmatic` and `recursive_declarations` fixture `go.mod` files are untidy, and `go mod tidy` silently removes the requirement the tests depend on

Evidence: `codegen/testdata/programmatic/go.mod:9` and `codegen/testdata/recursive_declarations/go.mod:9` add `github.com/tylergannon/devalue/v5 v5.0.0` inside the `// indirect` require block, without an `// indirect` marker and without any committed source importing it. `go mod tidy -diff` in either directory outputs a diff deleting that line. The requirement is nevertheless load-bearing: `codegen/codegen_test.go:110-125` and `:232-282` copy these modules to a temp dir, generate `codec_gen.go` (which imports `devalue/v5`) and run `go test` inside the copy under the default `-mod=readonly`, so without the require the generated package fails with "no required module provides package github.com/tylergannon/devalue/v5".

Impact: the first agent or developer who runs `go mod tidy` in a fixture (the obvious reaction to a `go.sum` complaint, and what `just lint` does at the root) breaks `TestCodegen*` and `TestRecursiveDeclarations*` with no hint that the generator output is the reason. Before this migration the runtime lived inside the replaced polytype module, so the fixtures carried no such trap; the migration introduced it. The `devalue/codegen/testdata/fixture` module avoids the problem only because its committed `codec_test.go` imports the runtime, and the `recursive` fixture because its committed `codec_gen.go` does.

Suggested repair (not applied): make the dependency visible to tidy in each fixture, for example a `_ "github.com/tylergannon/devalue/v5"` import in an existing test file such as `recursive_declarations/devalue_test.go` and `programmatic/model/roundtrip_test.go` with a one-line comment that generated codecs import it, then run `go mod tidy` so the require lands in the direct block. At minimum, move the line into the direct `require` block with a comment explaining why tidy must not remove it.

### 2. issue — `just lint` is not idempotent on this branch: `modernize -fix` rewrites `grammar/grammar.go`

Evidence: running `just lint` from the worktree exits 0 but leaves `grammar/grammar.go` modified (import grouping via goimports, and `strings.HasPrefix`/`TrimPrefix` → `strings.CutPrefix` at the `-tags=` branch of `LoadWithConfig`). `git diff main -- grammar/grammar.go` is empty, so the drift is pre-existing from #161, not introduced here. The worklog records the decision to discard these edits to keep the change scoped.

Impact: the plan's proof criterion is "`just lint` ... pass", and the repository's standing practice (memory: clean checks before any PR, including pre-existing lint failures) treats a lint run that mutates tracked files as not clean. Anyone running `just lint` before committing this branch will pick up unrelated `grammar/grammar.go` edits into the migration commit, or must remember to revert them each time. CI's `go.yml` does not run `just lint`, so this will not fail CI, which is also why it survived #161.

Suggested repair (not applied): either commit the modernize/goimports rewrite of `grammar/grammar.go` as a separate `chore:`/`style:` commit ahead of this change, or accept it into this branch explicitly. Discarding it repeatedly is the one option that leaves the tree unclean.

### 3. nitpick — `devalue/codegen/testdata/fixture/go.sum` is new but not tidy

`go mod tidy -diff` in that module wants to add the transitive test-dependency sums of `devalue/v5` (goja, regexp2, sourcemap, pprof, testify, difflib). `TestGeneratedCodecsCompileAndRun` passes without them because it only builds and tests the fixture, so this is cosmetic. It is untracked and must be `git add`ed with the rest of the change; nothing is staged yet.

### 4. nitpick — the root module now requires `devalue/v5` only through a test-side `reflect.TypeFor[devalue.Object]().PkgPath()` import

`devalue/codegen/cache_inputs_test.go:16-20` imports the runtime so `go mod tidy` keeps it in the root `go.mod`, which `TrackFixtureDependencies` (`internal/testutils/cache_inputs.go:40`) needs because it resolves fixture imports with `go list -deps` from the test's own package. The intent is right and documented in the comment and worklog, but deriving the path via `reflect` obscures it: the assertion no longer pins the literal string that `generate.go:114` emits, so a typo in `devaluePackagePath` would no longer be caught by this test (it would still be caught by the compile-and-run fixture). A plain `require.Contains(t, imports, "github.com/tylergannon/devalue/v5")` next to a blank import would be clearer and keep the literal pinned.

### 5. nitpick — worklog file name omits the `HHMM` component

`ephemeral/worklog/20261008-devalue-runtime-migration.md` does not follow the session-worklog `YYYYMMDDHHMM-<task>` pattern that every other worklog in the directory uses. Content otherwise follows the protocol (decision, friction, doc_bug lines; no transcript).

## Observations outside the findings

- `website/pnpm-lock.yaml` appeared as an untracked file during this review (mtime 03:01 local) alongside the committed `package-lock.json`. Nothing I ran invokes pnpm, so it most likely comes from a concurrent session in this worktree; it is not part of the migration and should not be committed with it.
- The README's devalue section now reads as a migration notice ("The bundled ... runtime has been removed. Update runtime imports ...") inside the reference prose. That is appropriate for the minor release that ships the break, but is worth trimming to a changelog-style note in a later pass.
- Release policy: the plan calls for a minor release, so the eventual commit needs a `feat:` type (or a footer the semantic-release config treats as minor); there is no commit yet to check.
- Plan Step 2 (skgo) and the post-publication "fresh no-replace CLI install and rerun the consumer with the released polytype version" from the validation note are correctly deferred and not claimed as done.

## Outcome

material findings remain
