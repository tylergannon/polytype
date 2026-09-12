package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeMultiFileFixture is writeTypeGrammarFixture's multi-file sibling, for
// cases that need types and jsonschema-tagged registrations split across
// files the way real registration code is (types.go untagged, schema.go
// //go:build jsonschema).
func writeMultiFileFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	dir := t.TempDir()
	module := fmt.Sprintf("module example.com/typegrammarfixture\n\ngo 1.27\n\nrequire github.com/tylergannon/polytype v0.0.0\nreplace github.com/tylergannon/polytype => %s\n", root)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o644))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	return dir
}

// TestValidateRejectsFreeFunctionPointerRoot proves that --validate fails
// fast, with an actionable error, instead of silently succeeding while
// omitting ValidateJSON for a free-function schema root whose type can't
// have a method declared on it (pointer/interface underlying type). Before
// this check, generation would succeed and simply produce no ValidateJSON
// for that type -- a silent partial result.
func TestValidateRejectsFreeFunctionPointerRoot(t *testing.T) {
	dir := writeTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type PointerRoot *int

func PointerRootSchema(PointerRoot) json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(PointerRootSchema)
`)

	err := Run(BuilderArgs{TargetDir: dir, Validate: true})
	require.ErrorContains(t, err, "--validate cannot generate ValidateJSON for PointerRoot")
}

func TestSchemaGenerationRejectsImplicitPointerAndOmissionSemantics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		wants  []string
	}{
		{name: "bare pointer", source: "type Root struct { Child *string `json:\"child\"` }", wants: []string{"bare pointer field Root.Child", "use polytype.Nullable[T]"}},
		{name: "embedded bare pointer", source: "type Child struct{}\ntype Root struct { *Child }", wants: []string{"bare pointer field Root.Child", "use polytype.Nullable[T]"}},
		{name: "omitempty", source: "type Root struct { Value string `json:\"value,omitempty\"` }", wants: []string{`ordinary field Root.Value uses json:",omitempty"`, "use polytype.Optional[T]"}},
		{name: "embedded omitempty", source: "type Child struct{}\ntype Root struct { Child `json:\",omitempty\"` }", wants: []string{`ordinary field Root.Child uses json:",omitempty"`, "use polytype.Optional[T]"}},
		{name: "omitzero", source: "type Root struct { Value string `json:\"value,omitzero\"` }", wants: []string{`ordinary field Root.Value uses json:",omitzero"`, "use polytype.Optional[T]"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := writeMultiFileFixture(t, map[string]string{
				"types.go": "package fixture\n\n" + test.source + "\n",
				"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Root) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Root.Schema)
`,
			})

			err := Run(BuilderArgs{TargetDir: dir})
			for _, want := range test.wants {
				require.ErrorContains(t, err, want)
			}
		})
	}
}

// TestFreeFunctionRootForRegisteredInterfaceGeneratesFreeFunction proves
// that a free-function schema root for a type registered via the legacy
// NewInterfaceImpl (recorded in Scan.Interfaces, not Scan.LocalNamedTypes)
// is correctly classified as needing a free function, not a method: before
// this fix, hasInvalidMethodReceiverBase only consulted LocalNamedTypes, so
// this exact shape would be misrouted into SchemaMethods() and generate an
// uncompilable `func (Value) ValueSchema()` (Go forbids an interface
// receiver base). This is a fast, source-level check of the classification
// only; TestInterfaceFuncTypeSchemaCallable in
// testfixtures/entrypoints/entrypoints_test.go is the real compile-and-call
// proof, run through TestBasic's full go-build-and-test harness.
func TestFreeFunctionRootForRegisteredInterfaceGeneratesFreeFunction(t *testing.T) {
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go": `package fixture

type Value interface{ value() }

type First struct {
	Name string ` + "`json:\"name\"`" + `
}

func (First) value() {}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func ValueSchema(Value) json.RawMessage { panic("not implemented") }

var (
	_ = polytype.NewInterfaceImpl[Value](First{})
	_ = polytype.Declare(ValueSchema)
)
`,
	})

	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))

	generated, err := os.ReadFile(filepath.Join(dir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(generated), "func ValueSchema(Value) json.RawMessage {")
	require.NotContains(t, string(generated), "func (Value) ValueSchema()")
}

// TestValidateRejectsFreeFunctionInterfaceRoot proves the same --validate
// rejection fires for a registered-interface free-function root, not just a
// named-pointer one, since hasInvalidMethodReceiverBase classifies both.
func TestValidateRejectsFreeFunctionInterfaceRoot(t *testing.T) {
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go": `package fixture

type Value interface{ value() }

type First struct {
	Name string ` + "`json:\"name\"`" + `
}

func (First) value() {}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func ValueSchema(Value) json.RawMessage { panic("not implemented") }

var (
	_ = polytype.NewInterfaceImpl[Value](First{})
	_ = polytype.Declare(ValueSchema)
)
`,
	})

	err := Run(BuilderArgs{TargetDir: dir, Validate: true})
	require.ErrorContains(t, err, "--validate cannot generate ValidateJSON for Value")
}

// TestRenderProvidersRejectsFreeFunctionPointerRoot proves that
// RenderProviders() fails fast, with an actionable error, for a
// free-function root whose type can't have a method (so no RenderedSchema()
// could ever be generated for it) instead of silently writing a
// `.json.tmpl` that nothing can execute.
func TestRenderProvidersRejectsFreeFunctionPointerRoot(t *testing.T) {
	dir := writeTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type PointerRoot *int

func PointerRootSchema(PointerRoot) json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(PointerRootSchema).RenderProviders()
`)

	err := Run(BuilderArgs{TargetDir: dir})
	require.ErrorContains(t, err, "PointerRoot: RenderProviders() is not supported")
}

// TestBuilderRejectsInvalidReceiverPointerRoot proves that
// NewJSONSchemaBuilder (whose stub takes no arguments) fails fast for a
// type whose underlying type is a pointer or interface, instead of being
// misrouted through the free-function codegen path and emitted with the
// wrong (one-argument) signature -- which would either not match the
// original zero-argument stub's callers or, if the same builder function
// is reused for two such types, collide as a duplicate declaration.
func TestBuilderRejectsInvalidReceiverPointerRoot(t *testing.T) {
	dir := writeTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type PointerRoot *int

func BuildSchema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaBuilder[PointerRoot](BuildSchema)
`)

	err := Run(BuilderArgs{TargetDir: dir})
	require.ErrorContains(t, err, "PointerRoot: NewJSONSchemaBuilder is not supported")
}

// TestFreeFunctionRootForForwardingPointerTypeGeneratesFreeFunction proves
// that a type defined in terms of another named pointer type (type Q P,
// where P is itself a pointer) is classified as an invalid method receiver
// base, not just a type declared directly as a pointer (type Q *int). The
// classifier resolves through go/types' Underlying(), which follows
// arbitrary chains of named-type indirection, rather than pattern-matching
// only the immediate declaration's AST expression.
func TestFreeFunctionRootForForwardingPointerTypeGeneratesFreeFunction(t *testing.T) {
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go": `package fixture

type P *int
type Q P
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func QSchema(Q) json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(QSchema)
`,
	})

	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))

	generated, err := os.ReadFile(filepath.Join(dir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(generated), "func QSchema(Q) json.RawMessage {")
	require.NotContains(t, string(generated), "func (Q) QSchema()")
}
