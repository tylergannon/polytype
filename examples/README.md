# polytype Examples

This directory contains comprehensive examples demonstrating all features of polytype. Each subdirectory showcases different capabilities of the tool.

## Quick Start

Each example contains:
- `types.go` - Go type definitions
- `schema.go` - Schema registration with options
- Checked-in `jsonschema_gen.go` accessors and `jsonschema/` embed inputs

Generated artifacts are checked in so a fresh clone compiles and tests without
a bootstrap step. Regenerate them after changing an example or the generator;
the directives run the generator from this checkout, so no separately installed
binary is required.

To generate schemas:
```bash
cd [example-directory]
go generate
```

## Examples Overview

### Basic Examples

#### `basictypes/`
Demonstrates fundamental schema generation for simple types.
- Basic Go types (int, string, float, bool)
- Simple structs with documentation
- Nested structs
- Optional fields with `omitempty`

#### `structs/`
Complex struct examples with real-world patterns.
- Embedded structs
- Time.Time fields with RFC3339 format guidance
- Nested struct composition
- Field documentation
- Comprehensive test coverage

#### `indirecttypes/`
Shows handling of type aliases and indirection.
- Type aliases
- Pointer types
- Custom type definitions

### Enum Examples

#### `enums/`
String-based enums with explicit values.
- String-typed constants
- Auto-detection of string enum values
- Multiple enums in one package

#### `enums_stringmode/`
Alternative enum handling with string mode.
- String representation of numeric enums
- `.StringerEnum` field configuration

#### `stringer_enums/`
Integer enums with fmt.Stringer implementation.
- Iota-based constants
- Custom String() methods
- Enum validation

#### `iota_global/`
Integer iota enum example.
- Package-level iota constants marked with `func (T) enum()`
- Integer enum values

### Interface & Union Types

#### `uniontypes/`
Discriminated union types using sealed Go interfaces.
- Inferred membership from the sealing method
- Value and pointer variants
- Direct scalar and slice fields

#### `interfaces_options/`
Minimal sealed interface with a custom discriminator.
- Inferred union membership
- `SealedUnion[IFace]("!kind")` discriminator declaration

#### `sealed_interface_slices/`
Direct slices of registered interface unions.
- Array schemas with the union under `items.anyOf`
- Mixed value and pointer implementations
- Transactional element decoding with indexed errors

### Provider & Template Examples

#### `providers_rendering/`
Template-based schema generation with runtime providers.
- Field-level schema providers
- Runtime template rendering
- Three provider types:
  - `.Accessor` - No-arg struct method
  - `.Method` - Struct method with field arg
  - `.Function` - Free function provider

#### `template_rendering/`
Basic template rendering example.
- Template-based schema generation
- Static templates

#### `self_contained/`
Self-contained schema generation example.
- Complete example in single package
- No external dependencies

### Test & Configuration

#### `test_options/`
Various configuration options and edge cases.
- Different registration patterns
- Configuration options testing

## Key Patterns

### Basic Schema Registration
```go
func (MyType) Schema() json.RawMessage { 
    panic("not implemented") 
}
var _ = polytype.Declare(MyType.Schema)
```

### Enum Registration
```go
type Status string
func (Status) enum() {}
const (
    StatusPending Status = "pending"
    StatusActive  Status = "active"
)
type Task struct{ Status Status }
func (Task) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Task.Schema)
```

### Sealed Interface/Union Types
```go
type Shape interface{ isShape() }        // sealed by the unexported method
type Circle struct{ /* fields */ }
func (Circle) isShape() {}               // value variant, wire value "Circle"
type Rectangle struct{ /* fields */ }
func (*Rectangle) isShape() {}           // pointer variant, wire value "Rectangle"
type Owner struct{ Shape Shape }
func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Owner.Schema)   // membership is inferred
var _ = polytype.SealedUnion[Shape]("kind") // optional; default property is "type"
```

### Provider-Based Schema Generation
```go
var _ = polytype.Declare(Example.Schema).
    Accessor(polytype.Field[Example, string]("A"), (Example).ASchema).
    Method(polytype.Field[Example, int]("B"), (Example).BSchema).
    Function(polytype.Field[Example, bool]("C"), BoolSchema).
    RenderProviders()
```

## Running All Examples

To test all examples:
```bash
for dir in */; do 
    echo "Testing $dir"
    (cd "$dir" && go generate)
done
```

## Notes

- All examples use build tags to separate schema registration from normal builds
- The `//go:build jsonschema` tag is used in `schema.go` files
- Generated code uses `//go:build !jsonschema` to exclude from schema generation
- External types like `time.Time` are handled with descriptive guidance for LLMs
