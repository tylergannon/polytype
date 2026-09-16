# Remove the redundant named-container union refusal in gen_schema.go

Task: the third `unsupportedRegisteredInterfaceContainer` site in
`resolveRegisteredInterfaceField` (named type whose underlying is an
array/slice of a registered interface) was reported as UNREACHABLE by mutation
testing: deleting it left `go test ./...` green.

## correction: the "unreachable" premise was wrong

The block is *reached*. Instrumenting all four refusal sites with unique
markers and logging the error for each of the six cases in
`TestUnsupportedRegisteredInterfaceContainersFailDuringGeneration` gave:

| case                 | site that fires                |
| -------------------- | ------------------------------ |
| fixed array field    | gen_schema.go:1418             |
| nested slice field   | gen_schema.go:1418             |
| optional slice field | gen_schema.go:1397             |
| nullable slice field | gen_schema.go:1397             |
| named slice field    | **gen_schema.go:1426 (the block in question)** |
| top-level named slice| typegrammar.go:335             |

Only `top-level named slice` was refused by the earlier site. The task's claim
that `named slice field` was "already refused by an earlier site" is false —
that case is refused *by the block itself*.

decision: the block is REDUNDANT, not dead. Mutation testing stayed green
because deleting it lets the field fall through to `l.named()`, whose
`*dst.ArrayType` case (typegrammar.go:331) is the grammar's admission boundary
and emits the identical diagnostic. Same message, different site.

friction: "delete it and tests stay green" cannot distinguish dead code from
code shadowed by an equivalent downstream guard -> mutation results need a
follow-up that identifies *which* site produces the surviving error, not just
that an error survives.

## Proof

Probed 11 shapes before and after deletion (throwaway `zz_probe*_test.go`,
removed afterwards). Before deletion the block fired for six shapes: named
slice, named fixed array, named nested slice, named slice-of-pointer, optional
named slice, nullable named slice. After deletion all six are still refused
with the same message via typegrammar.go:331, and the error chain is strictly
better: it names the field *and* the named type's declaration site, which is
the information the deleted message hand-rolled as "through named type X".

No shape flipped from refused to accepted. `named-slice-of-struct-holding-
variant` (`type Variants []Inner` where `Inner` has a union field) was accepted
both before and after — the block only walked idents in the underlying array
expr, so it never caught that, and it is legitimately supported.

Cross-package check (sealed interface + named slice in a dependency package,
relevant after #138 switched dependency resolution to export data): still
refused after deletion, naming the dependency's own declaration.

## Change

- Deleted the block in `resolveRegisteredInterfaceField`.
- Deleted `SchemaBuilder.resolveNamedType`, which had no other caller and would
  have tripped staticcheck U1000.
- Left a comment stating that named containers are deliberately resolved at the
  grammar boundary instead.
- Extended the test table with the seven named-container shapes the deleted
  block had been absorbing (named fixed array, named nested slice, named
  slice-of-pointer, optional/nullable named slice, slice of named slice, named
  slice through a second named type) plus a cross-package test.

decision: added those tests rather than deleting bare, because before this
change only one of the eight named-container shapes was pinned at the surviving
guard. Verified they are not vacuous: removing the `isRegisteredInterface`
guard at typegrammar.go:331 fails all nine named-container cases, while the
four non-named cases keep passing at their own sites.

## Final state

`go test ./...`, `just lint`, `just build-tagged` all clean.
Branch: claude/suspicious-euclid-d95331
