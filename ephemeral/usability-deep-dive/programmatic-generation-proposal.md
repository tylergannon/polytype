# Make code generation callable as a library

This is a high-level proposal: requirements, suggested structure, examples, a work sequence, and a definition of done. It is not an instruction to implement the change now or a detailed API specification.

## Goal and starting point

Make polytype straightforward to embed in another Go tool. The caller should configure and initiate generation directly, without manufacturing files containing registration calls and then asking the CLI to rediscover that configuration.

TypeScript and devalue already have library generators. JSON Schema and its generated Go accessors, validation, and codecs are still tied to the registration workflow. The solution should expose the whole supported generation surface coherently.

## The central requirement: those same functions return configuration

The user's requested shape is:

```go
//go:build jsonschema

var _ = polytype.Declare(foo.bar).Whatever(baz)
```

and, elsewhere in an ordinary generation program:

```go
config := polytype.Declare(foo.bar).Whatever(baz)
codegen.Gen(config)
```

These examples express the requirement, not frozen signatures or a required blank-binding convention. **The exact same declaration and option functions must return usable configuration objects in both places.** Every option retains its arguments and contributes to the returned configuration. Another program can store, compose, and pass that object directly to generation.

It is not sufficient to have no-op markers interpreted by a scanner and a separate executable API that happens to produce the same internal representation. There must be one declaration API with one implementation of its meaning, documented once. Only the invocation instructions differ.

The user explicitly permits completely reinventing the traditional schema.go language, including the stub and marker-call conventions. Keep the file-based workflow available, but do not preserve old syntax at the expense of this requirement. For example, an evaluated field value may lose its identity; use an explicit field name or symbol if necessary. The final spelling and binding convention are implementation choices.

## Suggested structure and formalities

Use Go itself as the configuration language. Declaration calls construct ordinary values; helpers, composition, loops, and conditionals work normally. Merely constructing configuration should not trigger generation or global registration.

A small conceptual grammar is enough to guide the design:

```text
Configuration = selected roots + type/field rules + requested outputs
Rule          = enum | field encoding | union | reference | schema provider
Generation    = configuration + source context -> generated artifacts
```

Resolve configuration into the existing closed type grammar: scalars, time, enums, ordered objects, pointers, slices, fixed arrays, and references, with required/optional/nullable and supported union field forms. Preserve descriptions, identities, field order, enum/discriminator values, and array lengths. Static JSON Schema must consume this grammar too. Keep dynamic providers explicit where their output cannot be projected statically.

Both workflows supply configuration to the same engine. File collection may locate bindings, but must obtain values produced by the shared functions rather than supply separate AST-only option semantics. The direct API must work without a registration file or user-authored schema stubs in the model package. Allow caller-selected roots, explicit source context, and reuse of source/type information another generator already loaded.

Let callers select outputs and receive artifacts before writing them. A TypeScript-only request should not force schema files or Go accessors. Checking generated output should compare actual destination bytes without modifying them; application of output is explicit. Address first-run loading and references to generated methods during implementation. Do not turn this into unrelated expansion of the supported type system.

## Nine basic steps to success

1. **Start with the user's two examples.** Demonstrate one identical declaration expression returning real configuration in a file binding and an external program.
2. **Build the shared configuration API.** Make every declaration and option store its meaning; support ordinary Go composition and replace source-only syntax where needed.
3. **Separate loading from registration.** Resolve caller-selected types without registration files, reuse loaded source information, and handle first-generation dependencies.
4. **Unify static lowering and projection.** Make JSON Schema use the shared grammar while preserving supported rules and explicit provider capabilities.
5. **Expose generation directly.** Consume configuration through the public library API and return only the requested artifacts and emitted identifiers.
6. **Separate checking and writing.** Detect missing or altered output using actual contents; preflight destinations before applying changes.
7. **Migrate the file-based workflow.** Collect configurations from the same executable API and update traditional declarations and shipped examples as necessary.
8. **Prove real consumers.** Exercise both workflows, generated runtime behavior, independent output selection, and first-run/regeneration cases.
9. **Document once and remove duplication.** Publish one declaration reference with short instructions for each invocation workflow; retire no-op declarations and superseded generation machinery.

## Definition of done

- **Same API, same meaning:** identical declaration expressions in both contexts return equivalent configuration and output. Include helper-based composition so a second syntax-only interpreter cannot satisfy the check.
- **Usable externally:** a separate Go module configures and generates output without adding registration files or stubs to the model package or launching the polytype CLI. First generation and regeneration work, including the supported generated-method reference cases.
- **Supported capabilities retained:** representative enums, enum-name encoding, unions/discriminators, refs, providers, validation, YAML, TypeScript, and devalue work through the shared configuration model. Unsupported combinations produce clear diagnostics.
- **Behavior demonstrated:** generated consumers compile and execute. Test encode/validate/decode behavior. Represent byte-like slices correctly or consistently refuse them; fixed arrays must reject wrong-length input instead of validating data that Go silently truncates.
- **Outputs under caller control:** schema-only and TypeScript-only requests work; artifacts can be consumed by a caller-owned writer. Check mode reports altered/missing files without writes, even when checksum sidecars remain unchanged.
- **Migration complete:** examples use the executable API, old forms are migrated or rejected with useful guidance, and configuration is documented once. Appropriate tests, go test ./..., tagged builds, and generation/drift checks pass.

The first deliverable is one configuration expression producing correct JSON Schema through both workflows. Use that working result to guide the broader migration, rather than specifying every API detail in advance.
