# Make code generation callable as a library

Proposal only; no implementation is requested by this task.

## Core requirement

Use **exactly one executable configuration API**. The functions called in schema.go must return the same usable configuration objects as those called directly by another Go program. Every chained option retains its arguments. Document this API once.

The old declaration language may be completely replaced. Preserve the file-based workflow, not no-op markers or a separate AST-only interpretation of their meaning. Another generator must be able to construct configuration and generate output without creating registration files or stubs in the model package.

## Suggested boundaries

- Configuration describes **roots, rules, and requested outputs**; ordinary Go functions and composition build it.
- Generation takes **configuration plus source context** and returns artifacts. Writing artifacts is a separate operation.
- Both invocation workflows use the same compiler and shared static type grammar. Schema providers remain an explicit capability with clear projection limits.
- Keep naming, binding collection, and package organization open to the implementer. Avoid expanding the supported type system during this work.

## Suggested sequence

1. Demonstrate one identical declaration expression in both workflows.
2. Make declarations and options construct real configuration values.
3. Load caller-selected types independently of registration files.
4. Align static JSON Schema with the shared grammar.
5. Expose generation with explicit output selection and returned artifacts.
6. Separate read-only checking from output writes.
7. Migrate schema.go declarations to the executable API.
8. Prove external consumers and generated runtime behavior.
9. Document the API once and remove duplicate generation machinery.

## Done means

- The same declaration expression returns equivalent configuration and output in both workflows, including when composed through helpers.
- An external Go program generates usable output without model registration files, stubs, or a CLI subprocess; first generation and regeneration both work.
- Existing supported capabilities have representative executable coverage. Generated consumers compile and run; the observed byte-slice and fixed-array inconsistencies are corrected or consistently refused.
- Callers select outputs and control writes. Check mode detects altered or missing artifacts without modifying them.
- Examples are migrated, the configuration API has one reference, and unit tests, tagged builds, and generation/drift checks pass.
