# Issue 132 (rewritten): test projection separately from parsing in internal/builder

Branch: claude/issue-132-projection-tests, from origin/main at 9c6ca60 (#134 merged).

## Context

decision: Issue 132 was rewritten (2026-09-16T00:48Z) before #134 was implemented; #134 delivered the original framing, which the rewrite lists as a non-goal. Per the user, #132 is reopened and #134 treated as unrelated to it. This session does the rewritten scope only: a pure `renderGoCode(schemaTemplateData)` and moving projection assertions off the load path.

Baseline on main (before this work): `go test ./internal/builder -count=1` 32.9 s wall; ~0.3 s per package load in sequential tests; `t.Parallel()` tests report inflated times from contention, not slow loads. Load count is the whole cost.

## Decisions

decision: `schemaTemplateData` no longer embeds `SchemaBuilder`. It is a plain value (package name, build tag, subdir, flags, imports, `SchemaAccessor` entrypoints, rendered map, providers, owner codecs, interfaces, enum markers). `SchemaBuilder.templateData()` resolves it; `renderGoCode(data) ([]byte, error)` renders and formats it; `RenderGoCode()` only names and writes the file. Template edits are mechanical renames (`.Scan.Pkg.Name` -> `.PackageName`, `.Receiver.TypeName` -> `.TypeName`, `.SchemaMethodName` -> `.MethodName`, `.IsPointer` -> `.Pointer`); `GeneratesJSONUnmarshalers` stays a constant-true method so rendered bytes are unchanged.
decision: `HasNonRenderedTypes` and `GeneratesJSONUnmarshalers` move off `SchemaBuilder` (template-only).
decision: JSON Schema projection tests with zero loads already exist in `internal/schema` (from #134); nothing further is added there. The builder's remaining rendered-JSON assertions are converted to IR assertions instead.

## Proof so far

- `go test ./internal/builder -run TestBasic$ -count=1` green after the split: every golden byte-identical.
- `render_go_code_test.go`: 9 zero-load tests over hand-built `schemaTemplateData`, ~10 ms each.

## Test conversions (assert on lowering/plans, not rendered text)

- Deleted gen_schema_determinism_test.go (2 loads): TestRenderGoCodeIsDeterministic covers it without a load; TestBasic's idempotent union_codec run covers the builder side.
- enum_marker_test.go: two positive tests merged into TestEnumMarkerIsAPropertyOfTheType (one load) asserting on the lowered enums and `enumMarkers()`; the rendered assertion/codec text lives in TestRenderGoCodeEnumMarkers.
- enum_type_codec_test.go: TestEnumMarkerEmitsTypeLevelCodecs removed (render test + enumMarkers assertion).
- fluent_declaration_test.go: three parity tests (2 loads each) and the enum-marker test consolidated onto one fixture (1 load) comparing lowered field forms, provider tables, Rendered and RefTypes. decision: the parity oracle is now the IR and provider table rather than projected JSON; flagged to the user as the one judgment call.
- named_container_test.go: first test asserts on ownerCodecs; the nested test keeps the runtime round trip (no fixture in testfixtures covers a named container reaching a union).
- sealed_union_test.go: inferred + membership drift merged (3 loads), asserting on the lowered union; `unionDiscriminators` (read generated JSON) removed.
- sealed_union_discriminator_test.go: the two positive tests assert on the lowered unions and `DiscPropName` in the plan.
- validate_free_func_test.go: two free-function tests merged onto one fixture asserting `SchemaFreeFuncs()`/`SchemaMethods()` classification.
- unsupported_interface_containers_test.go: five byte-identical duplicate cases removed (11 -> 6 loads).
- single_lowering_test.go: `marshalSchema` JSON assertions replaced with IR assertions.
- New lowering_helpers_test.go: loadBuilder, loweredObject, loweredUnion, variantTags, loweredEnum.

## Proof

- `go test ./... -count=1` green; `just lint` 0 issues; `just build-tagged` green.
- `internal/builder` wall time 32.9 s -> 26.2 s. finding: the remaining loads are diagnostics tests, one load per bad package; they cannot be consolidated without aggregating errors, so the package does not reach the issue's hoped-for sub-30 s floor by much.

## Final state

Committed on claude/issue-132-projection-tests, PR opened against main with "Closes #132"; merged per user instruction ("completely shut this down and merge"). Review subagents skipped at the user's request to close out.
