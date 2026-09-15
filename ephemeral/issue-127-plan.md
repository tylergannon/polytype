# Issue #127: recursive types, without recursive JSON Schema

## Scope

Support finite tree values whose Go types recurse through structs, slices,
existing Optional/Nullable pointer forms, and sealed-union variants.
Include mutual recursion. Preserve existing omission, null, enum, and
discriminator rules; bare pointer fields do not gain new null semantics.
Defer recursive JSON Schema and schema-backed validation. Cyclic runtime
graphs and object-identity preservation remain outside the typed-codec contract.

## 1. Fix source discovery and lowering

- In `internal/syntax/scan_result.go:resolveTypeExpr`, treat an already-active
  named type as a discovered edge and stop descending, rather than returning
  `cyclic dependency found`. Track active versus completed discovery; complete
  a type only after all its fields and dependencies have been inspected.
- Keep Go type-checking errors, unsupported-shape diagnostics, and source
  positions. No parser rewrite or migration of the whole loader is needed.
- In `grammar.Load`, construct a scan/configuration-only builder. It currently
  calls `builder.New`, which maps registered roots into schemas immediately.
  Loading a package containing recursive declarations must not invoke schemas.
- Keep `typeGrammarLowerer.named`'s existing reserve-before-descend behavior:
  allocate one definition per package-qualified name, then fill its body.
  Recursive occurrences remain `Ref` edges; union variants remain named edges.
- Make dependency and embedding walks terminate explicitly. Preserve field
  selection rules; do not unroll recursive embedding until the depth cap.

## 2. Admit named recursion in the grammar

- In `typegrammar/validate.go`, separate named-definition traversal from inline
  constructor traversal. A back edge through a resolved named reference or
  union implementation is allowed; a literal cycle of constructor pointers is not.
- Still validate every definition and each context-sensitive use. A previously
  visited payload must not skip union-tag, discriminator, nullable-operand, or
  string-mode enum checks at its new use site.
- Make `dereference` and shape predicates terminate on reference-only loops;
  reject unresolved references and nonproductive alias loops deterministically.
- Update the grammar's DAG documentation and cycle tests to distinguish valid
  named recursion from malformed hand-built graphs. No new node kind is needed.

## 3. Separate Go JSON codec planning from schema rendering

- `codegen.Generate` currently selects schema mapping for `GoJSON()` too.
  Split that decision: schema construction is required only for schema output.
- Extract the existing owner/enum/union codec discovery from `mapNamedType`
  into a visited-by-name walk of the selected roots and reachable types.
  Follow containers, wrappers, and every sealed-union implementation; include
  nested owners even when only the outer root was declared.
- Populate the existing `OwnerCodec`, enum plans, and interface helper metadata
  directly from resolved source/configuration. Preserve method-collision,
  embedding, discriminator-collision, and unsupported-container checks.
  Avoid imposing the stricter portable grammar on previously supported GoJSON input.
- Reuse `schemas.go.tmpl`: owner aliases prevent self-calls, while named child
  fields invoke their own generated methods. Union helpers dispatch to concrete
  variants and add/read the discriminator at each recursive level.
- Keep ordinary structs on standard JSON serialization where no generated
  adapter is needed. Retain temporary-value decoding before receiver assignment.
  Ensure nested codec failures retain field/index context.

## 4. Keep devalue generation recursive by function call

- `devalue/codegen` already allocates names up front and emits `encT`/`decT`
  calls for references and union variants. Preserve this; never inline a named
  recursive definition. Audit supporting walks for assumptions of acyclicity.
- Verify `EncodeT`/`DecodeT` and `StringifyT`/`ParseT`, including nested tags,
  optional/null termination, invalid nested values, and field/index errors.
- The low-level devalue parser/stringifier already has object-reference support;
  finite recursive types do not require a new wire format or runtime rewrite.
  Keep the typed API's tree-only contract explicit; do not add identity support.

## 5. Preserve named TypeScript output

- `typescript.Generate` already allocates all names before projection and emits
  `Ref` as a name. Keep recursive properties such as `children: Array<Node>`.
- Retain field-local union expressions such as
  `Array<(Omit<Leaf, "type"> & { type: "Leaf" }) |
  (Omit<Branch, "type"> & { type: "Branch" })>` for `Branch.children`.
  Preserve configured discriminator names/tags and optional/null annotations.
- Verify recursive aliases and union expressions compile using the existing
  Go-driven tsc test harness; change the printer only if that exposes a defect.
  No runtime TypeScript serializer or new public union-alias API is required.

## 6. Make the schema deferral usable

- Use the existing `codegen.Gen` options: `GoJSON`, `TypeScript`, and `Devalue`
  without `JSONSchema` or `Validation`. No additional public flag is needed.
- Recursive schema requests return a clear unsupported diagnostic before any
  selected backend writes files. Keep acyclic schema behavior intact.
- The schema-first CLI remains unable to generate recursive models in this
  slice; document the programmatic entry point as the supported route.

## Acceptance evidence

- A Go integration fixture generates, compiles, and runs both codecs for the
  issue's `Node` and `Branch` examples, plus pointer/optional and mutual recursion.
  Use an ordinary object root containing the interface, matching today's API.
- Several-level Go JSON round trips preserve concrete variants and discriminator
  values at every level. Plain JSON parsing/stringifying in Node preserves the wire.
- Generated devalue codecs exchange the same finite nested values with the pinned
  JavaScript devalue runtime in both directions and retain semantic equality.
- Malformed nested tags/values fail with useful paths; JSON decode failure leaves
  its receiver unchanged. Existing nil/empty and absence/null rules remain tested
  separately for each transport, without assuming they are identical.
- Repeated generation is byte-identical, recursive TypeScript compiles, schema
  requests fail before writes, and existing acyclic fixtures remain compatible.
- Update Go unit/integration tests for these cases; install the pinned Node tools
  so interoperability checks run. Finish with `go test ./...` and tagged builds.
