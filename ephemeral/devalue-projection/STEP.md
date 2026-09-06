# Next step

Milestone 2 (#104) is complete and reviewed on this branch:

- `ec5245c` — `git mv internal/typegrammar typegrammar`, six import rewrites,
  the non-exhaustive-type-switch paragraph in the package doc. No behavior.
- `8048237` — the exported `grammar` package (`Load`, `Package`, `Root`,
  `Lower`, `Types`), the `types.Type` root bridge as
  `builder.SchemaBuilder.LowerRoots`, on-demand dependency loading via
  `syntax.ScanResult.EnsureRemoteType` + `syntax.LoadFrom`, the seven refusal
  strings hoisted to shared constants, and `loadScanResult`'s panics turned
  into errors. `Closes #104`.
- one follow-up commit — root nodes now pass the same admission boundary as
  the definitions (`typegrammar.Definitions.ValidateWithRoots`).

Review outcome: finding 1 accepted and fixed; findings 2 and 3 rejected by the
manager (see the worklog's "Milestone 2 review resolution").

Milestone 3 (#105, devalue runtime and codec backend) has not started.

## Gates at the follow-up commit

`go test ./...`, `go vet ./...`, `just build-tagged` — all pass.
