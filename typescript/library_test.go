package typescript_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/devalue/codegen"
	"github.com/tylergannon/polytype/grammar"
	"github.com/tylergannon/polytype/internal/testutils"
	"github.com/tylergannon/polytype/typegrammar"
	"github.com/tylergannon/polytype/typescript"
)

// The tagged file the CLI path needs. The library path never sees it: its
// copy of the fixture holds only model/types.go.
const cliRegistration = `//go:build jsonschema

package model

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Envelope) Schema() json.RawMessage    { panic("not implemented") }
func (Composition) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Envelope.Schema)
var _ = polytype.Declare(Composition.Schema)
`

// TestLibraryPathMatchesCLI is the acceptance test for issue #111: a program
// importing only grammar, typegrammar and this package, against a package
// holding no //go:build jsonschema file and no Declare, produces the same
// types.ts and index.ts as `polytype gen --typescript` does for the same
// roots, and the same lowered definitions feed devalue/codegen unchanged.
func TestLibraryPathMatchesCLI(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)
	root := t.TempDir()

	// Two copies of the fixture module: the CLI's gets the tagged file, the
	// library's stays exactly as committed.
	library := copyFixture(t, repoRoot, filepath.Join(root, "library"))
	cli := copyFixture(t, repoRoot, filepath.Join(root, "cli"))
	require.NoError(t, os.WriteFile(filepath.Join(cli, "model", "schema.go"), []byte(cliRegistration), 0o644))
	assertNoPolytypeFiles(t, filepath.Join(library, "model"))

	binary := filepath.Join(root, "polytype")
	runGo(t, repoRoot, "build", "-o", binary, "./polytype")
	tsDir := filepath.Join(root, "cli-ts")
	exit, stdout, stderr, err := testutils.RunCommand(binary, root, "gen", "--target", filepath.Join(cli, "model"), "--typescript", tsDir, "--typescript-barrel")
	require.NoError(t, err)
	require.Equal(t, 0, exit, stdout+stderr)

	pkg, err := grammar.Load(filepath.Join(library, "model"))
	require.NoError(t, err)
	scope := pkg.Types().Scope()
	defs, roots, err := pkg.Lower([]grammar.Root{
		{Type: scope.Lookup("Envelope").Type()},
		{Type: scope.Lookup("Composition").Type()},
	})
	require.NoError(t, err)

	result, err := typescript.Generate(defs, typescript.Options{Barrel: true})
	require.NoError(t, err)
	require.Len(t, result.Files, 2)
	for _, file := range result.Files {
		want, err := os.ReadFile(filepath.Join(tsDir, file.Name))
		require.NoError(t, err)
		require.Equal(t, string(want), string(file.Content), "%s differs between the CLI and the library path", file.Name)
	}
	assertNoPolytypeFiles(t, filepath.Join(library, "model"))

	// The driving generator writes `import type { Envelope } from './types'`
	// from the returned names; here none needed renaming.
	envelope := typegrammar.Name{PackagePath: "polytypetypescriptfixture/model", Name: "Envelope"}
	require.Equal(t, "Envelope", result.Names[envelope])
	require.Len(t, result.Names, len(defs))
	for _, def := range defs {
		require.Contains(t, string(result.Files[0].Content), "export type "+result.Names[def.Name]+" =")
	}

	// One Lower result serves both backends.
	src, err := codegen.Generate(defs, roots, codegen.Options{PackageName: "codec", ImportPath: "polytypetypescriptfixture/codec"})
	require.NoError(t, err)
	require.Contains(t, string(src), "func EncodeEnvelope(")
}

func copyFixture(t *testing.T, repoRoot, dir string) string {
	t.Helper()
	require.NoError(t, testutils.CopyDir("testdata/fixture", dir))
	gomod := filepath.Join(dir, "go.mod")
	contents, err := os.ReadFile(gomod)
	require.NoError(t, err)
	rewritten := strings.Replace(string(contents), "=> ../../../", "=> "+repoRoot, 1)
	require.NotEqual(t, string(contents), rewritten, "fixture go.mod has no replace directive to rewrite")
	require.NoError(t, os.WriteFile(gomod, []byte(rewritten), 0o644))
	runGo(t, dir, "mod", "tidy")
	return dir
}

// assertNoPolytypeFiles proves the library path left the package as the
// author wrote it: no tagged registration file, no Declare, no schema output.
func assertNoPolytypeFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.False(t, entry.IsDir(), "unexpected directory %s in %s", entry.Name(), dir)
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		require.False(t, strings.HasPrefix(string(content), "//go:build jsonschema"), "%s is a tagged file", entry.Name())
		require.NotContains(t, string(content), "polytype.Declare", "%s declares a root", entry.Name())
	}
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	exit, stdout, stderr, err := testutils.RunCommand("go", dir, args...)
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("go %v:\n%s\n%s", args, stdout, stderr))
}
