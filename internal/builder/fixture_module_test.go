package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
)

// fixtureModulePath is the module path every temp-module fixture is written
// under. Fixtures needing more than one package (a root plus a dependency it
// imports) put each package in its own subdirectory and import it as
// fixtureModulePath + "/" + <subdir>.
//
// The spelling is load-bearing: generated import aliases and diagnostics
// quote it, so tests assert on it (owner_codec_test.go:186 and
// sealed_union_discriminator_test.go:168 both build expectations from it).
const fixtureModulePath = "example.com/typegrammarfixture"

// newFixtureModule creates an isolated single-module fixture tree under
// t.TempDir() and returns its root directory.
//
// Fixtures live outside the repository module on purpose. A fixture written
// inside the module root is recorded by `go test` as an input file, and
// because these directories are created per-run and deleted on cleanup their
// recorded paths no longer exist on the next run -- so computeTestInputsID
// can never reproduce the stored ID and the package's test result is
// permanently uncacheable (issue #131). Writing fixtures under TMPDIR keeps
// them out of that accounting entirely, because cmd/go ignores opens and stats
// outside the module root.
func newFixtureModule(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	dir := t.TempDir()
	module := fmt.Sprintf(
		"module %s\n\ngo 1.27\n\nrequire github.com/tylergannon/polytype v0.0.0\n\nreplace github.com/tylergannon/polytype => %s\n",
		fixtureModulePath, root,
	)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o644))
	return dir
}

// writeFixturePackage writes one package of a fixture module and returns its
// directory. An empty subdir writes the module's root package; any other
// subdir is importable as fixtureModulePath + "/" + subdir.
func writeFixturePackage(t *testing.T, moduleDir, subdir string, files map[string]string) string {
	t.Helper()
	dir := moduleDir
	if subdir != "" {
		dir = filepath.Join(moduleDir, subdir)
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	return dir
}

// newFixture is the common shape: a fresh module holding a single root
// package built from files.
func newFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	return writeFixturePackage(t, newFixtureModule(t), "", files)
}

// fixtureCase is one package of a batched fixture module: a directory name
// plus the files to write into it.
type fixtureCase struct {
	name  string
	files map[string]string
}

// loadedCase is a materialized fixture case: its loaded package and its
// directory on disk.
type loadedCase struct {
	pkg *decorator.Package
	dir string
}

// loadFixtureCases writes every case as its own package inside a single
// module, then loads them all in ONE decorator.Load.
//
// A load costs roughly the same for one package as for many -- the expense is
// fixed setup (spawning `go list`, resolving the module, preparing the
// type-checker), not per-package work. Measured at 40 packages, one batched
// load was ~34x faster than forty individual ones. Table-driven tests that
// loaded per case therefore paid that fixed cost once per case for no reason.
func loadFixtureCases(t *testing.T, cases []fixtureCase) map[string]loadedCase {
	t.Helper()

	moduleDir := newFixtureModule(t)
	dirs := make(map[string]string, len(cases))
	for i, c := range cases {
		// Case names are prose ("reachable non-sealed interface"), so index
		// them rather than sanitizing into directory names.
		subdir := fmt.Sprintf("case%02d", i)
		dirs[c.name] = writeFixturePackage(t, moduleDir, subdir, c.files)
	}

	cfg := *syntax.DefaultPackageCfg
	cfg.Dir = moduleDir
	pkgs, err := decorator.Load(&cfg, "./...")
	require.NoError(t, err)

	byDir := make(map[string]*decorator.Package, len(pkgs))
	for _, pkg := range pkgs {
		if len(pkg.GoFiles) > 0 {
			byDir[filepath.Dir(pkg.GoFiles[0])] = pkg
		} else if len(pkg.CompiledGoFiles) > 0 {
			byDir[filepath.Dir(pkg.CompiledGoFiles[0])] = pkg
		}
	}

	result := make(map[string]loadedCase, len(cases))
	for name, dir := range dirs {
		pkg, ok := byDir[dir]
		require.True(t, ok, "fixture case %q was not loaded from %s", name, dir)
		result[name] = loadedCase{pkg: pkg, dir: dir}
	}
	return result
}
