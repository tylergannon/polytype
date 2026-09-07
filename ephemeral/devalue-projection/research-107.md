# Research: #107 (facts for the implementer)

## check.mjs claims vs existing Go coverage
Covered already: user-owned output preserved with failing diagnostic
(internal/builder/typescript_output_test.go:57, :158); deterministic
re-prepare (:26); barrel create (:26) and remove-only-if-owned (:98);
`--typescript-barrel` without `--typescript` (:19).

Not covered, move to polytype/main_test.go as CLI subprocess tests:
- relative `--typescript` dir + `--typescript-barrel` resolved against CWD,
  not `--target`, producing exactly index.ts and types.ts;
- `--no-changes` fails on a stale types.ts without mutating it, and on a
  missing barrel when `--typescript-barrel` is requested;
- jsonschema/Envelope.json enum membership equals the TS literal union
  (`["ready","wait\"ing","converted"]`, `[0,1,8,4]`, names list).
Drop (they are TypeScript-semantics assertions, not polytype behavior):
consumer.ts type-level obligations, missing-case TS2345, added-variant
break, "Node absent from PATH".

## Edge-case program (tests/typescript/projection/generate/main.go)
Already covered in internal/typescript tests: 2^53 literal and 2^53+1
rejection (generate_test.go:65-71, :163); quote/backslash/newline/CJK in a
string enum member (:51); `*/` in a comment (:35); U+2028 (printer_test.go:57);
quoted property name with escapes (printer_test.go:60); Unicode name
collision `_u96EA_` single case (generate_test.go:141); empty module and
barrel (:188); crude no-`any`/`unknown` check (:121).
Not covered; put into the new Go fixture the tsc check compiles:
- the escaped string used as a union *discriminator* property and in `Omit<>`;
- two colliding names (`雪` and literal `_u96EA_`) forcing suffixing;
- `*/` plus U+2028 in a *definition* description reaching Generate;
- word-boundary `\b(any|unknown)\b` check.

## CI
.github/workflows/go.yml job test-and-generate: checkout, setup-go, go test,
build tagged examples, go generate, porcelain check, JSONSCHEMA_NO_CHANGES
generate, go test. Insert actions/setup-node@v4 (node 24) + `npm ci
--ignore-scripts --no-audit --no-fund` after setup-go. Delete
.github/workflows/typescript.yml. No root package.json exists today;
tests/typescript/package.json pins typescript 6.0.3.

## Golden convention
internal/testutils.AssertGoldenFile diffs a generated file against a
sibling `.golden`; it does not fit a committed JSON data file. The devalue
golden test reads testdata/golden.json with os.ReadFile and asserts per
record. No testutils involvement.

## devalue version
skgo has no npm dependency on devalue. Its goldens were produced against a
pinned source clone at ~/src/skgo/ephemeral/inspiration/reference/devalue,
package.json version "5.9.2" (kit pins ^5.9.0). Pin `"devalue": "5.9.2"`
from the registry; same version, so bytes match.
