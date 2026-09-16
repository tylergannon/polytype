package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// fixtureModulePath is the module path every temp-module fixture is written
// under. Fixtures needing more than one package (a root plus a dependency it
// imports) put each package in its own subdirectory and import it as
// fixtureModulePath + "/" + <subdir>.
//
// The spelling is load-bearing: diagnostics quote it, so tests assert on it
// (see TestRegisteredInterfaceRejectedOutsideDirectField).
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
