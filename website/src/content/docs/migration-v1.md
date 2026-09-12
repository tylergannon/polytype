---
title: Migrate to v1
description: Move a go-gen-jsonschema or polytype prerelease integration onto the stable v1 API and JSON contract.
---

V1 changes the module identity, authoring API, field-presence rules, enum and
union discovery, and generated-output contract. Migrate the source declarations
first, regenerate once, and review every generated schema and codec diff before
updating consumers.

## Install the stable module and tool

Replace imports of `github.com/tylergannon/go-gen-jsonschema` with
`github.com/tylergannon/polytype`. Pin the generator through the Go 1.27 tool
directive:

```bash
go get -tool github.com/tylergannon/polytype/polytype@v1.0.0
go mod tidy
```

Replace `go install .../gen-jsonschema`, PATH-based `gen-jsonschema` commands,
and custom `gen/main.go` wrappers with `go tool polytype`:

```go
//go:generate go tool polytype --validate
```

Keep one `//go:build jsonschema` registration file in each generated package.
The legacy `// +build jsonschema` line is no longer needed.

## Use fluent declarations

The old declaration helpers remain only where the API reference marks them
deprecated. New code uses `Declare` and typed `Field` references:

```go
var _ = polytype.Declare(Task.Schema).
    StringerEnum(polytype.Field[Task, Priority]("Priority"))
```

`NewJSONSchemaMethod`, `NewJSONSchemaFunc`, and their remaining `With*` options
still compile in 1.x, so they can be migrated gradually. APIs removed during
the prerelease cycle need the replacements below.

## Make absence and null explicit

`jsonschema:"optional"` has no effect in v1. Use `Optional[T]` for an absent or
present non-null property, and include `json:",omitzero"`:

```go
Nickname polytype.Optional[string] `json:"nickname,omitzero"`
```

Use `Nullable[T]` for a required property whose value may be JSON null:

```go
ExpiresAt polytype.Nullable[time.Time] `json:"expiresAt"`
```

Ordinary fields are required and non-null. Direct pointer fields and ordinary
`omitempty`/`omitzero` fields are rejected because their Go encoding can
violate that schema. Replace them with the wrapper that states the intended
wire behavior. Validate external bytes with generated `ValidateJSON` before
decoding when required-key presence matters.

## Move enum declarations onto the type

`NewEnumType`, `.Enum`, and `WithEnum` are removed. Mark a named type as an enum
beside its typed constants:

```go
type Priority int

const (
    PriorityLow Priority = iota
    PriorityHigh
)

func (Priority) enum() {}
```

The marker selects value mode everywhere the type appears. A field-level
`.StringerEnum(Field[Owner, Priority]("Priority"))` selects constant-name string
encoding for that field. Renaming a constant changes that field's wire value.

## Replace explicit interface membership with sealed unions

`NewInterfaceImpl`, `.Interface`, `WithInterface*`, `Impl`, and `Discriminator`
are removed. Put an unexported method directly on the interface and each
same-package struct variant:

```go
type Event interface{ isEvent() }

type Created struct {
    ID string `json:"id"`
}

func (Created) isEvent() {}
```

The discriminator property defaults to `"type"`, and values default to
PascalCase concrete Go type names. Declare a different property or supported
inflection once in the interface's package:

```go
var _ = polytype.SealedUnion[Event]("kind", polytype.Snake)
```

Changing a variant type name, inflector, or qualifying implementation changes
the wire contract. Review the generated schema and TypeScript diffs.

## Remove generated YAML use

V1 generates JSON methods and validation only. Remove `--formats`,
`codegen.YAML`, calls to generated `ValidateYAML`/`UnmarshalYAML`, and generated
yaml/v4 requirements. Applications accepting YAML convert it to a generic JSON
value with their chosen library, marshal that value to JSON, then call
`ValidateJSON` and decode with `encoding/json`.

## Adopt generated-output ownership

Commit `jsonschema_gen.go`, the whole generated `jsonschema/` directory, and any
requested TypeScript or devalue output. The `.sum` sidecar marks a schema as
generator-owned. Removing or renaming a registration prunes an unchanged stale
schema pair; an edited orphan is preserved and reported.

One CLI run owns one TypeScript output directory. Use distinct directories for
different target packages, or use `grammar` plus `typescript.Generate` to lower
several package roots into one declaration graph.

Regeneration without `--validate` refuses to remove existing generated
validation methods. Keep `--validate`, or use `--force` when removal is
intentional.

## Verify the migrated package

```bash
go generate ./...
go mod tidy
go test ./...
JSONSCHEMA_NO_CHANGES=1 go generate ./...
test -z "$(git status --porcelain)"
```

Review changes to discriminator values, enum strings, required properties,
nullability, generated method ownership, and committed output files before
merging the migration.
