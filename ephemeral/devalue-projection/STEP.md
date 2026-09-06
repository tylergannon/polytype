# Next step

Milestones 1 (#106), 2 (#104) and 3 (#105) are committed; the codec backend's
open review finding was closed by 5ecd5f2. Milestone 4 (#107) is untouched.

## The step

All of #107 in one step — manager ruling: not sliced. Read
`ephemeral/devalue-projection/research-107.md` first; it names exactly which
`check.mjs` cases move to `polytype/main_test.go` and which tsc edge cases the
new Go fixture must add. Follow the brief's "Milestone 4" section and its
`#107` addenda.

Deliverables:

- Delete `tests/typescript/` and `.github/workflows/typescript.yml`.
- One root `package.json` (`private: true`) pinning `typescript` 6.0.3 and
  `devalue` 5.9.2, with its lockfile committed and `node_modules/` added to
  `.gitignore`. No second package.json anywhere.
- A test in `internal/typescript` that writes the edge-case output as a Go
  fixture to a temp dir and runs `tsc` when found — `$POLYTYPE_TSC`, else
  `node_modules/.bin/tsc` at the repo root (walk up to the go.mod) — and
  `t.Skip`s otherwise. The fixture adds only the four cases research-107 lists
  as uncovered: the escaped string as a union discriminator and in `Omit<>`;
  colliding `雪` and literal `_u96EA_`; `*/` plus U+2028 in a definition
  description; a word-boundary `\b(any|unknown)\b` check. Everything it lists
  as already covered is not re-tested.
- The three uncovered `check.mjs` claims as CLI subprocess tests in
  `polytype/main_test.go`: relative `--typescript` dir with
  `--typescript-barrel` resolved against CWD not `--target`; `--no-changes`
  failing on a stale types.ts without mutating it and on a missing requested
  barrel; `jsonschema/Envelope.json` enum membership equal to the TS literal
  union. Drop the four TypeScript-semantics claims.
- `.github/workflows/go.yml`: insert `actions/setup-node@v4` (node 24) and
  `npm ci --ignore-scripts --no-audit --no-fund` after `setup-go` in
  test-and-generate.
- The recorder under `devalue/testdata/record/`: a Go program writing a `.mjs`
  of named JS value expressions from depth-limited nesting of the admitted
  kinds (booleans; strings including keys needing UTF-16 ordering and
  escaping; numbers; `null`; arrays incl. empty and nested; plain objects with
  explicit property order; discriminated-union-shaped objects), plus a fixed
  Node script of a few dozen lines importing the pinned `devalue` that writes
  `devalue/testdata/golden.json`. A few hundred cases, generated not curated.
  Run `go run ./devalue/testdata/record` then `node` once now — node v24 is
  installed — and commit `golden.json`. Three-line README documenting that
  regeneration. `go test` never runs Node.
- Golden tests reading `testdata/golden.json` with `os.ReadFile` (not
  `testutils.AssertGoldenFile`): `Parse` accepts every entry;
  `Stringify(Parse(bytes))` reproduces the bytes exactly; a `FuzzRoundTrip`
  seeded from the entries holding the same property and requiring a rejected
  document to be rejected without panicking. Existing skgo tests unchanged.

Plain `go test` only: no ledgers, no JS or TS test code beyond the pinned
recorder script and the tsc fixture the Go test compiles, no machinery for one
caller.

## How it is demonstrated

`go test ./...` (the new `internal/typescript` tsc test actually runs, not
skips, once `npm ci` has populated `node_modules/.bin/tsc` locally), plus the
new `polytype` CLI subprocess tests and the devalue golden and fuzz tests.

## Done when

`go test ./...`, `go vet ./...` and `just build-tagged` pass, `tests/typescript`
and the typescript workflow are gone, the worklog records the decisions and any
friction, and the work is committed on this branch (not pushed) with
`Closes #107`.

## Not this step

Nothing after — this is the final milestone. Do not push.
