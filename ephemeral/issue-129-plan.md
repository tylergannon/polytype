# Issue #129 plan

Authority: GitHub issue #129 (five defects, acceptance criteria per defect).
User constraints: no new proof machinery; new fixture examples for the issue's
exact inputs; broken examples reproducing the reported breakage; independent
adversarial review to consensus.

## Decisions

1. **Defect 1 — Compose in a declaration file: reject it.** A top-level
   `polytype.X(...)` call that is not a declaration marker (`Compose`, or any
   other non-marker function) becomes a scanner error naming the file:line:col
   of the call, replacing the stdout `Unsupported MarkerFunction` line and the
   silent skip. Scanning precedes every write, so the failed run creates and
   modifies nothing. This is the resolution the checkbox criteria describe, and
   matches the recorded intent that declaration files list markers individually
   (`ephemeral/worklog/202609112010-issue-91-orphan-pruning.md`). Compose godoc
   and README say so.

2. **Programmatic runs stop interpreting source root markers.**
   `NewProgrammatic` already discards scanned roots; parsing them first is what
   makes `codegen.Gen` fail on declaration files. A programmatic scan (and every
   dependency-package scan, whose roots are never used) reads only
   `SealedUnion` markers, which remain type-level settings shared by every
   output (config still overrides). Root markers, `Compose`, and unknown calls
   are ignored there.

3. **Defect 2 — no-arg `Declare[T]()` in a declaration file.** The scanner
   takes the receiver from the type argument and records no entrypoint. CLI
   meaning, per the `Declare` godoc ("does not imply JSON Schema or an accessor
   method"): the root gets the CLI's always-on Go JSON codecs and, with
   `--typescript`, TypeScript; it gets no schema file and no accessor. Roots
   with an entrypoint keep today's behavior. `--validate` with an
   entrypoint-less root, and provider/RenderProviders chain rules on one, are
   errors naming the type (same rules codegen already enforces). The Go file
   embeds `jsonschema/` only when some root has a schema, so an
   entrypoint-less-only run never writes an embed of an empty directory.
   codegen's no-output error names the declared types.

4. **Defect 3 — recursion diagnostic.** Cycle detection returns a typed error;
   the schema-mapping loop converts it once, at the root, into the issue's
   suggested wording: JSON Schema cannot express the recursive type (with
   position), other outputs support it, use `codegen.GoJSON()` or a
   declaration-file `Declare[Root]()` without an entrypoint. No
   `rendering struct field:` prefix chain.

5. **Defect 4 — spec row.** Split `docs/spec/v1.md` recursive types into JSON
   Schema (excluded) and Go JSON codecs/TypeScript/devalue (supported), linking
   `codegen/testdata/recursive` and its tests.

6. **Defect 5 — cosmetics.** The generated Go file is `jsonschema_gen.go` when
   it embeds schemas, otherwise `polytype_gen.go`; writing one removes the other
   if it carries the polytype generated header, so upgrades and mode switches
   never leave duplicate methods. Generated-file discovery knows both names.
   `errNoDiscriminator` is removed; the default-discriminator branch reports its
   own property name like the declared-discriminator branch already does.

## Proof (existing machinery only)

- New fixture module `codegen/testdata/recursive_declarations`: the issue's
  30-line types in three packages — `noarg` (documented no-arg form plus a
  3-deep round-trip test), `compose` (the issue's broken Compose file), and
  `entrypoint` (`Declare(Tree.Schema)` for a recursive type) — plus the issue's
  `gen` program (OUT switch, TARGET switch).
- CLI e2e tests (polytype package, built binary, temp copy): Compose fails with
  position, writes nothing, package still builds and is byte-identical; no-arg
  generates `polytype_gen.go`, round-trips, emits TypeScript; `--validate`
  names the type; entrypoint shows the new diagnostic.
- codegen e2e tests (`copyNamedFixture`): gojson/ts/devalue succeed with each
  declaration file present; schema shows the new diagnostic once; codec file
  name and discriminator text; no-output error names the type.
- Unit tests for scanner and builder behavior; regenerate every committed
  generated file and golden for the template change.
