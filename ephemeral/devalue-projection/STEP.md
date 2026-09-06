# Next step

Milestone 2 (#104) is complete on this branch in two commits:

- `ec5245c` — `git mv internal/typegrammar typegrammar`, six import rewrites,
  the non-exhaustive-type-switch paragraph in the package doc. No behavior.
- `8048237` — the exported `grammar` package (`Load`, `Package`, `Root`,
  `Lower`, `Types`), the `types.Type` root bridge as
  `builder.SchemaBuilder.LowerRoots`, on-demand dependency loading via
  `syntax.ScanResult.EnsureRemoteType` + `syntax.LoadFrom`, the seven refusal
  strings hoisted to shared constants, and `loadScanResult`'s panics turned
  into errors. `Closes #104`.

Awaiting the milestone review. Milestone 3 (#105, devalue runtime and codec
backend) has not started.

## Gates run at 8048237

`go test ./...`, `go vet ./...`, `just build-tagged` — all pass.
`grep -rn "internal/typegrammar" --exclude-dir=.git --exclude-dir=ephemeral .`
returns nothing.

## Open items for the reviewer

The brief's #104 proof list asks for three refused roots whose error text
matches the builder's field lowering. Only `map` and an anonymous interface
literal actually behave that way; `chan`, `func`, presence wrappers and
pointer-to-sealed-interface are refused earlier or in a different context by
the builder. The worklog section "Milestone 2: #104 export the grammar"
records what was asserted instead and why.
