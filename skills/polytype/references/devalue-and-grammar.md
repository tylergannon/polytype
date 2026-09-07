# devalue Transport and Custom Backends

Read this when a Go service must speak SvelteKit's devalue wire format, or
when you need polytype's type grammar to write a new projection. Both are Go
library packages in the `github.com/tylergannon/polytype` module; the CLI
does not drive them.

## The devalue runtime — `devalue`

devalue is the structured-value format SvelteKit uses for `load` data and
remote functions. `github.com/tylergannon/polytype/devalue` ports its flat
`stringify`/`parse` pair and is byte-identical to devalue 5.9 for every shape
it implements.

```go
import "github.com/tylergannon/polytype/devalue"

s, err := devalue.Stringify(devalue.NewObject("name", "Ada", "tags", []any{"a", "b"}))
// [{"name":1,"tags":2},"Ada",[3,4],"a","b"]

v, err := devalue.Parse(s, nil) // *devalue.Object; numbers float64, null nil
```

- Value model: `*devalue.Object` (ordered properties; `Get`, `Set`, `Keys`;
  `map[string]any` is accepted on encode with sorted keys), `[]any`,
  `string`, Go numeric kinds (float64 after parse), `bool`, `nil`,
  `devalue.Undefined`, `devalue.Hole`, and the tagged forms `Date`, `*Map`,
  `*Set`, `BigInt`, `RegExp`, `ArrayBuffer`, `*Boxed`.
- `StringifyWith(v, []Reducer)` and `Parse(s, revivers)` are the custom-type
  hooks, tried in order before the built-ins.
- Typed arrays, `URL`, `URLSearchParams` and `Temporal` are not implemented;
  a payload containing one parses to an "Unknown type" error.

## Typed codecs — `devalue/codegen`

Generation is a small Go program you own. Run it from a `//go:generate`
directive in the package that will hold the codecs:

```go
//go:generate go run ./gen
```

```go
// gen/main.go
package main

import (
    "go/token"
    "go/types"
    "os"

    "github.com/tylergannon/polytype/devalue/codegen"
    "github.com/tylergannon/polytype/grammar"
)

func main() {
    pkg, err := grammar.Load("./model") // same loader the CLI uses
    if err != nil { panic(err) }
    scope := pkg.Types().Scope()
    defs, roots, err := pkg.Lower([]grammar.Root{
        {Type: scope.Lookup("Envelope").Type()},
        {Type: types.NewSlice(scope.Lookup("Envelope").Type()),
         Position: token.Position{Filename: "gen.go", Line: 1}},
    })
    if err != nil { panic(err) }
    src, err := codegen.Generate(defs, roots, codegen.Options{
        PackageName: "codec",
        ImportPath:  "example.com/app/codec",
    })
    if err != nil { panic(err) }
    if err := os.WriteFile("codec_gen.go", src, 0o644); err != nil { panic(err) }
}
```

Rules:

- Roots need no `Declare` marker and may be anonymous types such as
  `[]Envelope`. `Position` appears only in diagnostics; set it for anonymous
  roots, which have no source location.
- `PackageName` is required. `ImportPath` is the import path of the package
  the file lives in, so its own types are referenced unqualified.
- Emit into a package other than the one declaring the types; the output
  uses only exported fields.
- Run `go mod tidy` afterwards. The file imports the `devalue` runtime.
- Commit the generated file and re-run generation when the types change.

For every definition `T` reachable from the roots the file declares:

```go
func EncodeT(v model.T) (any, error)     // Go value → devalue value model
func DecodeT(raw any) (model.T, error)   // strict
func StringifyT(v model.T) (string, error)
func ParseT(s string) (model.T, error)
```

Anonymous roots get the same four functions named `Root0`, `Root1`, … by
index. Pair `EncodeT` with `devalue.StringifyWith` and `DecodeT` with
`devalue.Parse` when reducers or revivers are needed.

### Wire rules

- Every Go numeric kind is a JavaScript `number`; integers beyond 2^53 lose
  precision. There is no BigInt.
- `time.Time` is the string `encoding/json` writes, never a `Date`.
- Absent `Optional` is no property; absent `Nullable` is `null`; a nil
  required slice encodes as `[]`; a nil pointer where a value is required is
  an encode error.
- Enums honor their mode (values, or constant names under `.StringerEnum`)
  and reject non-members on both sides.
- Unions are one object whose discriminator property (default `type`, or
  the `SealedUnion[I](name)` override) holds the concrete type name.
- Decoders reject missing required properties, unknown properties, wrong
  kinds, `undefined`, `null` outside `Nullable`, wrong array lengths,
  unknown union tags, and every devalue tagged form. Errors carry a
  JSON-pointer-style path such as `/events/1/kind`.
- An anonymous struct is admitted as a field's own type (directly or under
  `Optional`/`Nullable`) but not under a slice, array or pointer, and not as
  a root.

Complete module covering every node kind, with tests:
`devalue/codegen/testdata/fixture` in the repository.

## Your own projection — `grammar` and `typegrammar`

`typegrammar` is the closed grammar every backend consumes. Nodes: `Scalar`,
`Time`, `Enum`, `Object`, `Pointer`, `Slice`, `Array`, `Ref`. Field values:
`Required`, `Optional`, `Nullable`, `Union`, `OptionalUnion`, `UnionSlice`.
Objects are closed, ordered property sets; references form a DAG;
`Definitions.Validate` is the admission boundary. There is no `any`, map, or
open-union node. New kinds may appear in minor versions, so every type switch
needs a `default` case that reports the unknown kind.

`grammar` is the lowering entry point:

```go
pkg, err := grammar.Load(dir)                    // *grammar.Package
scope := pkg.Types().Scope()                     // *types.Package scope
defs, roots, err := pkg.Lower([]grammar.Root{...}) // Definitions, one Type per root
```

`Lower` refuses maps, channels, functions, unsealed interfaces, recursion,
misplaced presence wrappers, and the rest with the same diagnostics the CLI
prints. Walk `defs` and `roots` to emit your target; the devalue backend in
`devalue/codegen` is a compact worked example.
