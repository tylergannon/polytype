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
