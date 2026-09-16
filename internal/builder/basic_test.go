package builder_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/builder"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/internal/testutils"
)

// fixtureModulePath is the module path of internal/builder/testfixtures. The
// fixtures import each other by this prefix (enums/enumsremote,
// traversal/remotestruct, ...), so a copy of the tree keeps resolving as long
// as the copy declares the same module path.
const fixtureModulePath = "github.com/tylergannon/polytype/internal/builder/testfixtures"

type fixture struct {
	name string
	// validate mirrors the --validate flag the fixture's generator used.
	validate bool
	// idempotent fixtures are generated a second time, against a fresh load of
	// the already-generated tree, and must produce byte-identical output.
	idempotent bool
}

var basicFixtures = []fixture{
	{name: "basictypes"},
	{name: "entrypoints"},
	{name: "enums", validate: true},
	{name: "indirecttypes"},
	{name: "interfaces"},
	{name: "optionality", validate: true},
	{name: "providers"},
	{name: "providers_builder"},
	{name: "structs"},
	{name: "traversal"},
	{name: "union_codec", validate: true, idempotent: true},
	{name: "v1_enums_stringmode", validate: true, idempotent: true},
	{name: "v1_interfaces_options", validate: true},
}

// TestBasic generates every fixture and proves the output matches its golden
// files, then proves the generated code compiles and its runtime tests pass.
//
// The whole tree is copied into one temp module and loaded ONCE. Loading is
// the dominant cost -- it type-checks the full dependency graph from source --
// and that cost is per-load, not per-package, so thirteen fixtures in one load
// cost about the same as one (issue #131). Generation then runs in process via
// builder.RunLoaded rather than shelling out to `go generate` thirteen times.
//
// Nothing is written inside the repository module. Fixtures generated in place
// used to leave files that `go test` recorded as inputs and then rewrote,
// which permanently defeated the test cache.
func TestBasic(t *testing.T) {
	t.Parallel()

	moduleDir := materializeFixtureModule(t)

	cfg := *syntax.DefaultPackageCfg
	cfg.Dir = moduleDir
	pkgs, err := decorator.Load(&cfg, "./...")
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	loaded := make(map[string]*decorator.Package, len(pkgs))
	for _, pkg := range pkgs {
		loaded[pkg.PkgPath] = pkg
	}

	// Parallel children of this subtest all complete before t.Run returns, so
	// the acceptance phase below observes a fully generated tree.
	t.Run("generate", func(t *testing.T) {
		for _, f := range basicFixtures {
			t.Run(f.name, func(t *testing.T) {
				t.Parallel()

				dir := filepath.Join(moduleDir, f.name)
				pkg := loaded[fixtureModulePath+"/"+f.name]
				require.NotNil(t, pkg, "fixture %s was not loaded", f.name)

				// pkg.Errors is deliberately not asserted empty. Some fixtures
				// carry type errors on purpose under the jsonschema tag --
				// indirecttypes declares methods on pointer-underlying types,
				// which is the shape the builder must diagnose -- and
				// generation is expected to succeed regardless.
				require.NoError(t, builder.RunLoaded(pkg, builder.BuilderArgs{
					TargetDir: dir,
					Pretty:    true,
					Validate:  f.validate,
				}))

				assertGeneratedGoHeader(t, dir)
				assertGoldens(t, dir)
			})
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		assertIdempotentRegeneration(t, moduleDir)
	})

	t.Run("acceptance", func(t *testing.T) {
		assertFixtureModuleBuildsAndPasses(t, moduleDir)
	})
}

// materializeFixtureModule copies internal/builder/testfixtures into a temp
// directory as a single module and returns its root.
func materializeFixtureModule(t *testing.T) string {
	t.Helper()

	cwd, err := os.Getwd()
	require.NoError(t, err)
	repoRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	require.NoError(t, err)
	src := filepath.Join(cwd, "testfixtures")
	dst := t.TempDir()

	require.NoError(t, filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// The copy declares its own module; the checked-in one resolves the
		// repository through a relative replace that would not survive the move.
		if rel == "go.mod" || rel == "go.sum" {
			return nil
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}))

	gomod := fmt.Sprintf(`module %s

go 1.27

require (
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2
	github.com/tylergannon/polytype v0.2.1
)

replace github.com/tylergannon/polytype => %s
`, fixtureModulePath, repoRoot)
	require.NoError(t, os.WriteFile(filepath.Join(dst, "go.mod"), []byte(gomod), 0o644))

	// The repository's own go.sum already covers every fixture dependency.
	sum, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dst, "go.sum"), sum, 0o644))

	return dst
}

// assertGoldens compares every generated artifact in dir against its golden.
//
// It walks for "*.golden" rather than consulting a hardcoded file list: a list
// cannot notice a golden whose generated counterpart stopped being produced.
func assertGoldens(t *testing.T, dir string) {
	t.Helper()

	var checked int
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".golden") {
			return nil
		}
		actual := strings.TrimSuffix(path, ".golden")
		info, statErr := os.Stat(actual)
		require.NoError(t, statErr, "golden %s has no generated counterpart", path)
		require.False(t, info.IsDir(), "golden %s names a directory", path)
		testutils.AssertGoldenFile(t, actual, ".golden")
		checked++
		return nil
	}))
	require.NotZero(t, checked, "no golden files found under %s", dir)
}

func assertGeneratedGoHeader(t *testing.T, dir string) {
	t.Helper()

	generated, err := os.ReadFile(filepath.Join(dir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(generated), "//go:build !jsonschema\n\n"))
	require.Contains(t, string(generated), "// Code generated by polytype. DO NOT EDIT.\n\npackage ")
	require.NotContains(t, string(generated), "// +build")
}

// assertIdempotentRegeneration re-generates the fixtures marked idempotent and
// requires byte-identical output.
//
// It reloads the module rather than reusing the first load. Regeneration reads
// the previous run's artifacts back off disk, so the property under test is
// "generating over existing output changes nothing" -- reusing a package graph
// captured before any output existed would quietly test something weaker.
func assertIdempotentRegeneration(t *testing.T, moduleDir string) {
	t.Helper()

	before := make(map[string][]byte)
	var patterns []string
	for _, f := range basicFixtures {
		if !f.idempotent {
			continue
		}
		data, err := os.ReadFile(filepath.Join(moduleDir, f.name, "jsonschema_gen.go"))
		require.NoError(t, err)
		before[f.name] = data
		patterns = append(patterns, "./"+f.name)
	}

	// Only the fixtures that regenerate are reloaded. The other eleven would
	// be type-checked for nothing.
	cfg := *syntax.DefaultPackageCfg
	cfg.Dir = moduleDir
	pkgs, err := decorator.Load(&cfg, patterns...)
	require.NoError(t, err)

	loaded := make(map[string]*decorator.Package, len(pkgs))
	for _, pkg := range pkgs {
		loaded[pkg.PkgPath] = pkg
	}

	for _, f := range basicFixtures {
		if !f.idempotent {
			continue
		}
		pkg := loaded[fixtureModulePath+"/"+f.name]
		require.NotNil(t, pkg, "fixture %s was not reloaded", f.name)
		require.NoError(t, builder.RunLoaded(pkg, builder.BuilderArgs{
			TargetDir: filepath.Join(moduleDir, f.name),
			Pretty:    true,
			Validate:  f.validate,
		}))
		after, err := os.ReadFile(filepath.Join(moduleDir, f.name, "jsonschema_gen.go"))
		require.NoError(t, err)
		require.Equal(t, string(before[f.name]), string(after), "second generation changed %s/jsonschema_gen.go", f.name)
	}
}

// assertFixtureModuleBuildsAndPasses is the acceptance layer: one tidy, one
// vet and one test run over every generated fixture at once, replacing
// thirteen separate build/test invocations.
func assertFixtureModuleBuildsAndPasses(t *testing.T, moduleDir string) {
	t.Helper()

	run := func(args ...string) (string, string) {
		t.Helper()
		exitCode, stdout, stderr, err := testutils.RunCommand("go", moduleDir, args...)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "go %s\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), stdout, stderr)
		return stdout, stderr
	}

	// Generation introduces imports the fixture sources did not have (the
	// jsonschema validator for --validate fixtures), so tidy before building.
	run("mod", "tidy")
	run("vet", "./...")
	run("test", "./...")

	// Generated declarations must not leak into the fixture's package
	// documentation.
	stdout, _ := run("doc", "-all", "./basictypes")
	require.Contains(t, stdout, "Package basictypes is the authored fixture documentation.")
	require.NotContains(t, stdout, "Code generated by polytype")
}
