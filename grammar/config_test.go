package grammar_test

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/grammar"
	"github.com/tylergannon/polytype/typegrammar"
	"golang.org/x/tools/go/packages"
)

func TestLoadWithConfigKeepsOverlayForRecursiveDependencies(t *testing.T) {
	t.Parallel()
	dir := writeFixture(t, map[string]string{
		"root.go":    "package fixture\nimport \"example.com/grammarfixture/dep\"\ntype Root struct { Value dep.Value }\n",
		"dep/dep.go": "package dep\nthis file is syntactically broken\n",
	})
	dep := filepath.Join(dir, "dep", "dep.go")
	leaf := filepath.Join(dir, "newpkg", "value.go")
	cfg := &packages.Config{Dir: dir, Env: append(os.Environ(), "GOWORK=off"), BuildFlags: []string{"-tags=custom"}, Overlay: map[string][]byte{
		dep:  []byte("package dep\nimport \"example.com/grammarfixture/newpkg\"\ntype Value struct { Current newpkg.Value }\n"),
		leaf: []byte("//go:build custom && jsonschema\n\npackage newpkg\ntype Value struct { Text string }\n"),
	}}
	pkg, err := grammar.LoadWithConfig(cfg, ".")
	require.NoError(t, err)
	require.Equal(t, []string{"-tags=custom"}, cfg.BuildFlags)
	root := pkg.Types().Scope().Lookup("Root").Type()
	defs, nodes, err := pkg.Lower([]grammar.Root{{Type: root}})
	require.NoError(t, err)
	require.NoError(t, defs.Validate())
	require.Len(t, nodes, 1)
	names := map[typegrammar.Name]bool{}
	for _, def := range defs {
		names[def.Name] = true
	}
	require.True(t, names[typegrammar.Name{PackagePath: "example.com/grammarfixture/dep", Name: "Value"}])
	require.True(t, names[typegrammar.Name{PackagePath: "example.com/grammarfixture/newpkg", Name: "Value"}])
	// A second, marker-free Lower must retain the same loader too.
	depType := types.Unalias(root).Underlying().(*types.Struct).Field(0).Type()
	_, _, err = pkg.Lower([]grammar.Root{{Type: depType}})
	require.NoError(t, err)
	original, err := os.ReadFile(dep)
	require.NoError(t, err)
	require.Contains(t, string(original), "syntactically broken")
	_, err = os.Stat(leaf)
	require.True(t, os.IsNotExist(err))
	_, err = grammar.Load(dir)
	require.Error(t, err)
}

func TestLoadWithConfigPreservesCallerBuildTags(t *testing.T) {
	t.Parallel()
	for _, flags := range [][]string{{"-tags=custom second"}, {"-tags", "custom second"}, {"-tags=custom,second"}, {"-tags=unused", "-tags=custom second"}} {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			dir := writeFixture(t, map[string]string{"types.go": "package fixture\ntype Root struct { Field Selected }\n", "tagged.go": "//go:build jsonschema && custom && second && !unused\n\npackage fixture\ntype Selected string\n"})
			cfg := &packages.Config{Dir: dir, BuildFlags: append([]string(nil), flags...)}
			pkg, err := grammar.LoadWithConfig(cfg, ".")
			require.NoError(t, err)
			require.Equal(t, flags, cfg.BuildFlags)
			_, _, err = pkg.Lower([]grammar.Root{{Type: pkg.Types().Scope().Lookup("Root").Type()}})
			require.NoError(t, err)
		})
	}
}

func TestLoadWithConfigKeepsLegacyGOFLAGSSelection(t *testing.T) {
	t.Setenv("GOFLAGS", "-tags=custom")
	dir := writeFixture(t, map[string]string{
		"types.go":  "package fixture\ntype Root struct { Text string }\n",
		"custom.go": "//go:build custom\n\npackage fixture\ntype CustomSelected struct {}\n",
		"schema.go": "//go:build jsonschema\n\npackage fixture\ntype GenerationSelected struct {}\n",
	})
	plain, err := grammar.Load(dir)
	require.NoError(t, err)
	for _, flags := range [][]string{nil, {"-tags=jsonschema"}} {
		configured, err := grammar.LoadWithConfig(&packages.Config{Dir: dir, BuildFlags: flags}, ".")
		require.NoError(t, err)
		for _, pkg := range []*grammar.Package{plain, configured} {
			require.Nil(t, pkg.Types().Scope().Lookup("CustomSelected"), "the injected jsonschema flag overrides GOFLAGS")
			require.NotNil(t, pkg.Types().Scope().Lookup("GenerationSelected"))
		}
		want, _ := lowerNamed(t, plain, "Root")
		got, _ := lowerNamed(t, configured, "Root")
		requireProjectionParity(t, want, got)
	}
	explicit, err := grammar.LoadWithConfig(&packages.Config{Dir: dir, BuildFlags: []string{"-tags=custom"}}, ".")
	require.NoError(t, err)
	require.NotNil(t, explicit.Types().Scope().Lookup("CustomSelected"), "explicit caller tags remain selected alongside jsonschema")
	require.NotNil(t, explicit.Types().Scope().Lookup("GenerationSelected"))
}
