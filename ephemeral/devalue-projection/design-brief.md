# Design brief: issues #106, #104, #105, #107

This brief is the manager's decisions for one branch that closes four GitHub
issues in tylergannon/polytype. The decisions below are settled. Do not reopen
them, do not write alternatives documents, do not ask for a design review of
them. Where the brief is silent, choose the simplest thing that satisfies the
issue's acceptance list and move on.

Read the four issues with `gh issue view <n>` for their acceptance lists:
#106, #104, #105, #107. Read `docs/spec/v1.md` only where a milestone tells
you to amend it.

## Ground rules

- Plain `go test ./...` is the only test entry point. No test code in
  TypeScript or JavaScript. No Node inside `go test`. No ledgers, claims
  files, proof directories, provenance trackers, or run logs.
- If a test takes longer to write than the code it proves, it is the wrong
  test. Replace it with a smaller assertion.
- No new abstractions for one caller. No interfaces with one implementation.
- Commit after each milestone with a message that names the issue
  (`Closes #106` etc). Do not push. Do not open a PR.
- Run `go test ./...` before starting and after each change. Run
  `just build-tagged` and `go vet ./...` before each commit.
- Keep the session worklog at `ephemeral/worklog/202609061434-issues-104-107.md`
  updated with decisions, corrections, and friction only. It is not an
  activity log.
- Everything ephemeral goes under `ephemeral/`. Nothing goes in `docs/`
  except the spec amendment named below.

## Milestone 1: #106 enum encode-side membership

Decision: type-level codec on locally declared `enum()`-marked types, string
and integer, value mode. Generate `MarshalJSON` (value receiver) and
`UnmarshalJSON` (pointer receiver) on the enum type in `jsonschema_gen.go`.
Both reject a non-member with an error that names the type and the value.

- Only enum types declared in the generation target package get methods.
  Foreign enum types are not guarded; say so in one sentence in the enum
  guide (`website/src/content/docs/features/enums.md`).
- `.StringerEnum` owner codecs are unchanged. An integer enum that is used
  in string mode in one field and numeric mode elsewhere still gets the
  type-level numeric codec; the owner codec bypasses it by construction
  (it marshals the mapped string itself). Add one test proving both coexist.
- Generated methods live in a `!jsonschema` file, so the scanner cannot see
  them. Add one test proving a package that has already been generated
  regenerates cleanly (the "custom wire type" rejection does not fire).
- Amend the v1 spec table row for value-mode enums
  (`docs/spec/v1.md`, the row reading "Standard Go JSON" for marked enums)
  to name the generated type-level codec. One row and one sentence in the
  amendment log at the top of the file, nothing more.
- Proof: extend an existing enum fixture (`internal/builder/testfixtures/enums`
  or the `test_run` copy) so a consumer test shows `json.Marshal` of a
  zero-valued struct fails with an error naming the enum type and the
  offending value (`encoding/json` wraps it with the Go type; naming the
  enclosing field would need an owner codec on every struct with an enum
  field, which is forbidden), a member marshals, a validated document
  round-trips byte-identically, and `json.Unmarshal` of a non-member fails.

## Milestone 2: #104 export the grammar

Decision: `git mv internal/typegrammar typegrammar`. Import path becomes
`github.com/tylergannon/polytype/typegrammar`. Rewrite imports. Add one
paragraph to the package doc saying new node kinds may be added in minor
versions, so consumers must not treat type switches as exhaustive.

Decision: the lowering entry point is a new exported package
`github.com/tylergannon/polytype/grammar` (it may import `internal/builder`
and `internal/syntax`; the reverse is impossible). Shape:

```go
// Load loads the Go package at dir with the jsonschema build tag, the same
// way the CLI does, together with the packages it references.
func Load(dir string) (*Package, error)

// Root is one shape a caller wants lowered. Type may be anonymous.
// Position is reported in diagnostics because an anonymous type has none.
type Root struct {
    Type     types.Type
    Position token.Position
}

// Lower returns the validated definition graph reachable from the roots,
// and one grammar node per root in order. Named roots need no Declare
// marker.
func (p *Package) Lower(roots []Root) (typegrammar.Definitions, []typegrammar.Type, error)
```

Rename freely if a better name is obvious; keep the shape.

- The root bridge lowers a `types.Type` structurally: `*types.Basic` to a
  scalar, `*types.Slice`, `*types.Array`, `*types.Pointer` to their nodes,
  `time.Time` to `Time`, and a `*types.Named` hands off by
  `typegrammar.Name{PkgPath, Name}` to the existing `named()` lowering in
  `internal/builder/typegrammar.go`. That lowering is dst-based and stays
  dst-based. Do not rewrite it over `go/types`.
- The bridge matches names only, never `types.Type` identity, so a caller's
  own `packages.Load` result works.
- Refuse maps, channels, functions, interfaces not reached as a direct
  field, anonymous struct literals, and presence wrappers as roots, with the
  exact message strings the existing `typ()` switch uses. Share the strings;
  do not duplicate them.
- Verify, with a test, that a package with no `//go:build jsonschema` file
  lowers correctly. If the scanner needs a change for that, make the
  smallest one.
- Proof: a test that lowers `[]pkg.Todo`, `string`, and `[3]int` against a
  fixture package and checks the result passes `Validate`; a test that a
  bare `*T` root, a `map` root, and an unregistered interface root produce
  the same error text as running the existing builder on a fixture
  declaring those shapes in a field (compare strings from both paths).

Not in scope: #100, #101, transport types, any new admitted shape.

## Milestone 3: #105 devalue runtime and codec backend

### Runtime

Decision: copy `~/src/skgo/internal/devalue/*.go` (source and tests, not
git history) to `github.com/tylergannon/polytype/devalue`. Keep the API
(`Stringify`, `StringifyWith`, `Parse`, `Reducer`, `Object`, `Undefined`,
`Hole`, and the rest). Fix the package doc so it no longer says SvelteKit.
`internal/remotearg` stays in skgo; it is kit policy.

### Codec backend

Decision: package `github.com/tylergannon/polytype/devalue/codegen`. Go API,
no CLI:

```go
type Options struct {
    // PackageName of the emitted file. ImportPath of the package the
    // file will live in, so self-imports are avoided.
    PackageName string
    ImportPath  string
}

// Generate emits one Go source file holding, for each definition and each
// root, an encoder and a strict decoder as free functions over the type's
// exported fields.
func Generate(defs typegrammar.Definitions, roots []typegrammar.Type, opts Options) ([]byte, error)
```

Emitted functions convert between the Go value and the runtime's value
model, and a pair of convenience wrappers call `Stringify`/`Parse`. Naming
is the implementer's choice; make it deterministic and collision-free across
packages (qualify by package when two definitions share a local name).

Wire mapping, decided:

- Every Go numeric kind is a JavaScript number. No BigInt. Document that
  values beyond 2^53 lose precision, in the package doc. This matches the
  TypeScript backend, which emits `number`.
- `time.Time` is the same string `encoding/json` produces. Never a Date.
- Required nil slice encodes as `[]`. Required nil pointer is an encode
  error naming the path. Decoders never produce nil pointers or nil slices
  for Required fields.
- Absent `Optional` is no property. Present `Optional` is the value.
- `Nullable` absent is `null`; present is the value.
- Enum members are checked on both sides; a non-member is an error naming
  the path and the value.
- Unions encode as one object with the discriminator property holding the
  concrete type name, as the JSON codec does. Decoders switch on it and
  reject unknown tags.
- Decoders reject: a missing required property, an unknown property, a
  wrong kind, `undefined` as a value, `null` where not Nullable, and any
  devalue tagged form (Date, Map, Set, BigInt, RegExp) since the grammar
  admits none. Every error names the JSON-pointer-style path.
- Encoders never dedupe references; every value is emitted fresh. Decoders
  accept a slot referenced more than once by copying.
- Reducers and revivers are a caller concern passed to `StringifyWith`/
  `Parse`; the generated code does not choose them.

Not in scope: transport/reduced custom types (no grammar node exists;
skgo can request one later), any SvelteKit knowledge, a CLI flag.

### Proof

- A fixture package under `devalue/codegen/testdata/` (or an internal
  fixture module, whichever the builder tests already do for consumers)
  with one struct covering every grammar node kind, and a sibling package
  that receives the generated codecs so "compiles outside the declaring
  package" is proven by the build.
- Tests: encode then decode returns an equal value; a nil Required slice
  encodes as `[]`; an absent Optional is not a key; a Nullable zero is
  `null`; the decoder rejects a missing required property, a wrong kind,
  and an enum non-member, each error containing the path. Assert on the
  parsed value model or the error string. No hand-written wire strings
  beyond those seven cases.

## Milestone 4: #107 replace tests/typescript, record devalue goldens

- Delete `tests/typescript/` and `.github/workflows/typescript.yml`.
- Add one test in `internal/typescript` that writes the edge-case output
  (the cases now in `tests/typescript/projection/generate/main.go`; keep
  them as a Go fixture) to a temp directory and runs `tsc` when found, and
  `t.Skip`s otherwise. Look for the compiler at `$POLYTYPE_TSC` and at
  `node_modules/.bin/tsc` under the repo root. Add an `npm ci` step to the
  existing Go CI workflow so it does not skip there; a `package.json`
  pinning `typescript` at the repo root or under `internal/typescript/` is
  fine. That is the only npm artifact in the repo besides the recorder.
- Any CLI behavior the old `check.mjs` asserted that `polytype/main_test.go`
  does not already assert moves there. Read both before deciding; most are
  duplicates.
- Devalue goldens: a Go program under `devalue/testdata/record/` writes a
  `.mjs` file of named JavaScript value expressions generated by
  depth-limited nesting of the admitted kinds: booleans, strings (including
  keys needing UTF-16 ordering and escaping), numbers, `null`, arrays
  including empty and nested, plain objects with explicit property order,
  and discriminated-union-shaped objects. A fixed Node script of a few
  dozen lines maps each through the pinned `devalue` npm package and writes
  `devalue/testdata/golden.json`. Commit the golden. Regeneration is
  `go run ./devalue/testdata/record` followed by `node`, documented in a
  three-line README there. `go test` never runs Node.
- Tests over the golden: `Parse` accepts every entry;
  `Stringify(Parse(bytes))` reproduces the bytes exactly; a `FuzzRoundTrip`
  seeded from the entries holds the same property and requires that a
  rejected document is rejected without a panic. Existing skgo tests come
  along unchanged.
- A few hundred cases, generated, not curated. Run the recorder once with
  Node available on this machine (`node` v24 is installed).

## Done

All four issues' acceptance lists hold, `go test ./...`, `go vet ./...`,
and `just build-tagged` pass, each milestone is committed, and the worklog
records the decisions and any friction. Then stop.

## Addenda from research (manager)

- #104: `Scan.deps` is populated only by marker-seeded traversal
  (internal/syntax/scan_result.go). A root whose named type lives in a
  package other than the loaded directory must trigger the same dependency
  load the marker path uses, from inside `Lower`, before `named()` runs.
  Do this on demand per package; do not pre-load the whole module.
  `SchemaBuilder.loadScanResult` panics on an unloaded package
  (internal/builder/gen_schema.go:807); convert that to an error on the
  `Lower` path rather than letting a caller's process die.
- #104: the fixture helpers `loadTypeGrammarFixture`/`writeTypeGrammarFixture`
  in internal/builder/typegrammar_adapter_test.go are unexported. Copy the
  few lines the `grammar` tests need; do not create a shared test package.
- #107: one root `package.json` (`private: true`) with devDependencies
  `typescript` 6.0.3 and `devalue` 5.9.2, plus its lockfile; add
  `node_modules/` to .gitignore. The tsc check looks for
  `node_modules/.bin/tsc` at the repo root (walk up from the test's
  directory to the go.mod) or `$POLYTYPE_TSC`. The recorder script imports
  `devalue` from that same install. Do not create a second package.json.
- #107: the golden file is read with `os.ReadFile`; `testutils.AssertGoldenFile`
  is for generated sibling files and does not apply.
- #107: see research-107.md for exactly which check.mjs cases move to
  polytype/main_test.go and which edge cases the tsc fixture must add;
  everything it lists as already covered is not re-tested.
- #105 rules the brief left unstated, now decided:
  - `Array`: encodes as an array of exactly Length elements; the decoder
    rejects any other length with an error naming the path.
  - `Ref`: the emitter calls the referenced definition's function pair.
  - `OptionalUnion`: Optional semantics around the union rule.
    `UnionSlice`: Slice semantics around the union rule; nil encodes `[]`.
  - `Enum` Mode: honor it as the TypeScript backend does. `EnumNames` puts
    the constant name on the wire; `EnumValues` puts the value. Membership
    is checked against whichever set applies.
  - Pointers: a nil pointer anywhere a value is required is an encode error
    naming the path (Required, present Optional, present Nullable). Only an
    absent Nullable produces `null`.
  - Recursion cannot occur; `Validate` rejects it before the emitter runs.
- #105 runtime: the skgo `asFloat` lacks `uint8`; add it in the copy. The
  emitter still converts every numeric to `float64` itself before handing
  values to the runtime, so generated code never depends on `asFloat`'s
  accepted set.
- #105 dedupe: "never dedupe" means the emitter builds a fresh `*Object` and
  `[]any` per value. Primitives sharing a slot is the format's normal
  behavior and is fine.
- #105 emitter mechanism: `text/template` rendered and formatted with
  `builder.FormatCodeWithGoimports`, mirroring internal/typescript's
  Validate → allocateNames → two type switches with a threaded `at` path.
- #105 fixture: a new fixture package under `devalue/codegen/testdata/`
  covering every node kind (start from union_codec; add time.Time, `[N]T`,
  Nullable, non-union Optional, plain slice, plain pointer, bool, floats,
  sized ints and uints). The test copies it to `t.TempDir()`, writes a
  go.mod whose replace uses an absolute path to the repo, runs Generate
  into a sibling package, then `go build` and `go test` there with
  `testutils.RunCommand`. No committed `test_run` copy.
