# Loaded package lowering

friction: the required baseline on Go 1.27.2 failed because x/tools v0.49.0 only decodes export version 4, while Go writes version 5. Upgrade to v0.52.0 and tidy fixture modules that pin its indirect dependencies before testing the API.
friction: codegen tests still look for tsc after the repository replaced its pin with tsgo, so npm ci alone silently skips their strict TypeScript checks. Use the existing pinned tsgo binary.
decision: FromPackages accepts the original packages.Config and same-load package graph. go/packages does not retain build provenance, so callers must attest the snapshot's exact tags, overlays and module view; duplicate package identities are rejected, and missing reached source is an error instead of an implicit load.
correction: marker admission previously reopened the declaration file to check its build constraints, which loses overlay-only declarations. The supplied path checks AST headers against the snapshot build settings, including disabling jsonschema for production admission.
friction: the only remaining named test skip after npm ci is the existing unconditional live OpenAI acceptance placeholder. It is not part of package reuse proof and is not a demonstrated external API acceptance.
