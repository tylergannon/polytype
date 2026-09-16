# Post-v1 refactor review (22ea516..dfb5a7c) and missing-golden guard

## Review method
- decision: judged behavior drift by building the CLI at 22ea516, 9c6ca60^, dafd00d
  (v1.0.1) and HEAD and diffing generated trees over every example/fixture, plus
  targeted probe packages. The checked-in corpus was byte-identical (JSON Schema,
  TypeScript); every regression found needed a shape the corpus does not cover.
- friction: the repo corpus alone would have "proved" the refactor lateral ->
  fixtures need the shapes filed below before another lowering refactor.

## Regressions filed
- #146 cross-package struct with sealed-union field fails generation (v1.0.3, #134)
- #147 embedded `json:"-"` renders a required "-" property (v1.0.3, #134)
- #148 #140 admits ref-tagged/provider named union slices (unreleased)
- #149 codec-only root skips codecs for types reached only via ref= (v1.0.3, #134)
- #150 explicit ref= tag ignored on union/StringerEnum fields (v1.0.3, #134)
- #151 declarations in untagged files silently ignored (v1.0.2, #130)
- #152 TestBasic acceptance results can be stale in the test cache (#133)

## Open decisions for the maintainer
- decision-pending: whether codec-only roots (`Declare[T]()`) should reject every
  shape the grammar cannot lower (json.RawMessage, error, unsealed interfaces,
  omitempty strings) as they do since #134, or stay lenient as before.
- decision-pending: whether untagged declarations are an error or a warning (#151).

## Change made
- `internal/builder/basic_test.go`: `assertGoldens` now also fails when a generated
  artifact (jsonschema_gen.go, polytype_gen.go, jsonschema/* except .golden/.sum)
  has no golden. Proven by deleting two goldens in a scratch copy: TestBasic fails
  naming both artifacts. `TestArtifactsWithoutGoldens` covers the classifier.
- `go test ./...` and `just lint` clean.

## Fixing the filed regressions (one PR)
- decision: #150 and #149 share one change (explicit ref checked first; the
  static value behind a Provided field is lowered speculatively and used by
  codec planning), so they landed in one commit.
- decision: #148 is enforced inside that speculation: failures roll back,
  except sealed-union misuse (`sealedUnionMisuse`), which still refuses.
- decision: #151 refuses only blank `var _ =` markers in ordinary files; named
  values stay legal executable configuration (TestOrdinaryFileIsNotReadAsDeclarations).
  A "no declarations at all" error was not added.
- correction: first #152 attempt used blank imports; the cache stayed stale
  because the linker drops unreferenced code, so the binary hash is unchanged.
  Reading dependency sources from the test process (testlog-tracked) works.
- friction: embedding a foreign type that has generated codecs is refused by a
  check that reads the dependency's generated files from disk, so its outcome
  in TestBasic depends on parallel generation order -> crosspkg_union embeds a
  codec-free foreign struct instead. Worth a follow-up if that shape matters.
- friction: no golden-update flag; regenerated goldens by running the worktree
  CLI with TestBasic's flags on a scratch copy of the fixture module.
- friction: zsh does not word-split `$VAR` file lists; a revert-and-test
  command failed before reverting (harmless), and a broad `git checkout --
  devalue/` cleanup reverted an intended edit (re-applied).
- proof: every fix was checked by reverting the source change and watching
  the new tests fail. Final: `go test ./...`, `just lint`, `just build-tagged`,
  `go generate ./...` with a clean tree.
