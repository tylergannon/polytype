package codegen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tylergannon/polytype/devalue/codegen"
	"github.com/tylergannon/polytype/grammar"
	"github.com/tylergannon/polytype/internal/testutils"
	"github.com/tylergannon/polytype/typegrammar"
)

// TestGeneratedCodecsCompileAndRun copies the fixture module into a temporary
// directory, lowers it, generates codecs into the sibling package that already
// holds the assertions, and then builds and runs that module. Compiling in a
// package other than the one declaring the types is the point: it proves the
// emitted code touches nothing unexported.
func TestGeneratedCodecsCompileAndRun(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "fixture")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := testutils.CopyDir("testdata/fixture", dir); err != nil {
		t.Fatal(err)
	}
	// The copy is outside the repository, so the replace directive has to be
	// rewritten to an absolute path before the module can resolve polytype.
	gomod := filepath.Join(dir, "go.mod")
	contents, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.Replace(string(contents), "=> ../../../../", "=> "+repoRoot, 1)
	if rewritten == string(contents) {
		t.Fatalf("fixture go.mod has no replace directive to rewrite:\n%s", contents)
	}
	if err := os.WriteFile(gomod, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}

	pkg, err := grammar.Load(filepath.Join(dir, "model"))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	scope := pkg.Types().Scope()
	envelope := scope.Lookup("Envelope").Type()
	detail := scope.Lookup("Detail").Type()
	defs, roots, err := pkg.Lower([]grammar.Root{
		{Type: types.NewSlice(envelope), Position: token.Position{Filename: "roots", Line: 1}},
		{Type: detail, Position: token.Position{Filename: "roots", Line: 2}},
	})
	if err != nil {
		t.Fatalf("lower fixture: %v", err)
	}
	assertCoversEveryNodeKind(t, defs)

	source, err := codegen.Generate(defs, roots, codegen.Options{
		PackageName: "codec",
		ImportPath:  "polytypedevaluefixture/codec",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codec", "codec_gen.go"), source, 0o644); err != nil {
		t.Fatal(err)
	}

	run(t, dir, "mod", "tidy")
	run(t, dir, "build", "./...")
	run(t, dir, "test", "./...")
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	exit, stdout, stderr, err := testutils.RunCommand("go", dir, args...)
	if err != nil {
		t.Fatalf("go %s: %v", strings.Join(args, " "), err)
	}
	if exit != 0 {
		t.Fatalf("go %s failed:\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), stdout, stderr)
	}
}

// assertCoversEveryNodeKind fails if the fixture stops exercising a
// constructor, which would silently narrow what the compile-and-run proof
// covers.
func assertCoversEveryNodeKind(t *testing.T, defs typegrammar.Definitions) {
	t.Helper()
	seen := map[string]bool{}
	var walkValue func(typegrammar.FieldValue)
	var walk func(typegrammar.Type)
	walk = func(node typegrammar.Type) {
		switch n := node.(type) {
		case *typegrammar.Scalar:
			seen["Scalar/"+string(n.Kind)] = true
		case *typegrammar.Time:
			seen["Time"] = true
		case *typegrammar.Enum:
			seen["Enum"] = true
			if n.Mode == typegrammar.EnumNames {
				seen["Enum/names"] = true
			}
		case *typegrammar.Object:
			seen["Object"] = true
			for _, field := range n.Fields {
				walkValue(field.Value)
			}
		case *typegrammar.Pointer:
			seen["Pointer"] = true
			walk(n.Element)
		case *typegrammar.Slice:
			seen["Slice"] = true
			walk(n.Element)
		case *typegrammar.Array:
			seen["Array"] = true
			walk(n.Element)
		case *typegrammar.Ref:
			seen["Ref"] = true
		}
	}
	walkValue = func(value typegrammar.FieldValue) {
		switch n := value.(type) {
		case *typegrammar.Required:
			seen["Required"] = true
			// An inline object is a distinct backend path: it is reached
			// through selectors on the parent because its type cannot be
			// spelled. Object alone is satisfied by the named definitions.
			if _, inline := n.Type.(*typegrammar.Object); inline {
				seen["Object/inline"] = true
			}
			walk(n.Type)
		case *typegrammar.Optional:
			seen["Optional"] = true
			walk(n.Type)
		case *typegrammar.Nullable:
			seen["Nullable"] = true
			walk(n.Type)
		case *typegrammar.Union:
			seen["Union"] = true
		case *typegrammar.OptionalUnion:
			seen["OptionalUnion"] = true
		case *typegrammar.UnionSlice:
			seen["UnionSlice"] = true
		}
	}
	for _, def := range defs {
		walk(def.Type)
	}
	want := []string{
		"Time", "Enum", "Enum/names", "Object", "Object/inline", "Pointer", "Slice", "Array", "Ref",
		"Required", "Optional", "Nullable", "Union", "OptionalUnion", "UnionSlice",
		"Scalar/bool", "Scalar/string", "Scalar/int", "Scalar/int8", "Scalar/int16",
		"Scalar/int32", "Scalar/int64", "Scalar/uint", "Scalar/uint8", "Scalar/uint16",
		"Scalar/uint32", "Scalar/uint64", "Scalar/float32", "Scalar/float64",
	}
	for _, kind := range want {
		if !seen[kind] {
			t.Errorf("fixture no longer covers %s", kind)
		}
	}
}

// TestSanitizedNameCollisionInOnePackage covers the collision the TypeScript
// backend already admits as a projection edge case: 雪 sanitizes to _u96EA_,
// which is itself a legal Go type name, so two definitions in one package can
// reach the same base identifier. Qualifying by package path cannot separate
// them, so each must take a distinct suffix or Generate emits duplicate
// declarations.
func TestSanitizedNameCollisionInOnePackage(t *testing.T) {
	t.Parallel()

	object := func() *typegrammar.Object {
		return &typegrammar.Object{Fields: []typegrammar.Field{{
			GoName: "Text", JSONName: "text", Value: &typegrammar.Required{Type: &typegrammar.Scalar{Kind: typegrammar.String}},
		}}}
	}
	defs := typegrammar.Definitions{
		{Name: typegrammar.Name{PackagePath: "example.com/model", Name: "雪"}, Type: object()},
		{Name: typegrammar.Name{PackagePath: "example.com/model", Name: "_u96EA_"}, Type: object()},
	}
	source, err := codegen.Generate(defs, nil, codegen.Options{PackageName: "codec", ImportPath: "example.com/codec"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Parsing is the real assertion: a duplicate func declaration is a
	// redeclaration the type checker rejects, and go/parser gives us the
	// names without building the fixture module.
	file, err := parser.ParseFile(token.NewFileSet(), "codec_gen.go", source, 0)
	if err != nil {
		t.Fatalf("parse generated source: %v\n%s", err, source)
	}
	seen := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		if seen[fn.Name.Name] {
			t.Errorf("generated source declares %s twice", fn.Name.Name)
		}
		seen[fn.Name.Name] = true
	}
	// Both definitions still get a full family; neither was dropped.
	for _, prefix := range []string{"Encode", "Decode", "Stringify", "Parse"} {
		var count int
		for name := range seen {
			if strings.HasPrefix(name, prefix) {
				count++
			}
		}
		if count != len(defs) {
			t.Errorf("%s functions = %d, want %d: %v", prefix, count, len(defs), slices.Sorted(maps.Keys(seen)))
		}
	}
}

func TestGenerateRefusals(t *testing.T) {
	t.Parallel()
	valid := typegrammar.Definitions{{
		Name: typegrammar.Name{PackagePath: "example.com/model", Name: "Note"},
		Type: &typegrammar.Object{},
	}}
	options := codegen.Options{PackageName: "codec", ImportPath: "example.com/codec"}

	for _, tc := range []struct {
		name    string
		defs    typegrammar.Definitions
		roots   []typegrammar.Type
		options codegen.Options
		want    string
	}{
		{
			name: "package name is required",
			defs: valid,
			want: "Options.PackageName is required",
		},
		{
			name:    "nil root",
			defs:    valid,
			roots:   []typegrammar.Type{nil},
			options: options,
			want:    "root 0 is nil",
		},
		{
			name:    "grammar admission applies to roots",
			defs:    valid,
			roots:   []typegrammar.Type{&typegrammar.Scalar{Kind: "complex128"}},
			options: options,
			want:    "roots[0]",
		},
		{
			name: "anonymous struct under a slice",
			defs: typegrammar.Definitions{{
				Name: typegrammar.Name{PackagePath: "example.com/model", Name: "Note"},
				Type: &typegrammar.Object{Fields: []typegrammar.Field{{
					GoName: "Lines", JSONName: "lines", Value: &typegrammar.Required{Type: &typegrammar.Slice{
						Element: &typegrammar.Object{Fields: []typegrammar.Field{{
							GoName: "Text", JSONName: "text", Value: &typegrammar.Required{Type: &typegrammar.Scalar{Kind: typegrammar.String}},
						}}},
					}},
				}}},
			}},
			options: options,
			want:    "example.com/model.Note.Lines.items: an anonymous struct type",
		},
		{
			name: "anonymous struct root",
			defs: valid,
			roots: []typegrammar.Type{&typegrammar.Object{Fields: []typegrammar.Field{{
				GoName: "Text", JSONName: "text", Value: &typegrammar.Required{Type: &typegrammar.Scalar{Kind: typegrammar.String}},
			}}}},
			options: options,
			want:    "anonymous struct type",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := codegen.Generate(tc.defs, tc.roots, tc.options)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}
