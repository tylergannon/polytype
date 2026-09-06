# Next step

Milestones 1 (#106) and 2 (#104) are committed and reviewed. Milestone 3
(#105) is half done: the runtime is ported and committed
(`github.com/tylergannon/polytype/devalue`, nine files from skgo, `uint8`
added to `asFloat`, package doc and three cross-repo comments de-skgo-ed).
The codec backend has not started.

## The step

Build the codec backend: package
`github.com/tylergannon/polytype/devalue/codegen`, per the brief's
"Codec backend" section and the `#105` addenda.

- `Generate(defs typegrammar.Definitions, roots []typegrammar.Type, opts Options) ([]byte, error)`,
  Go API, no CLI.
- Emitter mechanism: `text/template` rendered and formatted with
  `builder.FormatCodeWithGoimports`, mirroring `internal/typescript`'s
  Validate -> allocateNames -> two type switches with a threaded `at` path.
- Wire mapping and the per-node-kind rules are settled in the brief and its
  addenda (`Array` length, `Ref`, `OptionalUnion`, `UnionSlice`, `Enum`
  Mode, pointer nil rules, no dedupe). Do not reopen them.

## How it is demonstrated

The brief's milestone 3 proof: a fixture package under
`devalue/codegen/testdata/` covering every node kind (start from
`union_codec`; add `time.Time`, `[N]T`, Nullable, non-union Optional, plain
slice, plain pointer, bool, floats, sized ints and uints), copied to
`t.TempDir()` with a go.mod whose replace is an absolute path to the repo,
`Generate` into a sibling package, then `go build` and `go test` there via
`testutils.RunCommand`. No committed `test_run` copy.

Tests: encode/decode round-trip equality; nil Required slice encodes `[]`;
absent Optional is not a key; Nullable zero is `null`; the decoder rejects a
missing required property, a wrong kind, and an enum non-member, each error
containing the path.

Then commit with `Closes #105`.

## Not this step

Milestone 4 (#107): deleting `tests/typescript/`, the `tsc` test, the devalue
goldens and the recorder.
