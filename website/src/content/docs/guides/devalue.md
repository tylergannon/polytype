---
title: devalue transport
description: Generate strict Go codecs for the devalue wire format SvelteKit uses, from the same Go types.
---

[devalue](https://github.com/sveltejs/devalue) is the structured-value wire
format SvelteKit uses for `load` data and remote functions. polytype ships a
Go port of its flat `stringify`/`parse` pair and a generator that emits typed
Go codecs for your types on that wire. A Go service can then produce exactly
what `devalue.parse` expects in the browser and consume what
`devalue.stringify` sends back, with every value checked against the type
grammar on the way in.

Both pieces are Go packages in the polytype module. The CLI does not drive
them; you write a short generator program.

## 1. The runtime: `devalue`

```go
import "github.com/tylergannon/polytype/devalue"

s, err := devalue.Stringify(devalue.NewObject("name", "Ada", "tags", []any{"a", "b"}))
// [{"name":1,"tags":2},"Ada",[3,4],"a","b"]

v, err := devalue.Parse(s, nil) // *devalue.Object; numbers are float64, null is nil
```

- Value model: `*devalue.Object` with ordered properties (`Get`, `Set`,
  `Keys`; a `map[string]any` is accepted on encode with sorted keys),
  `[]any`, `string`, the Go numeric kinds (float64 after parse), `bool`,
  `nil`, `devalue.Undefined`, `devalue.Hole`, and the tagged forms `Date`,
  `*Map`, `*Set`, `BigInt`, `RegExp`, `ArrayBuffer`, `*Boxed`.
- `StringifyWith(v, reducers)` and the `revivers` argument to `Parse` are
  devalue's custom-type hooks, tried in order before the built-ins.
- Output is byte-identical to devalue 5.9 for every shape the port
  implements. Typed arrays, `URL`, `URLSearchParams` and `Temporal` are not
  implemented and parse to an error.

## 2. Generate typed codecs: `devalue/codegen`

Put a generator program next to the package that will hold the codecs and
run it from a `//go:generate` directive:

```go title="codec/gen.go"
//go:generate go run ./gen
package codec
```

```go title="codec/gen/main.go"
package main

import (
    "go/token"
    "go/types"
    "os"

    "github.com/tylergannon/polytype/devalue/codegen"
    "github.com/tylergannon/polytype/grammar"
)

func main() {
    pkg, err := grammar.Load("../model") // same loader the CLI uses
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

```bash
go generate ./codec && go mod tidy
```

- Roots need no `Declare` marker and may be anonymous types such as
  `[]Envelope`. `Position` appears only in diagnostics; give anonymous roots
  one, since they have no source location.
- `PackageName` is required. `ImportPath` is the import path of the package
  the file lives in, so its own types are referenced unqualified.
- Emit into a package other than the one declaring the types. The output uses
  only exported fields.
- The generated file imports the `devalue` runtime, so run `go mod tidy`.
  Commit the file and regenerate when the types change.

## 3. Use the generated functions

For every definition `T` reachable from the roots the file declares:

```go
func EncodeT(v model.T) (any, error)     // Go value → devalue value model
func DecodeT(raw any) (model.T, error)   // strict; rejects what the grammar does not admit
func StringifyT(v model.T) (string, error)
func ParseT(s string) (model.T, error)
```

Anonymous roots get the same four functions named `Root0`, `Root1`, … by
index. Pair `EncodeT` with `devalue.StringifyWith` and `DecodeT` with
`devalue.Parse` when you need reducers or revivers.

```go
payload, err := codec.StringifyEnvelope(envelope) // send to the browser
envelope, err := codec.ParseEnvelope(body)        // from devalue.stringify in the browser
```

## Wire rules

- Every Go numeric kind is a JavaScript `number`; integers beyond 2^53 lose
  precision. There is no BigInt.
- `time.Time` is the string `encoding/json` writes, never a `Date`.
- An absent `Optional` is no property; an absent `Nullable` is `null`; a nil
  required slice encodes as `[]`; a nil pointer where a value is required is
  an encode error.
- Enums honor their mode (values, or constant names under `.StringerEnum`)
  and reject non-members on both sides.
- Unions are one object whose discriminator property (default `type`, or the
  `SealedUnion[I](name)` override) holds the concrete type name, exactly as
  in the JSON Schema and TypeScript projections.
- Decoders reject missing required properties, unknown properties, wrong
  kinds, `undefined`, `null` outside `Nullable`, wrong array lengths, unknown
  union tags, and every devalue tagged form. Errors carry a JSON-pointer-style
  path such as `/events/1/kind`.
- An anonymous struct is admitted as a field's own type, directly or under
  `Optional`/`Nullable`, but not under a slice, array or pointer, and not as a
  root.

A complete module covering every grammar node kind, with tests that build and
run it, lives at
[`devalue/codegen/testdata/fixture`](https://github.com/tylergannon/polytype/tree/main/devalue/codegen/testdata/fixture).
The [Go API reference](/api/) has the full contract.
