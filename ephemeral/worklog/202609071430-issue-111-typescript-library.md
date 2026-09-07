# Issue #111: export the TypeScript backend as a library

Branch `claude/issue-111-plan-46f7f6`, worktree `.claude/worktrees/issue-111-plan-46f7f6`.

decision: The worktree branch had been cut before `grammar`, `typegrammar` and `devalue/codegen` landed and carried two docs commits that also live on `origin/docs/reenvision-projection`. Reset the branch to `origin/main` (backup tag `backup/reenvision-docs-11f6f2f`) rather than rebase docs work into a feature PR; the user will rebase the docs branch afterwards. Source: user, this session.

decision: `typescript.Generate` returns a `Result{Files, Names}` struct, not the `([]File, error)` sketched in the issue, so `Names` and any later field ride one signature. Source: plan agreed with user.

decision: The acceptance fixture is its own module at `typescript/testdata/fixture` with no tagged file, mirroring `devalue/codegen/testdata/fixture`, rather than reusing `polytype/testdata/consumer`. The consumer fixture uses `.StringerEnum`, a Declare-level option, so a marker-free copy of it could not be byte-identical to the CLI's output; the point of the test is that the library copy holds no polytype files at all.

discovery: `.StringerEnum` and `SealedUnion` still apply on the library path when the target package happens to carry a tagged file, because `grammar.Load` loads with the `jsonschema` tag. They are not required. Documented in llms.txt, the skill reference, and the website guide.

discovery: Byte-identity between `SchemaBuilder.TypeDefinitions()` (CLI) and `grammar.Package.Lower` (library) held on the first run for two roots in Declare order; definition order is DFS from the roots in both. Proven by `TestLibraryPathMatchesCLI`, which builds the CLI binary and diffs `types.ts` and `index.ts`.

friction: `just lint` fails at `staticcheck` on pre-existing ST1005 findings in `devalue/parse.go` and `devalue/stringify.go` (capitalised error strings mirroring devalue's JS messages), unrelated to this change. -> Either suppress ST1005 for `devalue/` in staticcheck config or lower-case the strings; until then the remaining lint steps have to be run by hand after staticcheck.

friction: My own fixture doc comment contained the literal text `//go:build jsonschema`, which the "no tagged file" assertion caught. -> The assertion now checks the file prefix, which is what Go's build constraint rule requires anyway.

doc_bug: llms.txt §"Driving polytype from another generator" said there is no library entry point and none is needed, contradicting the `grammar`/`devalue/codegen` section directly above it. -> Rewritten as two paths (CLI for a package that owns its schema; library for a generator that knows its roots), with the TypeScript backend on the library side.

Proof: `go test ./...` green before and after; `golangci-lint run ./...` 0 issues; `goimports -l` and `gofmt -l` empty; `just build-tagged` clean; `govulncheck` reports nothing reachable. `website/package.json` prebuild now feeds `../typescript` to gomarkdoc so the API reference page gains the package on the next site build (not run locally).

Not done (separable per the issue): `grammar.LoadPattern` for a root declared in a dependency.
