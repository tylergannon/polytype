# Research: #104 — move `internal/typegrammar` + `grammar` bridge

Module: `github.com/tylergannon/polytype`.

## 1. Importers (6 lines, mechanical rewrite)

- `internal/builder/typegrammar.go:14`
- `internal/typescript/generate.go:15`
- `tests/typescript/projection/generate/main.go:13` (root module; no separate go.mod)
- `internal/typegrammar/grammar_test.go:11` (aliased `g`, external test pkg)
- `internal/builder/typegrammar_adapter_test.go:13`
- `internal/typescript/generate_test.go:10`

No test or fixture references the path as a string; other hits are `ephemeral/**` prose/logs only.

## 2. Directory → `SchemaBuilder`

`builder.go:57` `syntax.Load(dir)` → `:61` non-empty check → `:64` `New(pkgs[0])`. `New` (`gen_schema.go:37`) → `NewForTypes` (`:43`), whose first act is `syntax.LoadPackage(pkg)` (`:44`) building `Scan`; the rest collects marker options and calls `mapType` per root. `syntax.Load` (`internal/syntax/loader.go:30`) uses `-tags=jsonschema` (`:24-28`).

Markers are **not** required. `loadPackageInternal` (`internal/syntax/scan_result.go:381`) populates `LocalNamedTypes`/`Constants`/`Interfaces` from every type decl (`:451-487`) independent of markers; with no `MarkerCalls` the mapping loops (`gen_schema.go:145-160`) are no-ops and `New` succeeds. No `//go:build jsonschema` file needed — the tag only adds files.

What does fail: remote packages enter `Scan.deps` only via marker-seeded `typesToMap` → `requestType` → `resolveTypes` (`scan_result.go:493-497`, `:659-677`). A cross-package root therefore hits `internal/builder/typegrammar.go:59-62` ("unresolved package %q for named type %s") or `:134-136`. The bridge must seed discovery or preload deps.

## 3. State `named()` touches

`Scan` only: `GetPackage` (`typegrammar.go:59,134,452`), `scan.Pkg.Errors`/`PkgPath` (`:695-699`), `Constants` (`:69,456`), `LocalNamedTypes` (`:73,458`), `Interfaces` (`:75`), `Pkg.Types.Scope()` (`:557,592`), `Pkg.Decorator.Ast.Nodes`+`TypesInfo` (`:578-583`).
Builder struct: `TypeProvidersMap` (`:325`), `EnumV1` (`:438`), `DiscriminatorProp` (`:412`); methods `resolveEmbeddedType` (`:247`; `gen_schema.go:1608`), `resolveRegisteredInterfaceField` (`:341,378`; `gen_schema.go:1895`), `find` (`:426`; `gen_schema.go:814` — its `loadScanResult` **panics** on an unloaded pkg, `:807`). Roots come from `SchemaMethods()`/`SchemaFreeFuncs()` (`gen_schema.go:711,748`).
Minimum: a `Scan` with all needed packages in `deps`, non-nil `TypeProvidersMap`/`EnumV1`, `DiscriminatorProp`.

## 4. Error strings in `typ()` (`internal/builder/typegrammar.go`)

- `:191` `maps are outside the static type grammar at %s`
- `:193` `channels are outside the static type grammar at %s`
- `:195` `functions are outside the static type grammar at %s`
- `:197` `interfaces are valid only as configured direct fields at %s`
- `:199` (`IndexExpr`/`IndexListExpr`) `presence wrappers are valid only as complete direct named fields at %s`
- `:135` `unresolved external type %s; no static wire shape was loaded`
- `:201` default `unsupported type expression %T at %s`

Hoist to package-level consts/`func(pos) error` in the moved package so both lowerers emit identical text.

## 5. Test helper to reuse

`loadTypeGrammarFixture(t, source) SchemaBuilder` — `internal/builder/typegrammar_adapter_test.go:400`; writes a temp module via `writeTypeGrammarFixture` (`:412`: `t.TempDir()`, go.mod with `replace ... => <repo root>`, `fixture.go`), then `syntax.Load` → assert `packages[0].Errors` empty → `New`. Siblings: `writeMultiFileFixture` (`validate_free_func_test.go:16`), `writeEnumCodecFixture` (`enum_codec_test.go:141`), `writeFluentFixture` (`fluent_declaration_test.go:16`), and `internal/syntax/loader_test.go:15`. Both helpers are unexported in `package builder`, so `grammar`'s tests need a copy or an exported equivalent.
