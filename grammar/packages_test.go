package grammar_test

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/grammar"
	"github.com/tylergannon/polytype/typegrammar"
	"github.com/tylergannon/polytype/typescript"
	"golang.org/x/tools/go/packages"
)

func packageConfig(dir string) *packages.Config {
	return &packages.Config{Dir: dir, Mode: grammar.LoadMode, BuildFlags: []string{"-tags=jsonschema"}}
}

func loadSnapshot(t testing.TB, cfg *packages.Config, patterns ...string) []*packages.Package {
	t.Helper()
	pkgs, err := packages.Load(cfg, patterns...)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)
	for _, pkg := range pkgs {
		require.Empty(t, pkg.Errors)
	}
	slices.SortFunc(pkgs, func(a, b *packages.Package) int { return cmp.Compare(a.PkgPath, b.PkgPath) })
	return pkgs
}

func forbidPackageLoads(t *testing.T) {
	t.Helper()
	// A driver invocation cannot succeed after this point. Check the tripwire
	// itself so a ignored environment variable cannot make the proof vacuous.
	t.Setenv("GOPACKAGESDRIVER", filepath.Join(t.TempDir(), "unexpected-package-load"))
	_, err := packages.Load(&packages.Config{}, ".")
	require.Error(t, err)
}

func lowerNamed(t testing.TB, pkg *grammar.Package, name string) (typegrammar.Definitions, []typegrammar.Type) {
	t.Helper()
	obj := pkg.Types().Scope().Lookup(name)
	require.NotNil(t, obj)
	defs, roots, err := pkg.Lower([]grammar.Root{{Type: obj.Type()}})
	require.NoError(t, err)
	return defs, roots
}

func requireProjectionParity(t *testing.T, ordinary, supplied typegrammar.Definitions) {
	t.Helper()
	want, err := typescript.Generate(ordinary, typescript.Options{})
	require.NoError(t, err)
	got, err := typescript.Generate(supplied, typescript.Options{})
	require.NoError(t, err)
	require.NotEmpty(t, got.Files)
	require.Equal(t, want.Files, got.Files)
}

func TestFromPackagesReusesRootAndDependencies(t *testing.T) {
	dir := writeFixture(t, map[string]string{"fixture.go": holderSource, "todo/todo.go": todoSource})
	cfg := packageConfig(dir)
	pkgs := loadSnapshot(t, cfg, "./...")
	ordinary, err := grammar.LoadWithConfig(cfg, ".")
	require.NoError(t, err)
	wantDefs, wantRoots := lowerNamed(t, ordinary, "Holder")
	forbidPackageLoads(t)

	supplied, err := grammar.FromPackages(cfg, pkgs[0], pkgs[1:]...)
	require.NoError(t, err)
	require.Same(t, pkgs[0].Types, supplied.Types())
	defs, roots := lowerNamed(t, supplied, "Holder")
	require.Equal(t, wantDefs, defs)
	require.Equal(t, wantRoots, roots)
	// This second root is dependency-owned, and still must not load again.
	todo := pkgs[0].Imports["example.com/grammarfixture/todo"]
	_, _, err = supplied.Lower([]grammar.Root{{Type: todo.Types.Scope().Lookup("Todo").Type()}})
	require.NoError(t, err)
	definition := requireDefinition(t, defs, typegrammar.Name{PackagePath: todo.PkgPath, Name: "Todo"})
	require.Equal(t, "Todo is one item.", definition.Description)
	require.Same(t, todo, pkgs[0].Imports[todo.PkgPath])
}

func TestFromPackagesMissingDependencyDoesNotReload(t *testing.T) {
	dir := writeFixture(t, map[string]string{"fixture.go": holderSource, "todo/todo.go": todoSource})
	cfg := packageConfig(dir)
	pkgs := loadSnapshot(t, cfg, ".") // dep has exported types, but no AST.
	forbidPackageLoads(t)
	pkg, err := grammar.FromPackages(cfg, pkgs[0])
	require.NoError(t, err)
	_, _, err = pkg.Lower([]grammar.Root{{Type: pkg.Types().Scope().Lookup("Holder").Type()}})
	require.ErrorContains(t, err, "package example.com/grammarfixture/todo needs type-checked source")
	// A missing AST is a clear requirement, never a hidden Go-command load.
	require.NotContains(t, err.Error(), "unexpected-package-load")
}

func TestFromPackagesKeepsCrossPackageUnionDeclaration(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"root.go": "package fixture\nimport \"example.com/grammarfixture/dep\"\ntype Root struct { Nested dep.Holder }\n",
		"dep/types.go": `package dep
type Holder struct { Shape Shape }
type Shape interface { shape() }
type Circle struct { Radius int }
func (Circle) shape() {}
`,
		"dep/schema.go": `//go:build jsonschema

package dep
import "github.com/tylergannon/polytype"
var _ = polytype.SealedUnion[Shape]("kind")
`,
	})
	cfg := packageConfig(dir)
	pkgs := loadSnapshot(t, cfg, "./...")
	ordinary, err := grammar.LoadWithConfig(cfg, ".")
	require.NoError(t, err)
	wantDefs, wantRoots := lowerNamed(t, ordinary, "Root")
	forbidPackageLoads(t)
	pkg, err := grammar.FromPackages(cfg, pkgs[0], pkgs[1:]...)
	require.NoError(t, err)
	defs, roots := lowerNamed(t, pkg, "Root")
	require.Equal(t, wantDefs, defs)
	require.Equal(t, wantRoots, roots)
	root := requireDefinition(t, defs, typegrammar.Name{PackagePath: "example.com/grammarfixture/dep", Name: "Holder"})
	union := root.Type.(*typegrammar.Object).Fields[0].Value.(*typegrammar.Union)
	require.Equal(t, "kind", union.Discriminator)
}

func TestFromPackagesPreservesOverlayAndObservesNextSnapshot(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"root.go":    "package fixture\nimport \"example.com/grammarfixture/dep\"\ntype Root struct { Value dep.Value }\n",
		"dep/dep.go": "//go:build jsonschema && custom\n\npackage dep\n// Value describes the input.\ntype Value struct { Before string }\n",
	})
	cfg := packageConfig(dir)
	cfg.BuildFlags = []string{"-tags", "jsonschema,custom"}
	cfg.Mode |= packages.NeedDeps
	dep := filepath.Join(dir, "dep", "dep.go")
	cfg.Overlay = map[string][]byte{dep: []byte("//go:build jsonschema && custom\n\npackage dep\n// Value comes from the overlay.\ntype Value struct { Current int }\n")}
	pkgs := loadSnapshot(t, cfg, ".")
	pkg, err := grammar.FromPackages(cfg, pkgs[0])
	require.NoError(t, err)
	defs, _ := lowerNamed(t, pkg, "Root")
	valueName := typegrammar.Name{PackagePath: "example.com/grammarfixture/dep", Name: "Value"}
	value := requireDefinition(t, defs, valueName)
	require.Equal(t, "Value comes from the overlay.", value.Description)
	require.Equal(t, "Current", value.Type.(*typegrammar.Object).Fields[0].GoName)

	// A subsequent invocation loads a new source snapshot, with no persistent
	// decorated-source or lowering cache masking the edit.
	require.NoError(t, os.WriteFile(dep, []byte("//go:build jsonschema && custom\n\npackage dep\ntype Value struct { Edited bool }\n"), 0o644))
	cfg.Overlay = nil
	next := loadSnapshot(t, cfg, ".")
	forbidPackageLoads(t)
	fresh, err := grammar.FromPackages(cfg, next[0])
	require.NoError(t, err)
	freshDefs, _ := lowerNamed(t, fresh, "Root")
	require.Equal(t, "Edited", requireDefinition(t, freshDefs, valueName).Type.(*typegrammar.Object).Fields[0].GoName)
}

func TestFromPackagesReadsDeclarationConstraintsFromSnapshot(t *testing.T) {
	for _, constraint := range []string{"jsonschema && custom", "jsonschema || custom"} {
		t.Run(constraint, func(t *testing.T) {
			dir := writeFixture(t, map[string]string{"types.go": `package fixture
type Root struct { Shape Shape }
type Shape interface { shape() }
type Circle struct { Radius int }
func (Circle) shape() {}
`})
			cfg := packageConfig(dir)
			cfg.BuildFlags = []string{"-tags=jsonschema,custom"}
			cfg.Overlay = map[string][]byte{filepath.Join(dir, "schema.go"): []byte("//go:build " + constraint + "\n\npackage fixture\nimport \"github.com/tylergannon/polytype\"\nvar _ = polytype.SealedUnion[Shape](\"overlayKind\")\n")}
			pkgs := loadSnapshot(t, cfg, ".")
			ordinary, ordinaryErr := grammar.LoadWithConfig(cfg, ".")
			var wantDefs typegrammar.Definitions
			var wantRoots []typegrammar.Type
			if ordinaryErr == nil {
				wantDefs, wantRoots = lowerNamed(t, ordinary, "Root")
			}
			forbidPackageLoads(t)
			pkg, err := grammar.FromPackages(cfg, pkgs[0])
			if constraint == "jsonschema || custom" {
				require.ErrorContains(t, err, "is in a file compiled into the package")
				require.EqualError(t, ordinaryErr, err.Error())
				return
			}
			require.NoError(t, ordinaryErr)
			require.NoError(t, err)
			defs, roots := lowerNamed(t, pkg, "Root")
			require.Equal(t, wantDefs, defs)
			require.Equal(t, wantRoots, roots)
			requireProjectionParity(t, wantDefs, defs)
			root := requireDefinition(t, defs, typegrammar.Name{PackagePath: "example.com/grammarfixture", Name: "Root"})
			require.Equal(t, "overlayKind", root.Type.(*typegrammar.Object).Fields[0].Value.(*typegrammar.Union).Discriminator)
		})
	}
}

func TestConfiguredSourcesSeparatePhysicalAndDiagnosticFilenames(t *testing.T) {
	for _, constraint := range []string{"jsonschema", "jsonschema || custom"} {
		t.Run(constraint, func(t *testing.T) {
			dir := writeFixture(t, map[string]string{
				"types.go":  "package fixture\ntype Root struct { Text string }\n",
				"schema.go": "//go:build " + constraint + "\n\npackage fixture\nimport \"github.com/tylergannon/polytype\"\nvar _ = polytype.Declare[Root]()\n",
			})
			cfg := packageConfig(dir)
			cfg.BuildFlags = []string{"-tags=jsonschema,custom"}
			diagnosticDir := filepath.Join(t.TempDir(), "authored")
			cfg.ParseFile = func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
				// Match staging parsers: keep the physical filename for package
				// selection, but report locations in an authored/virtual file.
				display := filepath.Join(diagnosticDir, filepath.Base(filename))
				return parser.ParseFile(fset, filename, "//line "+display+":1\n"+string(src), parser.ParseComments)
			}
			pkgs := loadSnapshot(t, cfg, ".")
			ordinary, ordinaryErr := grammar.LoadWithConfig(cfg, ".")
			var wantDefs typegrammar.Definitions
			if ordinaryErr == nil {
				wantDefs, _ = lowerNamed(t, ordinary, "Root")
			}
			forbidPackageLoads(t)
			supplied, suppliedErr := grammar.FromPackages(cfg, pkgs[0])
			if constraint == "jsonschema || custom" {
				require.ErrorContains(t, ordinaryErr, "is in a file compiled into the package")
				require.ErrorContains(t, ordinaryErr, filepath.Join(diagnosticDir, "schema.go"))
				require.EqualError(t, suppliedErr, ordinaryErr.Error())
				return
			}
			require.NoError(t, ordinaryErr)
			require.NoError(t, suppliedErr)
			defs, _ := lowerNamed(t, supplied, "Root")
			require.Equal(t, filepath.Join(diagnosticDir, "types.go"), requireDefinition(t, defs, typegrammar.Name{PackagePath: "example.com/grammarfixture", Name: "Root"}).Source.Filename)
			require.Equal(t, wantDefs, defs)
			requireProjectionParity(t, wantDefs, defs)
			require.NoDirExists(t, diagnosticDir)
		})
	}
}

func TestFromPackagesGOFLAGSAndProductionTags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		goFlags    string
		buildFlags []string
		env        bool
		custom     bool
		wantError  string
	}{
		{name: "environment_only", goFlags: "-tags=jsonschema"},
		{name: "configured_environment", goFlags: "-tags=jsonschema", env: true},
		{name: "quoted_environment", goFlags: `"-tags=jsonschema custom"`, custom: true, wantError: "is in a file compiled into the package"},
		{name: "explicit_generation_override_production_fallback", goFlags: "-tags=custom", buildFlags: []string{"-tags=jsonschema"}, wantError: "is in a file compiled into the package"},
		{name: "explicit_production_tags_override_environment", goFlags: "-tags=custom", buildFlags: []string{"-tags=jsonschema,other"}},
		{name: "explicit_override_removes_generation_tag", goFlags: "-tags=jsonschema", buildFlags: []string{"-tags=custom"}, custom: true, wantError: "explicit jsonschema build tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFixture(t, map[string]string{
				"types.go":  "package fixture\ntype Root struct { Text string }\n",
				"custom.go": "//go:build custom\n\npackage fixture\ntype CustomSelected struct {}\n",
				"schema.go": "//go:build jsonschema || custom\n\npackage fixture\nimport \"github.com/tylergannon/polytype\"\nvar _ = polytype.Declare[Root]()\n",
			})
			cfg := packageConfig(dir)
			cfg.BuildFlags = tc.buildFlags
			if tc.env {
				t.Setenv("GOFLAGS", "-tags=custom")
				cfg.Env = append(os.Environ(), "GOFLAGS="+tc.goFlags)
			} else {
				t.Setenv("GOFLAGS", tc.goFlags)
			}
			pkgs := loadSnapshot(t, cfg, ".")
			// This is actual Go-command selection, independent of the tag parser
			// under test. Explicit -tags overrides, rather than unions, GOFLAGS.
			require.Equal(t, tc.custom, pkgs[0].Types.Scope().Lookup("CustomSelected") != nil)
			var ordinaryErr error
			var wantDefs typegrammar.Definitions
			var wantRoots []typegrammar.Type
			if tc.wantError != "explicit jsonschema build tag" {
				var ordinary *grammar.Package
				ordinary, ordinaryErr = grammar.LoadWithConfig(cfg, ".")
				if ordinaryErr == nil {
					wantDefs, wantRoots = lowerNamed(t, ordinary, "Root")
				}
			}
			forbidPackageLoads(t)
			pkg, err := grammar.FromPackages(cfg, pkgs[0])
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				if tc.wantError != "explicit jsonschema build tag" {
					require.EqualError(t, ordinaryErr, err.Error())
				}
				return
			}
			require.NoError(t, ordinaryErr)
			require.NoError(t, err)
			defs, roots := lowerNamed(t, pkg, "Root")
			require.Len(t, defs, 1)
			require.Equal(t, wantDefs, defs)
			require.Equal(t, wantRoots, roots)
			requireProjectionParity(t, wantDefs, defs)
		})
	}
}

func TestFromPackagesPreservesRefusalDiagnostics(t *testing.T) {
	dir := writeFixture(t, map[string]string{"types.go": "package fixture\ntype Bad struct { Items map[string]int }\n"})
	cfg := packageConfig(dir)
	pkgs := loadSnapshot(t, cfg, ".")
	ordinary, err := grammar.LoadWithConfig(cfg, ".")
	require.NoError(t, err)
	_, _, want := ordinary.Lower([]grammar.Root{{Type: ordinary.Types().Scope().Lookup("Bad").Type()}})
	require.Error(t, want)
	forbidPackageLoads(t)
	supplied, err := grammar.FromPackages(cfg, pkgs[0])
	require.NoError(t, err)
	_, _, got := supplied.Lower([]grammar.Root{{Type: supplied.Types().Scope().Lookup("Bad").Type()}})
	require.EqualError(t, got, want.Error())
	require.Contains(t, got.Error(), "types.go:2:")
}

func TestFromPackagesRejectsIncompatibleOrIncompleteSnapshots(t *testing.T) {
	dir := writeFixture(t, map[string]string{"types.go": "package fixture\ntype Root struct { Name string }\n"})
	cfg := packageConfig(dir)
	pkgs := loadSnapshot(t, cfg, ".")
	other := loadSnapshot(t, cfg, ".")
	forbidPackageLoads(t)
	for _, flags := range [][]string{nil, {"-tags=custom"}, {"-tags=jsonschema", "-tags=custom"}} {
		bad := *cfg
		bad.BuildFlags = flags
		_, err := grammar.FromPackages(&bad, pkgs[0])
		require.ErrorContains(t, err, "explicit jsonschema build tag")
	}
	_, err := grammar.FromPackages(cfg, nil)
	require.ErrorContains(t, err, "nil root")
	_, err = grammar.FromPackages(cfg, pkgs[0], other[0])
	require.ErrorContains(t, err, "conflicting snapshots")
	for _, drop := range []func(*packages.Package){
		func(p *packages.Package) { p.Syntax = nil },
		func(p *packages.Package) { p.TypesInfo = nil },
		func(p *packages.Package) { p.Fset = nil },
		func(p *packages.Package) { p.Types = nil },
	} {
		incomplete := *pkgs[0]
		drop(&incomplete)
		_, err := grammar.FromPackages(cfg, &incomplete)
		require.ErrorContains(t, err, "needs type-checked source")
	}
	broken := *pkgs[0]
	broken.Errors = []packages.Error{{Pos: "types.go:2:5", Msg: "fixture diagnostic"}}
	_, err = grammar.FromPackages(cfg, &broken)
	require.ErrorContains(t, err, "types.go:2:5: fixture diagnostic")
}

func BenchmarkLowerPackageSnapshot(b *testing.B) {
	dir := writeFixture(b, map[string]string{"fixture.go": holderSource, "todo/todo.go": todoSource})
	cfg := packageConfig(dir)
	pkgs := loadSnapshot(b, cfg, "./...")
	for _, supplied := range []bool{false, true} {
		name := "load_again"
		if supplied {
			name = "caller_snapshot"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var pkg *grammar.Package
				var err error
				if supplied {
					pkg, err = grammar.FromPackages(cfg, pkgs[0], pkgs[1:]...)
				} else {
					pkg, err = grammar.LoadWithConfig(cfg, ".")
				}
				if err != nil {
					b.Fatal(err)
				}
				_, _, err = pkg.Lower([]grammar.Root{{Type: pkg.Types().Scope().Lookup("Holder").Type()}})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Ensure the public boundary works with caller-selected anonymous roots too.
func TestFromPackagesAnonymousRoot(t *testing.T) {
	dir := writeFixture(t, map[string]string{"types.go": "package fixture\ntype Root struct { Text string }\n"})
	cfg := packageConfig(dir)
	pkgs := loadSnapshot(t, cfg, ".")
	forbidPackageLoads(t)
	pkg, err := grammar.FromPackages(cfg, pkgs[0])
	require.NoError(t, err)
	_, roots, err := pkg.Lower([]grammar.Root{{Type: types.NewSlice(types.Typ[types.String])}})
	require.NoError(t, err)
	require.Equal(t, []typegrammar.Type{&typegrammar.Slice{Element: &typegrammar.Scalar{Kind: typegrammar.String}}}, roots)
}

func TestFromPackagesExternalModule(t *testing.T) {
	dir := writeFixture(t, map[string]string{
		"model/root.go": "package model\nimport \"example.com/grammarfixture/dep\"\ntype Root struct { Value dep.Value }\n",
		"dep/value.go":  "package dep\n// Value is imported source.\ntype Value struct { Text string }\n",
		"consumer_test.go": `package consumer_test
import (
 "path/filepath"
 "testing"
 "github.com/tylergannon/polytype/grammar"
 "golang.org/x/tools/go/packages"
)
func TestReuse(t *testing.T) {
 cfg := &packages.Config{Mode: grammar.LoadMode | packages.NeedDeps, BuildFlags: []string{"-tags=jsonschema"}}
 pkgs, err := packages.Load(cfg, "./model")
 if err != nil || len(pkgs) != 1 || len(pkgs[0].Errors) != 0 { t.Fatalf("load: %v, %v", pkgs, err) }
 t.Setenv("GOPACKAGESDRIVER", filepath.Join(t.TempDir(), "forbidden-reload"))
 pkg, err := grammar.FromPackages(cfg, pkgs[0])
 if err != nil { t.Fatal(err) }
 for range 2 {
  defs, roots, err := pkg.Lower([]grammar.Root{{Type: pkgs[0].Types.Scope().Lookup("Root").Type()}})
  if err != nil || len(defs) != 2 || len(roots) != 1 { t.Fatalf("lower: %v, %v, %v", defs, roots, err) }
 }
 if pkg.Types() != pkgs[0].Types { t.Fatal("lost caller type identity") }
}
`,
	})
	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
