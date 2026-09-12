---
title: Validation and CI
description: Validate generated JSON and prevent committed schemas from drifting.
---

## Generate validation methods

Add `--validate` to the generation directive and a matching stub to the
build-tagged registration file:

```go
//go:generate go tool polytype --validate
```

```go
//go:build jsonschema

func (ToolInput) Schema() json.RawMessage     { panic("not implemented") }
func (ToolInput) ValidateJSON(_ []byte) error { panic("not implemented") }

var _ = polytype.Declare(ToolInput.Schema)
```

```bash
go generate ./...
go mod tidy
```

The generated `ValidateJSON([]byte) error` compiles the schema once at startup.
Validation covers required fields, types, unknown properties, enum membership,
and nested structure. Schema-validation failures can be inspected as
`*jsonschemav6.ValidationError`; malformed JSON may instead return a parsing
error.

If a later generation command omits `--validate`, polytype refuses to remove an
existing generated `ValidateJSON` method. Restore the flag, or pass `--force`
when removing validation is intentional.

```go
import (
    "errors"
    "log"

    jsonschemav6 "github.com/santhosh-tekuri/jsonschema/v6"
)

func validateToolInput(data []byte) error {
    if err := (ToolInput{}).ValidateJSON(data); err != nil {
        var validationErr *jsonschemav6.ValidationError
        if errors.As(err, &validationErr) {
            log.Printf("invalid field: %s", validationErr.InstanceLocation)
        }
        return err
    }
    return nil
}
```

## Fail CI on drift

`JSONSCHEMA_NO_CHANGES` flows through every `go generate` directive and makes
the generator fail without writing schema files or requested TypeScript output
when either would change. Generation can still update `jsonschema_gen.go` when
those artifacts are unchanged, so pair the command with a Git status check:

```yaml
- name: Check generated schemas and TypeScript declarations
  run: JSONSCHEMA_NO_CHANGES=1 go generate ./... && test -z "$(git status --porcelain)"
```

For repositories with generators that do not understand
`JSONSCHEMA_NO_CHANGES`, use the broader fallback:

```yaml
- name: Check all generated files
  run: go generate ./... && test -z "$(git status --porcelain)"
```

Run `go test ./...` after the drift check so tests execute against the same
generated state that will be committed.

## YAML input

polytype generates no YAML methods and takes no YAML dependency. Convert YAML
to a generic value with the library your application already uses, marshal that
value to JSON, then call `ValidateJSON` and decode with `encoding/json`.
Property names come from `json` tags.
