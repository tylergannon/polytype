package grammar_test

import (
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/grammar"
	"github.com/tylergannon/polytype/internal/builder"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// The fixture package declares no //go:build jsonschema file and no Declare
// marker: a caller lowering its own roots must not be required to register
// them first.
const holderSource = `package fixture

import "example.com/grammarfixture/todo"

// Holder names the shapes the test lowers.
type Holder struct {
	Todos []todo.Todo
}
`

const todoSource = `package todo

import "time"

// Priority ranks a todo.
type Priority int

const (
	PriorityLow Priority = iota
	PriorityHigh
)

func (Priority) enum() {}

// Todo is one item.
type Todo struct {
	Title    string    ` + "`json:\"title\"`" + `
	Priority Priority  ` + "`json:\"priority\"`" + `
	Due      time.Time ` + "`json:\"due\"`" + `
	Tags     []string  ` + "`json:\"tags\"`" + `
}
`

func TestLowerAnonymousAndCrossPackageRoots(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"fixture.go":   holderSource,
		"todo/todo.go": todoSource,
	})
	pkg, err := grammar.Load(dir)
	require.NoError(t, err)

	// []todo.Todo comes from a package that no marker ever seeded, so Lower
	// must load it on demand.
	roots := []grammar.Root{
		{Type: fieldType(t, pkg, "Holder", 0)},
		{Type: types.Typ[types.String]},
		{Type: types.NewArray(types.Typ[types.Int], 3)},
	}
	defs, nodes, err := pkg.Lower(roots)
	require.NoError(t, err)
	require.NoError(t, defs.Validate())
	require.Len(t, nodes, 3)

	todoName := typegrammar.Name{PackagePath: "example.com/grammarfixture/todo", Name: "Todo"}
	slice, ok := nodes[0].(*typegrammar.Slice)
	require.True(t, ok)
	require.Equal(t, &typegrammar.Ref{Target: todoName}, slice.Element)
	require.Equal(t, &typegrammar.Scalar{Kind: typegrammar.String}, nodes[1])
	require.Equal(t, &typegrammar.Array{Length: 3, Element: &typegrammar.Scalar{Kind: typegrammar.Int}}, nodes[2])

	todo := requireDefinition(t, defs, todoName)
	object, ok := todo.Type.(*typegrammar.Object)
	require.True(t, ok)
	require.Len(t, object.Fields, 4)
	require.Equal(t, &typegrammar.Required{Type: &typegrammar.Time{}}, object.Fields[2].Value)
	// The enum's members came from the dst lowering, which go/types alone
	// could not have supplied.
	requireDefinition(t, defs, typegrammar.Name{PackagePath: todoName.PackagePath, Name: "Priority"})
}

// A shape the field lowering refuses must be refused by the root bridge in the
// same words, so a caller cannot tell which path found it.
func TestRefusedRootsMatchFieldLoweringText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape string
	}{
		{"map", "map[string]int"},
		{"anonymous_interface", "interface{ Do() }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fieldErr := lowerField(t, tc.shape)
			rootErr := lowerRoot(t, tc.shape)
			require.Error(t, fieldErr)
			require.Error(t, rootErr)
			require.Equal(t, refusal(fieldErr), refusal(rootErr))
		})
	}
}

// The remaining refusals cannot be compared against the builder: the scanner
// rejects these shapes in a struct field before any lowering runs, in its own
// older wording. The root bridge still has to speak the grammar's sentence,
// which it shares with the field lowering by construction.
func TestRefusedRootsUseGrammarWording(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape string
		want  string
	}{
		{"chan", "chan int", "channels are outside the static type grammar"},
		{"func", "func() error", "functions are outside the static type grammar"},
		{"presence_wrapper", "polytype.Optional[int]", "presence wrappers are valid only as complete direct named fields"},
		{"sealed_interface", "Shape", "registered interface example.com/grammarfixture.Shape is valid only as a configured direct field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := lowerRoot(t, tc.shape)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// A shape both paths accept must lower to the same node, not merely avoid an
// error: the bridge hands named types to the dst lowering rather than
// reimplementing it.
func TestAcceptedRootMatchesFieldLowering(t *testing.T) {
	source := shapeFixture("[]*Meta")
	dir := writeFixture(t, map[string]string{"fixture.go": source})
	pkg, err := grammar.Load(dir)
	require.NoError(t, err)
	_, nodes, err := pkg.Lower([]grammar.Root{{Type: fieldType(t, pkg, "Holder", 0)}})
	require.NoError(t, err)

	defs, _, err := pkg.Lower([]grammar.Root{{Type: namedType(t, pkg, "Holder")}})
	require.NoError(t, err)
	holder := requireDefinition(t, defs, typegrammar.Name{PackagePath: "example.com/grammarfixture", Name: "Holder"})
	object, ok := holder.Type.(*typegrammar.Object)
	require.True(t, ok)
	required, ok := object.Fields[0].Value.(*typegrammar.Required)
	require.True(t, ok)
	require.Equal(t, required.Type, nodes[0])
}

// A root node is reachable from no definition, so validating only the
// definitions would return a shape the grammar excludes. []byte is the case:
// Validate refuses a byte-like slice wherever a definition reaches one, and
// must refuse it as a root in the same words.
func TestRefusedAnonymousRootPassesAdmissionBoundary(t *testing.T) {
	dir := writeFixture(t, map[string]string{"fixture.go": shapeFixture("int")})
	pkg, err := grammar.Load(dir)
	require.NoError(t, err)

	_, _, err = pkg.Lower([]grammar.Root{{Type: types.NewSlice(types.Typ[types.Byte])}})
	var grammarErr *typegrammar.Error
	require.ErrorAs(t, err, &grammarErr)
	require.Equal(t, "roots[0]", grammarErr.Path)
	require.Equal(t, "byte-like slices have a base64 wire mapping outside this grammar", grammarErr.Message)
}

func TestLowerRejectsUnloadablePackage(t *testing.T) {
	_, err := grammar.Load(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}

// shapeFixture declares SHAPE both as a struct field of a registered root (the
// builder's path) and as a plain type the root bridge can be handed.
func shapeFixture(shape string) string {
	return fmt.Sprintf(`//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Shape interface{ shape() }

type Circle struct {
	Radius float64 %[2]s
}

func (Circle) shape() {}

type Meta struct {
	Code int %[3]s
}

type Holder struct {
	F %[1]s %[4]s
}

func (Holder) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Holder.Schema)
`, shape, "`json:\"radius\"`", "`json:\"code\"`", "`json:\"f\"`")
}

func lowerField(t *testing.T, shape string) error {
	t.Helper()
	dir := writeFixture(t, map[string]string{"fixture.go": shapeFixture(shape)})
	pkgs, err := syntax.Load(dir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	// Select no root for JSON-schema mapping: that mapping refuses several of
	// these shapes earlier and in its own older wording, and the claim under
	// test is about the type-grammar lowering, which still sees every
	// registration.
	b, err := builder.NewForTypes(pkgs[0], []string{"__no_such_root__"})
	if err != nil {
		return err
	}
	_, err = b.TypeDefinitions()
	return err
}

func lowerRoot(t *testing.T, shape string) error {
	t.Helper()
	dir := writeFixture(t, map[string]string{"fixture.go": shapeFixture("int"), "root.go": rootSource(shape)})
	pkg, err := grammar.Load(dir)
	if err != nil {
		return err
	}
	_, _, err = pkg.Lower([]grammar.Root{{Type: fieldType(t, pkg, "RootHolder", 0), Position: token.Position{Filename: "root.go", Line: 1}}})
	return err
}

func rootSource(shape string) string {
	return fmt.Sprintf(`//go:build jsonschema

package fixture

import "github.com/tylergannon/polytype"

var _ = polytype.Optional[int]{}

type RootHolder struct {
	F %s
}
`, shape)
}

// refusal strips the leading wrapper context and the trailing source position,
// which necessarily differ between the two paths, leaving the sentence itself.
func refusal(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	if i := strings.LastIndex(msg, " at "); i >= 0 {
		msg = msg[:i]
	}
	return msg
}

func fieldType(t *testing.T, pkg *grammar.Package, typeName string, index int) types.Type {
	t.Helper()
	object, ok := namedType(t, pkg, typeName).Underlying().(*types.Struct)
	require.True(t, ok)
	return object.Field(index).Type()
}

func namedType(t *testing.T, pkg *grammar.Package, typeName string) types.Type {
	t.Helper()
	obj := pkg.Types().Scope().Lookup(typeName)
	require.NotNil(t, obj, typeName)
	return obj.Type()
}

func requireDefinition(t *testing.T, defs typegrammar.Definitions, name typegrammar.Name) typegrammar.Definition {
	t.Helper()
	for _, def := range defs {
		if def.Name == name {
			return def
		}
	}
	require.FailNow(t, "missing definition", name.String())
	return typegrammar.Definition{}
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	dir := t.TempDir()
	module := fmt.Sprintf("module example.com/grammarfixture\n\ngo 1.27\n\nrequire github.com/tylergannon/polytype v0.0.0\nreplace github.com/tylergannon/polytype => %s\n", root)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o644))
	for name, source := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	}
	return dir
}
