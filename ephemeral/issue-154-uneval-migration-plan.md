# Issue 154: move devalue `uneval` into `polytype/devalue`

Status: implementation plan for review

Authoritative issue: https://github.com/tylergannon/polytype/issues/154

Source implementation: `tylergannon/skgo` at
`528c14cb06cf67c211d7549449b80b296a0a711f`, under `internal/devalue`.

Upstream semantic reference: npm `devalue@5.9.2`, already pinned in this
repository's `package.json`. The reviewed tarball has npm shasum
`2a3a8ad21904c6a630bf7bb160acb7ca2fa1e467`.

## Desired result

`github.com/tylergannon/polytype/devalue` owns both supported devalue output
forms:

- `Stringify` / `Parse`: the existing flat-array transport format;
- `Uneval` / `UnevalWith`: a JavaScript expression that reconstructs the value,
  including shared references and cycles.

The package exposes the generic API and value representations currently owned
by skgo. The implementation is published in a new Polytype release so skgo
issue 149 can delete its generic serializer and import Polytype directly.

The change must preserve the existing flat runtime and generated typed codec
behavior. It does not change schema generation, type grammar, SvelteKit
hydration envelopes, deferred-promise numbering, transport-hook expressions,
or document assembly.

## Scope boundary

This issue owns the generic serializer, generic replacer/error API, value
representations required by that serializer, tests of generic serialization,
and the Polytype release.

It does not perform the downstream skgo migration. It does not add every type
supported by upstream devalue to Polytype's flat codec merely because the type
is needed by `Uneval`. Any new value that flat `Stringify` does not support must
continue to fail explicitly rather than being silently widened or partially
encoded.

## Existing evidence and starting condition

- `go test -count=1 ./...` passes on Polytype `v1.0.4` / commit `bce9498`.
- `go test ./internal/devalue` passes in skgo at the source commit.
- The skgo implementation already aliases Polytype's `Undefined`, `Hole`,
  `Object`, `Map`, `Set`, `Date`, `BigInt`, `RegExp`, `ArrayBuffer`, `Boxed`,
  and flat serializer API. There is no competing base model to reconcile.
- skgo contains 1,511 lines of production source and 1,042 lines of tests for
  this serializer. Its execution suite evaluates ordinary fixtures in goja,
  asserts reconstructed values, cycles, and reference identity, and accounts
  for engine features goja lacks.

## Design

### Public API

Add the public surface already used by skgo:

- `Uneval(any) (string, error)`
- `UnevalWith(any, Replacer) (string, error)`
- `QuoteString(string) string`
- `Replacer`
- `DevalueError`, including the rejection path
- URL, URLSearchParams, Temporal, TypedArray, and DataView representations,
  their enum-like kinds, constructors, and typed-array helpers currently used
  by the harvested tests and consumer.

Preserve names and signatures unless a concrete conflict or correctness defect
requires a change. The downstream issue should be an import rewrite, not an API
adaptation project.

### Reuse existing Polytype helpers

Do not copy private helpers that are already the same implementation:

- `asFloat`
- `formatNumber`
- `maxArrayIndex`
- `isArrayIndexString`
- `propertyOrder`

Their current Polytype and skgo bodies are functionally identical. The
harvested Uneval code should call the existing package helpers.

The two identity questions remain separately named:

- `identityKey` is the flat codec's SameValueZero-style key policy for Map and
  Set values, including primitives.
- `referenceKey` is Uneval's object-reference policy for counting aliases and
  cycles and for offering otherwise unsupported objects to the replacer.

Share low-level key representations where that reduces duplication. Add a
container kind to slice-backed pointer keys so an array and ArrayBuffer cannot
alias merely because their address and length coincide. Do not collapse the two
policies into a Boolean mode or otherwise obscure their different contracts.

Extend `identityKey` only far enough to make newly introduced representations
obey the existing `Map` / `Set` API: pointer representations compare by pointer
identity and value representations by their serialized value. This closes the
concrete case where `NewSet(typedArray, typedArray)` currently records two Go
entries but emits a JavaScript Set of size one. It does not add flat wire
support for those representations.

### String literals and malformed Go strings

Upstream uses one `stringify_string` helper for both flat `stringify` and
`uneval`; Polytype should likewise have one script-safe JavaScript string
literal implementation. `QuoteString` and Uneval should reuse the existing
Polytype string writer after verifying parity for ordinary valid UTF-8,
controls, quotes, backslashes, `<`, U+2028, and U+2029.

Do not turn malformed Go strings into a subproject. The skgo port contains raw
WTF-8 surrogate fixtures whose output is invalid UTF-8 JavaScript and which the
execution suite explicitly skips. Those fixtures do not demonstrate working
software. For this issue:

- preserve and execute all valid-string upstream behavior;
- retain Polytype's existing behavior for malformed Go strings unless a
  concrete consumer or executable upstream case proves it wrong;
- remove or rewrite claims that raw invalid bytes are valid JavaScript proof;
- account for the excluded lone-surrogate fixtures explicitly in the test or
  package documentation.

A separate malformed-string policy is warranted only if an executable failing
case demonstrates user-visible loss. It is not a prerequisite for shipping the
serializer migration.

### Value-model and flat-format boundary

Place expression-specific representations in the existing `devalue` package so
one tree can be handed to the appropriate serializer. Do not duplicate or alias
types that already live there.

Adding a public representation does not automatically add flat wire support.
For each new type, tests must state whether `Stringify` currently supports it.
Unsupported cases must return an error. Existing `Stringify`, `Parse`, flat
goldens, fuzzing, and `devalue/codegen` behavior remain unchanged.

Full flat support for upstream URL, typed-array, DataView, or Temporal tags is
not part of issue 154 unless implementation reveals that the existing API
cannot remain coherent without it. If that occurs, stop and amend scope rather
than smuggling the expansion into the migration.

### JavaScript execution tests

Migrate the skgo snapshot fixtures and goja execution tests with the generic
serializer. Keep the tests in the primary Go module:

- ordinary `go test ./...` must discover them;
- clients importing `polytype/devalue` do not compile or link test imports;
- a synthetic empty-module-cache consumer build has already demonstrated that
  a test-only `goja` requirement downloads no module source during a normal
  build;
- a nested test module would make the required root test command silently skip
  the execution proof.

Add `goja` to `go.mod` only as required by the tests. Do not add
`goja_nodejs`; the migrated generic tests use the JavaScript runtime, not Node
compatibility modules.

Retain the suite's explicit lower bound on executed fixtures so a change in skip
behavior cannot quietly turn the test into snapshots only. Preserve direct
checks for:

- reconstructed primitive and container values;
- self-cycles and mutual cycles;
- repeated reference identity;
- sparse array holes and length;
- typed-array/buffer sharing where supported by goja;
- replacer-produced expressions;
- script-safe escaping;
- the large-graph parameter fallback;
- rejection messages and paths.

Keep unsupported-engine skips named and counted. Delete a skipped fixture only
when its premise is invalid for the Go API, and record that decision next to the
remaining accounting.

Audit skips against the pinned goja version rather than carrying skgo's comments
forward. BigInt, boxed BigInt, BigInt typed arrays, and replacer fixtures that
only need test globals are known to execute in that version and must be enabled.
A skip survives only when the pinned runtime actually fails because a named
global is unavailable, such as URL, URLSearchParams, or Temporal. Raw WTF-8
surrogate snapshots are excluded and accounted for separately because their
current output is invalid JavaScript source; do not reinterpret replacement
characters as proof of the original surrogate semantics. Set the executed-case
tripwire from the audited corpus, not the stale skgo count.

### Upstream comparison

Treat `devalue@5.9.2` as the semantic authority. Use the existing npm pin rather
than a floating checkout. For the migrated fixtures:

- retain the exact upstream test or source location in comments where one
  exists;
- compare output to the pinned implementation for deterministic supported
  cases;
- use JavaScript evaluation for behavioral claims, not snapshots alone;
- distinguish a deliberate Go representation constraint from an accidental
  port difference.

Extend the existing `devalue/testdata/record` mechanism with a sibling Uneval
golden sourced from the pinned npm package. For values the current flat format
can represent, record upstream `stringify` and `uneval`; the Go test parses the
recorded flat value, calls `Uneval`, and compares the expression. Add only the
small cyclic, shared-reference, and sparse cases missing from the existing
recorded corpus. Expression-specific values that cannot pass through the flat
format remain focused hand-built Go fixtures, with their header corrected to
cite `devalue@5.9.2` rather than the absent floating-checkout path. This extends
the existing recorder; it does not introduce a ledger or provenance framework.

## Implementation slices

### 1. Establish the shared model and helper seam

Move only the expression-specific value definitions and constructors from
skgo's `value.go`. Reuse the existing base types and numeric/property helpers.
Introduce the separately named reference-key policy and add focused tests for
cross-container identity and replacer visitation.

This slice is complete when existing flat tests remain unchanged and the new
model compiles without the Uneval algorithm.

### 2. Harvest Uneval and its literal helpers

Move the serializer, typed-array rendering, identifier/property rendering,
parameter naming, replacer, and error type. Adapt package-private calls to the
shared Polytype helpers. Route all JavaScript string literals through the one
shared writer and apply the bounded malformed-string decision above.

This slice is complete when migrated snapshot/rejection fixtures pass without
changing existing flat outputs.

### 3. Restore behavioral proof

Move the goja tests and test dependency. Execute emitted expressions and assert
independently written semantic conditions, particularly cycles and shared
identity. Audit every skip and preserve the executed-fixture tripwire.

This slice is complete when the migrated package tests demonstrate behavior,
not merely byte equality.

### 4. Document the two contracts

Update the `devalue` package comment and public symbol comments to state:

- which functions implement the flat format;
- which functions implement JavaScript expressions;
- that the value model is shared but support is serializer-specific;
- that `Replacer` returns trusted JavaScript and is not a flat `Reducer`;
- that generated typed codecs still target the flat format.

Do not add general SvelteKit documentation to Polytype; skgo remains responsible
for how it embeds these expressions in hydration documents.

Also correct the existing durable descriptions that would otherwise contradict
the released API: the root README, the website devalue guide, the Polytype skill
reference, and the `AGENTS.md` package-layout summary. Their wording should say
that the additional representations are available to `Uneval` but are not
currently supported by the flat format. Regenerated website API output needs no
hand edit.

### 5. Qualify and publish

Before merge:

- run `gofmt` / `goimports` on changed Go files;
- run focused `go test -count=1 ./devalue ./devalue/codegen`;
- run `go test -count=1 ./...`;
- run `just build-tagged` because repository completion requires tagged example
  registrations to compile even though this change should not affect them;
- run `npm ci --ignore-scripts --no-audit --no-fund` and any focused pinned
  upstream comparison added by the implementation;
- regenerate the pinned Uneval golden and run the Go comparison that consumes
  it;
- verify `git diff --check` and review the complete diff.

Use a conventional additive `feat` change so semantic-release selects the
appropriate minor release from the current `v1.0.4` baseline. After merge,
verify the GitHub release and public module version. From a fresh module with no
local `replace`, import the released `polytype/devalue`, call `Uneval`,
`UnevalWith`, and `QuoteString`, and evaluate at least one cyclic/shared
expression. The published dependency, not the implementation worktree, is the
acceptance boundary for issue 154.

## Completion criteria

Issue 154 is complete only when:

- the public API is in `polytype/devalue` and the generic skgo source can be
  replaced by imports without an adapter;
- migrated fixtures cover shared identity/cycles, sparse arrays, special
  values, script-safe escaping, replacers, and rejection paths;
- the committed `devalue@5.9.2` Uneval golden is reproducible and its Go
  comparison passes, including the added cyclic/shared/sparse cases;
- emitted JavaScript is evaluated for supported engine cases, with skips
  explicitly accounted for;
- existing flat runtime and generated codec tests are green;
- the two serializer contracts are unambiguous in package documentation;
- a released Polytype version passes a fresh-consumer proof without a local
  replacement.

## Review adjudication

Independent review is advisory evidence, not an instruction stream. Revise
this plan only for a finding grounded in at least one of:

- an explicit issue or repository contract;
- an existing Polytype or skgo consumer requirement;
- a concrete executable failure or failing input;
- a direct contradiction with the pinned upstream implementation.

Do not accept hypothetical malformed-input machinery, broader format parity,
new provenance infrastructure, generalized hardening, or adjacent cleanup
without demonstrated material impact on issue 154. Prefer the smallest safe
change that publishes the working serializer and unblocks skgo.
