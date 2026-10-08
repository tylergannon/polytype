# Separate devalue runtime migration

decision: The approved devalue migration plan removes the bundled runtime without an alias shim, retaining devalue/codegen. Release as a polytype minor using the existing semantic-release workflow.
friction: Initial baseline overlapped a root fast-forward and saw incomplete compiler imports -> isolated stable checkout passes all baseline tests.

doc_bug: Migration plan says the root JS devalue pin is recorder-only, but TestRecursiveDevalueJSInterop uses it to verify generated typed codecs. Retain that test dependency and describe its codegen ownership; deleting it would silently skip existing interoperability proof.

friction: Generated fixtures live in nested modules, so root go mod tidy cannot see their runtime imports. The fixture dependency test imports the standalone runtime and identifies its real package path, preserving the root dependency needed by the fixture source tracker.
friction: just lint's modernize -fix rewrites newly merged grammar LoaderConfig flag parsing and import formatting unrelated to runtime migration -> discard those automatic edits after verifying lint succeeds; keep this change scoped to the runtime separation.

decision: Independent review found that tidy drops standalone runtime requirements in two fixtures before their generated code exists. Pin that dependency with an import in existing fixture tests, then tidy all changed fixture modules.
decision: Include the small pre-existing grammar modernize/import-format change so just lint is idempotent; the reviewer reproduced the drift.
