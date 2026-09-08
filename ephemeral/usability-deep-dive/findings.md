# Three usability findings at 964f778

Ranked by impact on adopting polytype as a library. This is a review, not an implementation. Source links point into the supplied task worktree. The five characterization probes in `review_test.go` record existing behavior, including defects; a passing probe is not a product correctness claim. Full observed output is in `evidence.txt`.

## 1. The main product still has no composable generator API

The user's concern is correct for JSON Schema, schema accessors, validation, and Go JSON codecs. The public `grammar.Load/Lower`, `typescript.Generate`, and `devalue/codegen.Generate` already support marker-free roots; the first probe generates TypeScript and devalue source without writing into the model package. The missing capability is a public, configurable generation entry point covering the core outputs.

`Declaration[T]` is an empty struct; `Declare` discards its function and every chain method discards its options. The CLI's `builder.Run` is internal, takes a target directory rather than explicit roots, discovers registrations, and writes outputs directly. The public `jsonschema` package constructs schemas manually; it is not the automatic projection backend. Root selection and custom settings such as `.StringerEnum` and `SealedUnion` still depend on authored syntax for the CLI path, and the latter settings have no equivalent options on `grammar.Load/Lower`.

Evidence: [markers](/Users/tyler/.codex/worktrees/2bd1/polytype/declare.go:10), [internal runner](/Users/tyler/.codex/worktrees/2bd1/polytype/internal/builder/builder.go:47), [public loader](/Users/tyler/.codex/worktrees/2bd1/polytype/grammar/grammar.go:42), [TypeScript generator](/Users/tyler/.codex/worktrees/2bd1/polytype/typescript/generate.go:65), [devalue generator](/Users/tyler/.codex/worktrees/2bd1/polytype/devalue/codegen/generate.go:38).

Recommendation: make source package, selected roots, and projection options an ordinary configuration value, and return generated files before writing them. Keep marker scanning as another way to construct that configuration. Support marker-free selection without requiring a pre-existing `T.Schema` method.

The exact current fluent syntax cannot simply become a runtime builder. `Order{}.Priority` passes a field's zero value; two same-typed fields are indistinguishable after evaluation. The scanner currently recovers field identity from the selector AST. Programmatic configuration therefore needs explicit field names or source symbols, and source loading must remain responsible for comments, enum constants, and sealing methods. Returning useful configuration from compatible marker calls is reasonable, but it is an adapter to a real API rather than the API's foundation.

Evidence: [field identity extraction](/Users/tyler/.codex/worktrees/2bd1/polytype/internal/syntax/fluent_expr.go:150).

## 2. “One grammar” overstates the construction and the wire guarantees

The README and custom-backend guide say every backend consumes the same closed grammar. The default JSON Schema path instead uses `SchemaBuilder`'s separate `ObjectNode`/`ArrayNode` model and traversal. `TypeDefinitions` is called by the CLI when TypeScript is requested. TypeScript and devalue consume `typegrammar`; JSON Schema does not generally pass through its admission boundary.

Observed consequences:

- `Order{Data: []uint8{1, 2}}` marshals as `{"data":"AQI="}`. Generation with `--validate` succeeds, but its generated `ValidateJSON` rejects that output: “got string, want array.” Adding `--typescript` fails generation because the grammar correctly identifies a byte-like slice's base64 wire mapping as unsupported. The first attempted spelling, `[]byte`, failed even earlier with “mapNamedType: type byte not found”; use `[]uint8` to reproduce the accepted-but-wrong schema.
- For `Values [2]int`, generated `ValidateJSON` accepts `{"values":[1,2,3]}`. The documented validate-then-unmarshal workflow succeeds and retains only `[1 2]`, silently discarding the third value. The schema model lacks fixed-length constraints even though `typegrammar.Array` retains the length.

Evidence: [overstated README claim](/Users/tyler/.codex/worktrees/2bd1/polytype/README.md:661), [conditional grammar use](/Users/tyler/.codex/worktrees/2bd1/polytype/internal/builder/builder.go:105), [schema array model](/Users/tyler/.codex/worktrees/2bd1/polytype/internal/builder/model.go:78), [consumer observations](/Users/tyler/.codex/worktrees/2bd1/polytype/ephemeral/usability-deep-dive/evidence.txt:4).

Recommendation: make the portable projections share admission and structural semantics. Initially refuse incompatible byte-like shapes consistently and emit exact fixed-array bounds in JSON Schema. Keep runtime schema providers as an explicit capability outside the portable grammar, which currently cannot represent them. State those capability differences in the introductory documentation rather than promising one universal path. Add behavioral checks that marshal supported Go values, validate the bytes, decode, and compare values; golden schema text alone does not establish agreement.

## 3. Generation mixes rendering, checking, and writing

`--no-changes` compares newly rendered bytes with the saved `.sum`, not with the actual schema file. When the sum matches it still renames the newly rendered file over the destination. In the probe, altering `Order.json` to `{}` while retaining its sum leads to exit 0 and a rewritten schema. Deleting the JSON but retaining its sum leads to exit 0 and a recreated file. A drift check has silently repaired the drift it was meant to detect.

Output application also spans several independent writes. A fixture with a directory occupying `jsonschema_gen.go` makes generation fail after it has already created `Order.json` and `Order.json.sum`. This contradicts the agent-facing statement that a nonzero exit means nothing was written. This is a deterministic output-path collision, not a simulated disk failure. The test does not establish every possible failure ordering.

Evidence: [sum comparison and rename](/Users/tyler/.codex/worktrees/2bd1/polytype/internal/builder/gen_schema.go:1377), [schema-then-Go ordering](/Users/tyler/.codex/worktrees/2bd1/polytype/internal/builder/builder.go:121), [documentation promise](/Users/tyler/.codex/worktrees/2bd1/polytype/llms.txt:802), [observations](/Users/tyler/.codex/worktrees/2bd1/polytype/ephemeral/usability-deep-dive/evidence.txt:25).

Recommendation: render every requested artifact into an in-memory result, preflight destinations, compare with actual file bytes, then either report differences without writes or explicitly apply them. Do not promise all-or-nothing behavior for arbitrary I/O failures unless rollback is implemented. The existing TypeScript `Result.Files` and output-plan code offer a useful starting point. This separation also makes finding 1's library API straightforward to consume.

## Verification

- Repository `go test ./...` passed before exploration.
- Final repository `go test ./...` passed after the review.
- `just build-tagged` passed.
- Built the CLI with `go build -o ephemeral/usability-deep-dive/bin/polytype ./polytype`.
- All five characterization probes passed with `TMPDIR="$PWD/tmp" go test -mod=mod -v ./...` from this directory. Their two consumer modules executed generated validation and standard JSON encoding/decoding. The marker-free probe verifies generation, not execution of the returned devalue source.
- No production implementation changes. Review module and worklog are the only tracked additions.
