package codegen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/testutils"
)

func TestProgrammaticGenerationSelectsOutputsWithoutRegistration(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	t.Run("transport outputs do not generate schema", func(t *testing.T) {
		fixture := copyProgrammaticFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/transport")

		require.FileExists(t, filepath.Join(fixture, "generated", "typescript", "types.ts"))
		require.FileExists(t, filepath.Join(fixture, "generated", "typescript", "index.ts"))
		require.FileExists(t, filepath.Join(fixture, "generated", "codec", "codec_gen.go"))
		_, err := os.Stat(filepath.Join(fixture, "model", "jsonschema"))
		require.ErrorIs(t, err, os.ErrNotExist)
		_, err = os.Stat(filepath.Join(fixture, "model", "jsonschema_gen.go"))
		require.ErrorIs(t, err, os.ErrNotExist)
		runGo(t, fixture, "test", "./generated/codec")
	})

	t.Run("schema files do not require a schema method", func(t *testing.T) {
		fixture := copyProgrammaticFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/schema")

		require.FileExists(t, filepath.Join(fixture, "model", "jsonschema", "Envelope.json"))
		_, err := os.Stat(filepath.Join(fixture, "model", "jsonschema_gen.go"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Go JSON codecs do not generate schema", func(t *testing.T) {
		fixture := copyProgrammaticFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/gojson")

		require.FileExists(t, filepath.Join(fixture, "model", "jsonschema_gen.go"))
		_, err := os.Stat(filepath.Join(fixture, "model", "jsonschema"))
		require.ErrorIs(t, err, os.ErrNotExist)
		runGo(t, fixture, "test", "./model")
	})
}

func copyProgrammaticFixture(t *testing.T, repoRoot string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, testutils.CopyDir("testdata/programmatic", dir))
	goMod := filepath.Join(dir, "go.mod")
	contents, err := os.ReadFile(goMod)
	require.NoError(t, err)
	rewritten := strings.Replace(string(contents), "=> ../../../", "=> "+repoRoot, 1)
	require.NotEqual(t, string(contents), rewritten)
	require.NoError(t, os.WriteFile(goMod, []byte(rewritten), 0o644))
	runGo(t, dir, "mod", "tidy")
	return dir
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	exit, stdout, stderr, err := testutils.RunCommand("go", dir, args...)
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("go %v:\n%s\n%s", args, stdout, stderr))
}
