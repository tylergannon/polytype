# Current-main release-readiness probes

Assessed commit: 38f05b1 (also origin HEAD on 2026-09-11).
This nested module uses a local replace deliberately; it is current-source evidence,
not proof of public tag installation.

From the repository root:

```sh
go build -o ephemeral/release-readiness/polytype ./polytype
cd ephemeral/release-readiness
go mod tidy
./polytype --validate --typescript ts -target ./model
go mod tidy
go run ./cmd/probe
```

Observed output:

```text
zero-value JSON: {"note":null,"tags":null}
validation: jsonschema validation failed
- missing property 'title'
- at '/note': got null, want string
- at '/tags': got null, want array
```

The emitted TypeScript has required `note: string`, `tags: Array<string>`,
and `title: string`. This reproduces issue #100 without relying on the older RC9
report. Decide and enforce the admitted value domain; do not silently change the
wire contract in a v1 minor release.

To reproduce issue #102:

```sh
./polytype --typescript ts -target ./model
go run ./cmd/probe
```

Generation exits 0 without warning. The consumer then fails to compile because
`model.Todo.ValidateJSON` was removed. Restore the probe with:

```sh
./polytype --validate --typescript ts -target ./model
./polytype --validate --typescript ts -target ./model --no-changes
go test ./...
```

Documentation findings:

- `llms.txt` uses `StringerEnum(Task{}.LogLevel)` / `StringerEnum(Order{}.Priority)`
  despite the current FieldRef API, and calls the CLI the only path to schema,
  validation, and Go JSON outputs despite the new codegen package.
- Website API reference still shows `SealedUnion(discriminator string)` without
  inflection and lacks the new named inflectors.
- README's registration table and root godoc describe StringerEnum as comparing
  via fmt.Stringer, while actual documented enum behavior uses constant names.
- Website onboarding still uses relative release language and treats already
  completed transport proof as outstanding. YAML removal must be reflected across
  README, website, llms.txt, shipped skills, and compatibility guidance.

No implementation changes were made in this assessment.
