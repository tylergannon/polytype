---
title: Custom backends
description: Lower Go types into polytype's type grammar and write your own projection.
---

Every backend in polytype, the JSON Schema generator, the TypeScript
generator, and the devalue codec generator, consumes the same closed type
grammar. Two packages expose that grammar so another tool can add a
projection without going through the CLI or reading schema files.

## `typegrammar`: the grammar

`github.com/tylergannon/polytype/typegrammar` defines the node set:

| Kind | Nodes |
| --- | --- |
| Types | `Scalar`, `Time`, `Enum`, `Object`, `Pointer`, `Slice`, `Array`, `Ref` |
| Field values | `Required`, `Optional`, `Nullable`, `Union`, `OptionalUnion`, `UnionSlice` |

Objects are closed, ordered property sets. References form a DAG; recursion is
rejected. Ordinary values are non-null, and absence and null are separate field
constructors. Unions carry resolved discriminators and variants. There is no
`any`, map, or open-union node, and `Definitions.Validate` is the admission
boundary every backend relies on.

New node kinds may be added in minor versions. A type switch over the nodes
must carry a `default` case that reports the unrecognized kind rather than
ignoring it.

## `grammar`: lowering Go types

`github.com/tylergannon/polytype/grammar` is the public entry point to the
lowering the CLI performs:

```go
import (
    "go/token"
    "go/types"

    "github.com/tylergannon/polytype/grammar"
)

pkg, err := grammar.Load("./model")          // loads with the jsonschema build tag, like the CLI
scope := pkg.Types().Scope()                 // type-checked scope of the package
defs, roots, err := pkg.Lower([]grammar.Root{
    {Type: scope.Lookup("Order").Type()},
    {Type: types.NewSlice(scope.Lookup("Order").Type()),
     Position: token.Position{Filename: "roots", Line: 1}},
})
```

- `Load(dir)` reads the package at `dir` and the packages it references.
- `Types()` returns the `*types.Package` for looking up roots. A root from
  your own `packages.Load` also works; roots are matched structurally and by
  name.
- `Lower(roots)` returns the validated definition graph reachable from the
  roots plus one grammar node per root, in order. Named roots need no
  `Declare` marker. Anything the CLI would refuse in a struct field is refused
  here with the same diagnostic; `Position` is reported for anonymous roots,
  which have no source location of their own.

Registrations in the package's build-tagged file still apply: `.StringerEnum`
switches an enum field to name mode, `SealedUnion[I](name)` sets a union's
discriminator, and `func (T) enum()` markers and sealing methods are read from
the untagged source.

## Writing the backend

A backend is a function over `defs` and `roots`. Walk each definition's type,
switch over the node kinds, and emit your target. The devalue backend in
[`devalue/codegen`](https://github.com/tylergannon/polytype/tree/main/devalue/codegen)
is a compact worked example: it allocates names, emits one encoder and one
decoder per definition and per root, and refuses the one shape it cannot spell
(an anonymous struct under a slice, array, or pointer) with a diagnostic
naming the grammar path.

Prove a backend the way the repository proves its own: generate into a
throwaway module, then build and run it under `go test`.
