# Adversarial review — issue 154, round 01

Outcome: **material findings remain**

## Review target

Polytype commit `e9ce2f3` ("feat: add devalue uneval expressions") on top of plan
commit `0b88160`, as packaged in `IMPLEMENTATION.patch`, reviewed against
`CONTEXT.md`, `PLAN.md`, GitHub issue tylergannon/polytype#154 (read live with
`gh issue view`), and the repository `CLAUDE.md`.

## Operating notes

- The caller named `REVIEW.md` in the packet directory and forbade editing any
  other file. The skill's default location is `ephemeral/reviews/`; the caller's
  path is still under `ephemeral/`, so it was honored. No earlier round existed
  at this path. For the same reason no session-worklog entry was written.
- `CONTEXT.md` says findings are useful only when grounded in a contract, a
  concrete failing input, a consumer break, or an upstream contradiction. I
  treated that as adjudication guidance, not as a limit on what was inspected.
  Every finding below carries a reproduction or a cited instruction.
- Review was read-only. Probe programs lived in the session scratchpad and
  imported the worktree through a `replace` directive. `git status` is unchanged
  apart from this packet directory.

## Evidence inspected

- Whole packet: `CONTEXT.md`, `PLAN.md`, `IMPLEMENTATION.patch` (18 file diffs;
  `git apply --check -R` confirms it matches `HEAD`), `tractor-logs/` (contains
  only this review session's own event log).
- Files omitted from the packet, read from the worktree: `devalue/testdata/golden.json`,
  `devalue/testdata/record/values.mjs`.
- Full source at `HEAD`: `devalue/uneval.go`, `uneval_helpers.go`,
  `uneval_value.go`, `typedarray.go`, `internal.go`, `value.go` (diff),
  `parse.go` (tag handling), `uneval_test.go`, `eval_test.go`, `golden_test.go`,
  recorder changes, and all documentation diffs.
- Pinned upstream: `node_modules/devalue@5.9.2` `src/uneval.js`, `src/utils.js`.
- Harvest source: skgo `internal/devalue` at `528c14cb…` fetched with `gh api`
  and diffed. Production code is a near-verbatim move (`quoteJS` → `quoteString`,
  `range n` modernization, one `nolint`); exported names and signatures are
  unchanged, so the "import rewrite, not an adapter" criterion holds.

Commands run and results:

| Check | Result |
| --- | --- |
| `go test -count=1 ./...` (baseline, per CLAUDE.md) | pass |
| `go test -count=1 ./devalue ./devalue/codegen` | pass |
| `go vet ./devalue/...`, `staticcheck ./devalue/...`, `golangci-lint run ./devalue/...` | clean |
| `just build-tagged` | pass |
| `just lint` | **not run** — the recipe rewrites files (`go mod tidy`, `modernize -fix`, `goimports -w`), which a read-only review may not do |
| Old vs new `golden.json` | 321 → 324 cases, 0 removed, 0 flat `devalue` strings changed, every case has `uneval` |
| Golden reproducibility from the pin (recomputed in memory with devalue 5.9.2) | 324/324 identical |
| Differential: 3,000 random graphs (objects, null-proto, sparse/holey arrays, Map, Set, Date, BigInt, boxed, ArrayBuffer, −0/NaN/±Infinity/undefined, shared refs, cycles; 827 hoisted, 48 with ≥3 params) — upstream `stringify`+`uneval` recorded, Go `Parse` → `Uneval` compared | 2,990 byte-identical; all 10 mismatches are the documented empty-array non-sharing (`Array(0)` hoisted upstream) |
| Typed-array / DataView / subarray / BigInt64Array hand fixtures recomputed with pinned upstream | 8/8 identical |

Not verifiable at this point: the plan's release and fresh-consumer acceptance
steps, which happen after merge.

The port is faithful where upstream's invariants hold. The material findings are
both places where a JavaScript engine enforces an invariant that the Go value
model does not, and the port inherited upstream's reliance on it.

## Findings

### 1. critical — `RegExp.Flags` and `BigInt` are spliced into the expression unescaped; `Parse` → `Uneval` executes attacker JavaScript

Type: verifiable bug; contradiction with pinned upstream; violates the
script-safe-escaping requirement (issue acceptance, PLAN "script-safe escaping").

Evidence:

- `devalue/uneval.go:411` — `"new RegExp(" + quoteString(t.Source) + `,"` + t.Flags + `")``
- `devalue/uneval.go:743-744` — `case BigInt: return string(t) + "n"`
- `devalue/parse.go:271-290` — `Parse` stores `re.Flags` and `BigInt(digits)` from
  the wire with no validation.
- `devalue/uneval.go:48-50` documents the contract being broken: "nothing a value
  carries can close the `<script>` element the document puts it in."

Reproduction (scratch module, worktree via `replace`):

```go
v, _ := devalue.Parse(`[["RegExp","a","</script><script>alert(1)</script>"]]`, nil)
devalue.Uneval(v) // new RegExp("a","</script><script>alert(1)</script>")

v, _ = devalue.Parse(`[["RegExp","a","\"),(globalThis.pwned=1),(\""]]`, nil)
devalue.Uneval(v) // new RegExp("a",""),(globalThis.pwned=1),("")   → goja: pwned === 1

v, _ = devalue.Parse(`[["BigInt","(globalThis.pwned=2),1"]]`, nil)
devalue.Uneval(v) // (globalThis.pwned=2),1n                        → goja: pwned === 2

v, _ = devalue.Parse(`[["BigInt","1;</script><script>alert(1)</script>"]]`, nil)
devalue.Uneval(v) // 1;</script><script>alert(1)</script>n
```

Pinned upstream cannot produce any of these: `parse` of the same documents throws
`SyntaxError: Invalid flags supplied to RegExp constructor` and
`SyntaxError: Cannot convert … to a BigInt`, because the engine validates both
before `uneval` ever sees them. In Go both are free-form strings, so the port's
verbatim copy of upstream's unquoted emission loses the guarantee.

Impact: the output of `Uneval` is, by its own documentation, embedded in an SSR
`<script>`. Any flow where flags or digits are attacker-influenced — the
package's own `Parse` on a client payload followed by `Uneval` (exactly the
pipeline `TestUnevalGolden` blesses), or application code doing
`devalue.BigInt(userInput)` — is stored/reflected XSS. Precondition stated
plainly: it needs such a flow; a server that only builds these values from
trusted data is unaffected. The defect is inherited from skgo, but this commit is
what publishes it as a public, documented-safe API.

Smallest fix: in Uneval, reject (as a `*DevalueError` with path) a `BigInt` that
is not `-?[0-9]+` and `RegExp.Flags` outside `[dgimsuvy]*`, with one goja-backed
test per case. Same class, lower priority because only developer-typed: `Temporal.Kind`
is never checked (`Temporal{Kind:"alert(1)//"}` → `alert(1)//.from("x")`), and an
unknown `TypedArrayKind` is rejected only when its buffer is unshared
(`uneval.go:507-523`; with a shared buffer `NewTypedArray("Bogus;alert(1)", buf)`
emits `new Bogus;alert(1)(a)`). Checking kinds against the declared constants
closes both.

### 2. issue — `refKey` panics on unhashable values instead of returning `DevalueError`, and the replacer never sees them

Type: crash condition with reproduction; breaks the PLAN's "rejection messages and
paths" and the replacer-visitation contract.

Evidence:

- `devalue/uneval_value.go:306-311` keys `reflect.Func` values on `v` itself.
  Go func values are never hashable.
- `devalue/uneval_value.go:318` uses `rv.Type().Comparable()`, which is true for
  any struct or array with interface-typed fields regardless of what they hold.
- `devalue/uneval.go:165` then does `u.byKey[key]` → runtime panic, before the
  replacer call at `uneval.go:174`.

Reproduction:

```go
type resp struct{ Data any }
rep := func(v any, _ func(any) (string, error)) (string, bool, error) {
    if _, ok := v.(resp); ok { return "REPLACED", true, nil }
    return "", false, nil
}
devalue.UnevalWith(devalue.NewObject("r", resp{Data: 1}), rep)        // {r:REPLACED}
devalue.UnevalWith(devalue.NewObject("r", resp{Data: []any{1}}), rep) // PANIC: hash of unhashable type []interface {}
devalue.Uneval(devalue.NewObject("f", func() {}))                     // PANIC: hash of unhashable type func()
devalue.Uneval(struct{ X any }{X: []int{1}})                          // PANIC: hash of unhashable type []int
```

Impact: the failure is data-dependent. A replacer-handled custom value type — the
transport-hook use case the `Replacer` doc describes — works until one instance
happens to carry a slice or map in an `any` field, then takes down the render
with a runtime panic rather than a pathed `DevalueError`. Upstream offers a
function to the replacer and otherwise throws `Cannot stringify a function`; the
port's own "Function in nested structure" fixture substitutes a string type for
the function, which is why the suite never reaches this.

Smallest fix: use `rv.Comparable()` (value-level, Go ≥ 1.20) at line 318 and return
`dedupe=false` for `reflect.Func`; both then fall into the existing
"one chance at the replacer, then refuse" branch at `uneval.go:152-163`. Add the
three inputs above to `TestUnevalErrors`.

Related, lower weight: that same branch's comment says a typed nil "still gets one
chance at the replacer before it is refused", but typed nils of the modeled
pointer types (`(*Object)(nil)`, `*Map`, `*Set`, `*Boxed`, `*TypedArray`,
`*DataView`) are keyed at `uneval_value.go:260-271` and then dereferenced — all
six panic. Flat `Stringify` panics on the same inputs today, so this is parity
with existing behavior rather than a regression; the comment is what is wrong.

### 3. nitpick — equal `Date`/`RegExp`/`URL`/`URLSearchParams` values are hoisted as one shared object, contradicting upstream and the "byte-identical" claim

`devalue/uneval_value.go:272-281` keys these by value. Two distinct equal dates:

- Go: `(function(a){return [a,a]}(new Date(1)))`
- devalue 5.9.2: `[new Date(1),new Date(1)]`

`Date`, `URL`, `URLSearchParams` and stateful (`g`/`y`) `RegExp` are mutable in
the client, so `$[0].setTime(…)` now changes `$[1]`. The comment at
`uneval_value.go:243-247` calls this "safe for hydration" while the adjacent
empty-array comment calls the same kind of aliasing "a real aliasing bug". This
is a deliberate, recorded representation constraint, it matches what flat
`Stringify` already does, and it cannot be fixed without an API change — so it is
not blocking. What is missing is the user-facing statement: README, the website
guide and the skill reference all now say Uneval output is "byte-identical to
devalue 5.9 for every supported shape" with no mention of this or of the
empty-container exception (10/3,000 differential mismatches). One sentence in the
package comment would make the claim true.

### 4. nitpick — symbol-level documentation misses items PLAN slice 4 lists; two harvested comments went stale

- `Replacer` (`uneval.go:10-21`) says the string is "spliced … verbatim" but not
  that it is *trusted JavaScript* nor that it is *not a flat `Reducer`*; the
  package comment (`value.go:1-8`) does not say generated typed codecs still
  target the flat format. Both statements exist in README/guide/skill only; the
  plan asked for them in the package and symbol comments. Given finding 1, the
  trust statement is worth having where godoc shows it.
- `type URL string` (`uneval_value.go:9`) lost the doc comment it has in skgo
  (`value.go:72-78` there), leaving an undocumented exported type beside a
  documented `URLSearchParams`.
- `uneval_value.go:235` ("ptrKey stands in…") and `:243` ("dateKey and the other
  value keys below") describe identifiers that are now `referencePtrKey` /
  `referenceDateKey`; `referencePtrKey` is field-for-field identical to
  `internal.go`'s `ptrKey`, where the plan said to share the low-level key
  representation.

### 5. nitpick — the committed pinned-upstream comparison exercises no special values

`TestUnevalGolden` consumes 324 recorded cases, but the corpus is the
type-grammar generator's JSON-shaped values plus the three added cases. Counting
`uneval` strings in `golden.json`: `new Date`, `new Map`, `new Set`, `new RegExp`,
`Object(`, bigint literals, `void 0`, `NaN`, `Infinity`, `-0`, `.buffer`,
`__proto__:null` and `Object.assign` all occur **zero** times (the added
`sparse_array` has length 10, below the cost-rule flip, so it records the literal
form). All of those are flat-representable, which is the set the plan said to
record. Their only proof is hand-transcribed expectations in `uneval_test.go`.

No defect hides there today — the 3,000-graph differential and the 8 typed-array
spot checks above all match the pin — so this is a proof gap, not a bug, and the
plan did say "add only the small cyclic, shared-reference, and sparse cases". If
it is worth closing, a handful of curated `write(...)` lines in
`testdata/record/main.go` (one Date/Map/Set/RegExp/BigInt/boxed/null-proto value
and one `Array(40)` sparse case) does it with the existing mechanism and no new
infrastructure.

## Checked and found sound

- Exported API matches skgo name-for-name; helpers `asFloat`, `formatNumber`,
  `maxArrayIndex`, `isArrayIndexString`, `propertyOrder` are reused, not copied.
- `quoteString` parity with upstream `stringify_string`/`safe_key` for valid
  UTF-8, including `<`, U+2028/2029, controls, quotes and backslashes.
- Hoisting order, stable count sort, `get_name` numbering and reserved-word
  suffix, reconstruction-before-statements ordering, the 65,534-parameter
  fallback, sparse cost rule and trailing-hole comma all match `uneval.js`.
- New representations fail explicitly in flat `Stringify`
  (`TestNewUnevalValuesRemainUnsupportedByFlatStringify`); flat goldens are
  byte-unchanged; `goja` is a test-only import and `goja_nodejs` was not added.
- goja tripwire is exact (91); remaining skips are only URL, URLSearchParams and
  Temporal; the five lone-surrogate snapshots are excluded with a stated reason.

## Outcome

material findings remain
