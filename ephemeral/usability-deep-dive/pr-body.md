`polytype.Declare` currently behaves as scanner-only syntax, which prevents another Go program from configuring and invoking the complete generator without adding a tagged registration file and schema stubs to the model package. It also makes JSON Schema unavoidable even when a consumer only needs TypeScript, devalue, or generated Go JSON codecs.

This change makes declarations ordinary executable configuration values and adds the public `codegen` package. `Declare[T]()` selects a type without implying a schema method, while `Declare(T.Schema)` retains the traditional accessor identity. Callers can compose declarations and sealed-union settings, retain field identity with `Field[T]("Name")`, and independently select JSON Schema, Go JSON codecs, TypeScript, or devalue output. The existing build-tagged declaration syntax remains supported by the same API.

The integration fixture proves three distinct library workflows from a separate Go module with no registration file or stubs: TypeScript plus devalue emits no schema artifacts, schema-only generation requires no schema method, and generated enum/sealed-union MarshalJSON and UnmarshalJSON code works without a schema directory.

Validation:

- `go test ./...`
- `just build-tagged`
- `just lint`
- `git diff --check`
