---
applyTo: '**'
---

# polytype Internal Developer Notes

> Historical snapshot: this document records early implementation experiments.
> Current support and release boundaries are in [`docs/spec/v1.md`](spec/v1.md)
> and the current examples. The old TODO entries below are retained as raw
> history; completed provider, enum, entrypoint, and Optional/Nullable work is
> not implied to remain pending. The experimental YAML surface was removed
> before v1.

Owner: opencode

Last updated: 2025-08-10

Changelog
- Added internal/common/tags.go: centralized JSONSchema tag parsing (ref and param plan groundwork).
- Added builder pipeline hook (Transform) with RegisterTransform and applyTransforms in Run (no-op default). Tests remain green.
- Scanner now exposes MarkerFunctionCall.Args() and allows variadic options on NewJSONSchemaMethod; added parsing scaffold for sentinel options (WithFunction, WithStructAccessorMethod, WithStructFunctionMethod) into SchemaMethod.Options. Not yet used by builder; no behavior change.
- Introduced TemplateHoleNode to emit raw template placeholders; added TypeProviders plumbing in SchemaBuilder (now used in codegen template generation), and prepped template imports (bytes, text/template). Tests still green.


This is an internal, living document for deep understanding, navigation, and refactoring of polytype. It is deliberately exhaustive. Keep this up to date.

## 0) Quick facts
- Purpose: Generate JSON Schemas from Go types for LLM tool/function calls.
- Outputs: jsonschema/*.json (+ *.sum) and jsonschema_gen.go with method impls + custom Unmarshalers.
- Build tag flow: `//go:build jsonschema` gates stub methods/markers; generator loads package with `-tags=jsonschema`.
- Entry points:
  - CLI: polytype/gen
  - Core: internal/syntax (scan) → internal/builder (render) → files + gen code.
- Supported features: structs, primitives, arrays, enums, union types via interfaces, refs via struct tag, Optional/Nullable wrapper fields, description tags/comments.

## 1) Architecture map
- polytype (CLI)
  - main.go: `gen` (the only subcommand, and the default). Calls builder.Run(...)
- internal/syntax (package loader, AST scanner, type resolver)
  - loader.go: load packages with `-tags=jsonschema`.
  - scan_result.go: central scan pipeline → collects markers, local types, interfaces, enums, resolves types, tracks remote deps.
  - scan_expr.go: parse marker calls and schema method descriptors from AST.
  - node_wrappers.go: rich wrappers over dst AST (TypeSpec, StructType, StructField, etc.). Tag parsing, Required/Skip logic, PropNames.
- internal/builder (schema nodes + file/code generation)
  - gen_schema.go: SchemaBuilder: map types → internal schema nodes → write json files → render jsonschema_gen.go.
  - model.go: internal schema JSON model: ObjectNode, PropertyNode[T], ArrayNode, UnionTypeNode, RefNode. Custom MarshalJSON.
  - schemas.go.tmpl: generated code template; embeds jsonschema dir; emits method impls and interface unmarshaler helpers.
  - import_map.go, printer.go: template/goimports helpers.
- Public helpers (api) in root
  - json_schema.go: separate JSONSchema type and helper builders (StringSchema, ArraySchema, EnumSchema, ParentSchema...). Intended for manual schema construction.
  - union_type.go: marker functions: NewJSONSchemaMethod, NewEnumType, NewInterfaceImpl, NewJSONSchemaBuilder.

## 2) End-to-end flow
1. User writes:
   - schema.go (under build tag) + method stubs returning json.RawMessage
   - marker calls: NewJSONSchemaMethod, NewEnumType, NewInterfaceImpl, etc.
   - go:generate directive to run the generator.
2. polytype gen: loads package with jsonschema tag; internal/syntax finds markers, types, enums, interfaces; resolves types recursively (local + remote) and enforces invariants.
3. internal/builder maps each registered type into internal schema nodes.
   - Primitives → PropertyNode
   - Arrays → ArrayNode
   - Structs → ObjectNode with Properties and Required (`Optional[T]` fields are omitted)
   - Interfaces → UnionTypeNode(anyOf). Discriminator property injected when serializing union.
   - Ref via tag jsonschema:"ref=..." → RefNode
4. Writes jsonschema/<Type>.json (+ <Type>.json.sum checksum).
5. Writes jsonschema_gen.go (excluded from jsonschema build tag) with:
   - func (T) SchemaMethodName() json.RawMessage { read embed }
   - custom json.Unmarshaler for structs with interface-typed fields → reads discriminator and dispatches to impl type.

## 3) Key types (internal schema model)
- ObjectNode: Desc, Properties(ObjectPropSet = []ObjectProp{Name, Schema, Optional}), Discriminator (string), TypeID_. MarshalJSON: emits type:object, description, properties, required (computed), additionalProperties:false.
- PropertyNode[T]: Desc, Enum, Const, Typ (string), TypeID_. MarshalJSON: type, description, const (if set), enum (if set).
- ArrayNode: Desc, Items(JSONSchema), TypeID_.
- UnionTypeNode: Options []ObjectNode (each an object schema). MarshalJSON: { anyOf: [ object-with-discriminator, ...] }, discriminator property name defaults to `type`.
- RefNode: emits {"$ref": "..."}.

## 4) Tag semantics
- json: standard behavior for names, skipping ("-").
- polytype.Optional[T]: direct named field is not required and must use `json:",omitzero"`.
- polytype.Nullable[T]: direct named field remains required and its schema accepts null.
- jsonschema:"ref=...": replace field schema with $ref (field skipped from traversal).
- description:"...": overrides comment-sourced description for the field.

## 5) Interface/union semantics
- Register with NewInterfaceImpl[YourInterface](Impl1{}, Impl2{}, (*Impl3)(nil))
- Scanning records the interface and option types (pointer/value).
- JSON Schema for interface is anyOf of option object schemas with required discriminator `type` const equal to the type name.
- Generated code: per-interface unmarshal helper switching on discriminator; per-struct UnmarshalJSON for fields that are interface-typed in local structs.

## 6) Strengths
- Clear split: scanning (syntax) vs building (builder) vs CLI.
- Deterministic generation with checksum guard and --no-changes.
- Build-tag strategy isolates stubs/markers from normal builds.
- Practical interface union handling with discriminator and generated Unmarshalers.
- Good unit/integration coverage using test fixtures and golden files.

## 7) Weaknesses / Over-complications
- Two schema models: public (json_schema.go) vs internal (internal/builder/model.go). Divergent behavior and duplication increase cognitive load.
- Lots of custom string-based MarshalJSON code; manual JSON assembly increases maintenance risk vs using structs + encoding/json consistently.
- AST wrappers add indirection and learning curve; some logic (e.g., skipping, tags) is split across syntax and builder.
- Interface field handling restrictions (must be at field type position) are implicit; errors occur deep in traversal when violated.
- Discriminator injection happens at union serialization time (hidden coupling).
- CLI option NumTestSamples currently unused in code path.
- AdditionalProperties hard-coded to false in internal ObjectNode; less flexible than public helper API.

## 8) Targeted refactor suggestions
1) Unify schema model
- Option A: make builder/model use the public JSONSchema type from json_schema.go and delete the internal model. Extend public type with bits we need (e.g., discriminator helper/util).
- Option B: remove public helpers and expose a thin adapter; pick one canonical model.
- Benefit: single mental model; easier extension (e.g., parameterization feature).

2) Reduce custom string-building
- Replace manual MarshalJSON with plain struct forms and encoding/json where practical. Keep a few custom cases (discriminator prepend) minimal.

3) Centralize tag parsing
- Move all struct tag evaluation to a single utility and pass resolved attributes (optional, ref, description, param) forward. Avoid duplicating parsing between syntax and builder.

4) Make union discriminator explicit in schema nodes
- Add DiscriminatorPropName to UnionTypeNode or a SchemaOptions context passed during render to make the behavior less magical.

5) Improve method signature capture
- Extend scanning to capture schema method signatures now (needed for parameterization); use it to validate they return json.RawMessage and to reproduce signature in generated code.

6) Prune unused features
- Remove or implement NumTestSamples; keep surface area minimal.

7) Testing granularity
- Add unit tests for small pieces (renderStructField rules, tag parsing). Fewer golden surprises.

## 9) Parameterized fields and sentinel API plan (preview)
Two approaches that can coexist:

A) Tag-driven parameters (caller-supplied args at runtime)
- Tag: `jsonschema:"param=Name[,idx=N]"`
- Stub method accepts json.Marshaler params.
- Generator emits text/template schema and runtime rendering using provided args.

B) Sentinel-option API (type-safe provider functions)
- NewJSONSchemaMethod(T.JSONSchema, options...)
- Options:
  - WithFunction(fieldExpr, func(TField) json.Marshaler)
  - WithStructAccessorMethod(fieldExpr, methodExpr func(Receiver) json.Marshaler)
  - WithStructFunctionMethod(fieldExpr, methodExpr func(Receiver, TField) json.Marshaler)
- At runtime, generator invokes providers to build per-field schema fragments.
- IMPORTANT: pass the actual receiver instance, not a zero value. For method expressions, we call f(receiver, value) or f(receiver) as appropriate.

Scanner updates
- Variadic args captured from MarkerFunctionCall.Args(); parse which With* sentinel is used, extract:
  - Targeted field (from composite literal field selector).
  - Provider function expression (free or method expr) and argument expectations.

Codegen updates
- For fields with overrides: use template placeholders in JSON file and, in generated method, compute placeholder values by calling providers with the actual receiver instance and field value expression, then execute the template.

Back-compat: unchanged when no options/tags present.


## 10) Open questions
- Allow parameterization of interface fields (overriding union)? Initially no; keep union machinery.
- Allow combining `ref` and `param`? No; conflict. Emit friendly error.
- Pretty-print template output? Keep as-is; template preserves formatting.

## 11) Working notes
- Discriminator default name = `type` (DefaultDiscriminatorPropName). Template has access to this in generated code (schemas.go.tmpl uses `{{$discriminatorProp}}`).
- Interface unmarshaler function name format: `__jsonUnmarshal__<pkgName>__<TypeName>`.

## 12) TODO (engineering roadmap)
- [ ] v1: implement new entry points
  - [ ] NewJSONSchemaFunc(Func(T) json.RawMessage) scanning
  - [ ] NewJSONSchemaBuilder(Func() json.RawMessage) scanning
  - [ ] Goldens for both forms matching method output
- [ ] v1: consolidated options parsing on registrations
  - [ ] WithEnum / WithEnumMode(EnumStrings) / WithEnumName
  - [ ] WithInterface / WithInterfaceImpls / WithDiscriminator
  - [ ] WithRenderProviders (generate RenderedSchema())
  - [ ] Conflict detection with legacy NewEnumType/NewInterfaceImpl
- [ ] v1: provider rendering
  - [ ] Generate RenderedSchema() when WithRenderProviders is set
  - [ ] Provider collection and invocation (accessor, struct-func, free func)
  - [ ] Template execution with map[string]json.RawMessage
  - [ ] Deterministic tests for rendered output
- [ ] v1: enums incl. iota
  - [ ] Detect iota blocks; numeric mode default
  - [ ] String mode with String() fallback and WithEnumName overrides
  - [ ] Generate (un)marshalers where applicable
- [ ] v1: docs parity
  - [x] Draft v1 spec
  - [ ] Fill error message examples and migration
- [ ] Decide on schema model unification (public vs internal) and implement.
- [ ] Add focused unit tests for tag parsing and field rendering logic.
- [ ] Review and address CLI option drift (NumTestSamples).
- [ ] Document v1 features in README and examples.
