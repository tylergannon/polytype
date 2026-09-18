# Issue 154 plan review adjudication

Reviewer: Fable 5.1 through `tractor run-prompt`

Session: `6499f620-406c-417d-85dc-4b106afa9ba3`

Source review: `202609181648-issue-154-plan-round-01.md`

## Finding 1: pinned-upstream check is optional

Accepted. This is an explicit issue requirement, and the existing recorder is a
small native seam. The plan now requires a sibling pinned Uneval golden, a Go
comparison, a few missing cyclic/shared/sparse cases, and a completion gate.

## Finding 2: stale goja skip accounting

Accepted in part. BigInt, BigInt typed arrays, and custom-replacer fixtures must
execute because the pinned runtime supports them or only needs test globals.
Actual missing globals may remain named skips. Raw WTF-8 fixtures are explicitly
excluded rather than converted into replacement-character tests, which would
not prove their original semantics.

## Finding 3: durable docs contradict the API

Accepted. Update the existing README, website guide, skill reference, and
repository package-layout summary. This is required contract repair, not new
documentation architecture.

## Finding 4: Map and Set identity misses new representations

Accepted narrowly. The reviewer supplied a concrete failing TypedArray Set
case, and the shared value model should not build two entries that serialize as
one. Extend identity handling for the new representations without widening the
flat wire format.

## Finding 5: diagnostic-path escaping differs from upstream

Rejected. The difference affects diagnostic spelling only, remains script-safe,
and has no demonstrated consumer impact. Changing it would not advance issue
154.
