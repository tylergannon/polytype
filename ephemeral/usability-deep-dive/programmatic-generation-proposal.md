# Programmatic generation proposal

Status: proposed API, not implemented. This follows the review at 964f778. Preserve the supported legacy registration syntax while making a Go program the primary integration surface. This is a design proposal, not an accepted nlspec.

## Architecture

```text
Executable Go configuration ─┐
                            ├─ Config → Compile → Program → Render → Result
Legacy registration scanner ┘                                      ├─ caller-owned bytes
                                                                  ├─ Check (read-only)
                                                                  └─ Apply (explicit writes)
```

Add a public `github.com/tylergannon/polytype/codegen` package. It owns orchestration and the public configuration API. Keep heavy loading and generation dependencies out of the root marker/runtime package. Existing TypeScript and devalue packages remain independently callable.

The language is Go: struct literals plus small fluent constructors. Calls return real values. They never write source, invoke providers, register global state, or execute generation as an initialization side effect. A caller can build configuration with ordinary functions, loops, and conditionals. No separate configuration file is required.

## Configuration grammar

```text
Config      = Source + Roots + Rules + Outputs
Source      = LoadOptions | LoadedPackages
Root        = TypeRef + FieldRules + OptionalOutputName
TypeRef     = QualifiedTypeName | GoTypesReference
Rule        = Enum(TypeRef)
            | EnumNames(OwnerTypeRef, GoFieldName)
            | SealedUnion(InterfaceRef, DiscriminatorName)
            | SchemaRef(TypeRef)
            | Provider(OwnerTypeRef, GoFieldName, ProviderKind, SourceSymbol)
Output      = JSONSchema(Destination)
            | GoMethods(DeclaringPackage, Destination, MethodOptions)
            | TypeScript(Destination, Options)
            | Devalue(TargetPackage, Destination, Options)
```

Qualified names contain the Go import path and declared type name; a field identity is its owner type plus Go field name. JSON names are resolved from tags. Provider references identify a source function or method and its calling convention; closures and evaluated field values cannot replace source identities.

Roots and rules are independent: configuring an enum or a dependency's reference policy does not implicitly request an output root for it. `Declare(...).EnumNames("Priority")` is fluent sugar for a root plus a field rule. This new name describes the actual constant-name wire mode; the legacy `StringerEnum` spelling remains accepted by the legacy adapter.

Rules are ordinary data. Resolve unknown symbols, unsupported shapes, provider signatures, incompatible projections, invalid package destinations, and duplicate/conflicting rules during Compile. Do not use last-one-wins behavior. Explicit configuration does not silently discover legacy declarations; importing them is an explicit operation. Legacy CLI invocation imports them by default. Existing enum markers and interface sealing methods remain source facts, and explicit Enum rules allow models without polytype enum markers. SealedUnion continues to infer only the currently supported sealed-interface membership; this proposal does not add arbitrary open unions.

Source loading accepts an explicit module directory and build settings, including tags and overlays, or already-loaded packages with the syntax and type information required for lowering. Caller-supplied `go/types` roots are resolved against that source context; type identity alone does not supply comments and declaration metadata. Output paths are relative to an explicit output root, never an accidental process working directory.

## Example: executable configuration

All names below are proposed API names. Assume the model declares Order, Priority and its typed constants, Payment as a sealed interface, its variants, and Address. It needs no polytype registration file or schema method stubs.

```go
import gen "github.com/tylergannon/polytype/codegen"

model := gen.Package("example.com/shop/model")

cfg := gen.Config{
    Source: gen.Source{Dir: "/work/shop"},
    Roots: []gen.Declaration{
        gen.Declare(model.Type("Order")).EnumNames("Priority"),
    },
    Rules: []gen.Rule{
        gen.Enum(model.Type("Priority")),
        gen.SealedUnion(model.Type("Payment")).Discriminator("kind"),
        gen.SchemaRef(model.Type("Address")),
    },
    Outputs: []gen.Output{
        gen.JSONSchema("model/jsonschema"),
        gen.GoMethods(model, gen.MethodOptions{
            File:         "model/jsonschema_gen.go",
            Schema:       "Schema",
            ValidateJSON: true,
        }),
        gen.TypeScript("web/src/generated"),
    },
}

result, err := gen.Generate(ctx, cfg)
if err != nil {
    return err
}
// result.Files contains relative paths and bytes.
// result.Names exposes allocated identifiers per output and source identity.
// Source loading has happened, but no generated files have been written.

return result.Apply(ctx, "/work/shop")
```

GoMethods preserves inferred enum and owner codecs and requests accessor/validation methods via its options. Methods are generated in the package declaring their receiver. The schema destination must be compatible with Go embedding; Compile diagnoses an invalid relationship. Running the generator from another tool does not relax Go's restrictions on defining methods for foreign types.

A caller wanting only TypeScript chooses only `gen.TypeScript(...)`. No JSON Schema, checksum, accessor, or Go method output is implicit in the programmatic API. A schema-only caller receives JSON files and can consume their bytes directly. The old CLI retains its existing default outputs.

For a tool that already loaded its packages and selected a type:

```go
cfg.Source = gen.Source{Packages: loadedPackages}
cfg.Roots = []gen.Declaration{
    gen.Declare(gen.FromType(argumentType)),
}
cfg.Outputs = []gen.Output{
    gen.TypeScript("generated"),
}
result, err := gen.Generate(ctx, cfg)
```

The loaded-package source requires syntax, type information, dependencies, and source positions; it avoids a second package load. Existing anonymous-root restrictions remain explicit backend capabilities rather than being silently widened.

## Canonical resolved grammar

Keep and complete the current `typegrammar` rather than inventing a new general type system:

```text
Definitions = ordered Definition*
Definition  = QualifiedName + Description + SourcePosition + Type
Type        = Scalar(GoKind)
            | Time
            | Enum(GoKind, WireMode, ConstantMembers)
            | Object(ordered Field*)
            | Pointer(Type)
            | Slice(Type)
            | Array(Length, Type)
            | Ref(QualifiedName)
Field       = GoName + JSONName + Description + SourcePosition + FieldValue
FieldValue  = Required(Type) | Optional(Type) | Nullable(Type)
            | Union(Interface, Discriminator, Variants)
            | OptionalUnion(Union) | UnionSlice(Union)
Variant     = QualifiedName + PointerIdentity + WireTag
```

Retain the current operand restrictions, non-recursive references, numeric widths, exact enum constants, pointer identity, source diagnostics, and field ordering. Do not add maps, recursion, arbitrary generics, or a permissive any node as part of this API change. Byte-like slices are consistently refused initially; fixed-array length becomes an actual schema constraint. TypeScript remains structural, with its expressiveness limits documented. Numeric precision, presence and nil behavior, custom wire methods, and encoding/json omissions remain explicit capability/validation rules rather than an assumed consequence of sharing a graph.

The resolved Program contains this graph, selected roots, resolved projection settings, Go output bindings, and explicit schema-provider plans. Static schema generation consumes this grammar. Providers are a schema-specific extension: their source symbols and bindings live in explicit schema plans, not an opaque any node passed to portable backends. Provider-dependent roots retain the existing schema/rendered-method behavior; requesting a static projection that needs unknown provider output fails before rendering. This preserves dynamic functionality without claiming it has a statically known portable shape.

Public operations:

```go
Compile(ctx context.Context, cfg Config) (*Program, error)
Render(ctx context.Context, program *Program) (Result, error)
Generate(ctx context.Context, cfg Config) (Result, error) // Compile + Render
```

Compile snapshots and validates input without mutating it. Render returns deterministic output bytes and allocated names without destination writes. Independent backend entry points remain available for callers that already have an admitted graph. There is no global registration registry.

## Legacy compatibility

Existing code continues to work:

```go
//go:build jsonschema

func (Order) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Order.Schema).
    StringerEnum(Order{}.Priority)

var _ = polytype.SealedUnion[Payment]("kind")
```

The scanner converts the AST into the same source/type/field/provider configuration values. It records the legacy schema entrypoint name and output binding. It no longer constructs a second schema model or owns the generation pipeline. Preserve supported legacy free functions, Ref, provider options, RenderProviders, build tags, validation flags, and default paths.

The exact evaluated legacy field expression cannot construct a complete runtime field rule: `Order{}.Priority` loses its field identity after evaluation. Keep that spelling source-scanned. The new executable declaration uses `.EnumNames("Priority")` with an owner identity. New executable declarations return meaningful data; the old marker return values need not pull the generation engine into every model's runtime dependency graph.

Bootstrap is part of the implementation. When normal source already calls generated methods, the loader can synthesize the configured signatures in an in-memory overlay and exclude owned generated implementations while resolving source. Preserve legacy tagged stubs when loading legacy packages. Handle source type-check diagnostics selectively: missing generated signatures can be supplied from configuration; unrelated errors must remain errors. Never require consumers to write or commit a bootstrap file.

## Check and apply

`Result.Check(ctx, outputRoot)` compares actual bytes and file existence and reports changes without creating directories, temp files, sums, or outputs. `.sum` files may remain in compatibility output but never decide whether an artifact actually matches.

`Result.Apply(ctx, outputRoot)` validates destination ownership and collisions before writes, stages generated files, and replaces each file atomically. Do not claim transactionality across arbitrary multi-file I/O failures without rollback. Return accurate failure state. Automatic deletion is limited to provably generator-owned outputs; no directory-wide cleanup. Callers can ignore both helpers and write `Result.Files` themselves.

## Nine steps to success

1. **Fix the public contract with a consumer example.** Agree the Config, Generate, Result, and explicit-root shapes. The acceptance fixture is an external module with ordinary model source and no registration file or method stub. Keep unrelated type-system expansion out of this change.
2. **Implement configuration values and symbol resolution.** Add pure declaration/rule constructors, explicit enum/discriminator/ref/provider configuration, output selection, source identities, conflict diagnostics, and deterministic config normalization. Prove building config performs no I/O or global registration.
3. **Separate loading from registration discovery.** Support explicit roots, caller build settings and overlays, and already-loaded source packages. Implement configured method signatures in overlays for the first-run/rebuild case; prove generation when owned output is absent and callers refer to configured methods.
4. **Make static JSON Schema a backend of the admitted grammar.** Preserve supported schema behavior, exact descriptions/order, reference policy, and enum/union rules. Consistently refuse byte-like slices and encode fixed-array bounds. Keep explicit provider plans for dynamic schemas. Convert the review findings into corrected regression expectations.
5. **Return complete results from every backend.** Expose JSON Schema projection, route TypeScript and devalue through their existing APIs, and extract Go accessor/validation/codec rendering from filesystem writes. Validate requested capabilities and return allocated names. Prove TypeScript-only and schema-only generation.
6. **Build strictly read-only Check and explicit Apply.** Compare real destination bytes, detect missing files and path/ownership collisions, retain compatibility sums only as outputs, and report I/O failures honestly. Prove no writes on check and no writes for preflight failures.
7. **Translate legacy registrations into Config.** Route old CLI invocations through the new engine. Verify representative old/new configuration equivalence for enums, unions, refs, free functions, providers, validation, YAML, tags, and output paths. Isolate intentional correctness differences in schema goldens.
8. **Prove the integration from an external module.** Generate, build, and execute output without registration files or user-authored stubs; test already-loaded packages and method bootstrap; demonstrate encode/validate/decode behavior and fixed-array rejection. Run `go test ./...`, `just build-tagged`, and regeneration/drift checks. Compilation alone is insufficient for wire claims.
9. **Make the library API the documentation front door.** Lead with a runnable Go generator example and the three operations Compile/Render/Apply; show output selection and source-loading choices. Keep a short legacy workflow and migration mapping. Delete the obsolete static schema traversal once parity is proven, while retaining provider-specific rendering where required.

The first deliverable should be one external program generating correct JSON Schema through the new API. Complete that executable slice before broadening the migration across every output.
