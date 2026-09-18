# Issue 154 implementation review adjudication

Reviewer: Fable 5.1 through `tractor run-prompt`

Session: `54d352c5-3827-4f44-8b38-74831a7ec496`

Source review: `202609181726-issue-154-implementation-round-01.md`

## Finding 1: executable type metadata

Accepted. Fable supplied working injection inputs for free-form `BigInt` text and
`RegExp.Flags`, plus the same executable-constructor risk for forged Temporal
and typed-array kinds. Uneval now validates BigInt decimal syntax, unique
ECMAScript regexp flags, declared Temporal kinds, and declared typed-array
kinds before rendering. Regression tests cover the exact flat
`Parse -> Uneval` injection path and the expression-only kinds.

## Finding 2: unhashable reference keys panic

Accepted. The review demonstrated panics for functions and structs whose
interface fields hold slices. Reference keys now use value-level
`reflect.Value.Comparable`, functions take the existing replacer-or-error path,
and typed nils do not reach modeled pointer dereferences. Tests cover pathed
errors and successful replacement of an unhashable custom value.

## Finding 3: value-modeled identity differs from JavaScript

Accepted as a documentation correction, not a redesign. Go value forms cannot
distinguish equal-but-separate Date, RegExp, URL, URLSearchParams, or Temporal
objects, and zero-length slices cannot prove shared identity. The package and
user documentation now state that boundary; the public representations remain
compatible with skgo.

## Finding 4: stale or incomplete documentation

Accepted. Replacer now explicitly says it receives trusted JavaScript and is not
a flat Reducer; the package comment says generated codecs target the flat
format; URL and reference-key comments were corrected; the duplicated low-level
pointer key was replaced with the existing shared `ptrKey`.

## Finding 5: recorded upstream corpus lacks special values

Accepted narrowly because issue 154 explicitly requires pinned-upstream checks
for special values. The existing recorder gained curated Date, Map, Set,
RegExp, BigInt, boxed primitive, ArrayBuffer, null-prototype, undefined,
non-finite/negative-zero, and cost-rule sparse cases. No new proof framework was
introduced.

No hypothetical malformed-string machinery, flat-format expansion, identity API
redesign, or adjacent cleanup was accepted.
