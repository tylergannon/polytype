package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/javascript"
	"github.com/tylergannon/polytype/typescript"
)

func generatedJavaScriptFile(name, body string) javascript.File {
	return javascript.File{
		Name:    name,
		Content: []byte(javascript.GeneratedHeader + body),
	}
}

func TestJavaScriptOutputCreatesRequestedFileDeterministically(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "custom", "generated")
	files := []javascript.File{generatedJavaScriptFile("types.js", "export {};\n")}
	plan, err := prepareJavaScriptOutput(dir, files)
	require.NoError(t, err)
	require.True(t, plan.changed())
	require.Equal(t, []string{filepath.Join(dir, "types.js")}, plan.changedPaths())
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist, "preflight must not create the output directory")

	require.NoError(t, plan.apply(false))
	actual, readErr := os.ReadFile(filepath.Join(dir, "types.js"))
	require.NoError(t, readErr)
	require.Equal(t, files[0].Content, actual)

	second, err := prepareJavaScriptOutput(dir, files)
	require.NoError(t, err)
	require.False(t, second.changed())
	require.Empty(t, second.changedPaths())
}

func TestJavaScriptOutputRefusesUnownedCollision(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "types.js")
	require.NoError(t, os.WriteFile(path, []byte("// maintained by the application\n"), 0o644))

	_, err := prepareJavaScriptOutput(dir, []javascript.File{generatedJavaScriptFile("types.js", "export {};\n")})
	require.ErrorContains(t, err, "refusing to overwrite unowned JavaScript output")
	actual, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, "// maintained by the application\n", string(actual))
}

func TestJavaScriptOutputReplacesOwnedStaleFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "types.js")
	require.NoError(t, os.WriteFile(path, []byte(javascript.GeneratedHeader+"stale\n"), 0o644))
	want := generatedJavaScriptFile("types.js", "/** @typedef {string} T */\n")

	plan, err := prepareJavaScriptOutput(dir, []javascript.File{want})
	require.NoError(t, err)
	require.True(t, plan.changed())
	require.NoError(t, plan.apply(false))
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want.Content, actual)
}

func TestJavaScriptOutputRemovesGeneratedTypeScriptSiblings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "types.ts"), []byte(typescript.GeneratedHeader+"export type T = string;\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.ts"), []byte(typescript.GeneratedHeader+"export type { T } from \"./types.js\";\n"), 0o644))

	plan, err := prepareJavaScriptOutput(dir, []javascript.File{generatedJavaScriptFile("types.js", "export {};\n")})
	require.NoError(t, err)
	require.True(t, plan.changed())
	require.ElementsMatch(t, []string{
		filepath.Join(dir, "types.js"),
		filepath.Join(dir, "types.ts"),
		filepath.Join(dir, "index.ts"),
	}, plan.changedPaths())
	require.NoError(t, plan.apply(false))
	for _, name := range []string{"types.ts", "index.ts"} {
		_, statErr := os.Stat(filepath.Join(dir, name))
		require.ErrorIs(t, statErr, os.ErrNotExist, "generated %s must be removed when switching to JavaScript", name)
	}
}

func TestJavaScriptOutputPreservesUnownedTypeScriptSiblings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "types.ts"), []byte("// application types\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.ts"), []byte("// application barrel\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("unrelated\n"), 0o644))

	plan, err := prepareJavaScriptOutput(dir, []javascript.File{generatedJavaScriptFile("types.js", "export {};\n")})
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(dir, "types.js")}, plan.changedPaths())
	require.NoError(t, plan.apply(false))
	for name, want := range map[string]string{
		"types.ts":  "// application types\n",
		"index.ts":  "// application barrel\n",
		"notes.txt": "unrelated\n",
	} {
		actual, readErr := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, readErr)
		require.Equal(t, want, string(actual), "%s must be preserved", name)
	}
}

func TestTypeScriptOutputRemovesGeneratedJavaScriptModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "types.js"), []byte(javascript.GeneratedHeader+"export {};\n"), 0o644))
	types := generatedTypeScriptFile("types.ts", "export type T = string;\n")

	plan, err := prepareTypeScriptOutput(dir, []typescript.File{types}, false)
	require.NoError(t, err)
	require.True(t, plan.changed())
	require.ElementsMatch(t, []string{
		filepath.Join(dir, "types.ts"),
		filepath.Join(dir, "types.js"),
	}, plan.changedPaths())
	require.NoError(t, plan.apply(false))
	_, statErr := os.Stat(filepath.Join(dir, "types.js"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "generated types.js must be removed when switching to TypeScript")
}

func TestTypeScriptOutputPreservesUnownedJavaScriptModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "types.js"), []byte("// application module\n"), 0o644))
	types := generatedTypeScriptFile("types.ts", "export type T = string;\n")

	plan, err := prepareTypeScriptOutput(dir, []typescript.File{types}, false)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(dir, "types.ts")}, plan.changedPaths())
	require.NoError(t, plan.apply(false))
	actual, readErr := os.ReadFile(filepath.Join(dir, "types.js"))
	require.NoError(t, readErr)
	require.Equal(t, "// application module\n", string(actual))
}

func TestDeclarationOutputModesRejectSameDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "generated")
	require.ErrorContains(t, validateDeclarationOutputArgs(BuilderArgs{TypeScriptDir: dir, JavaScriptDir: dir}),
		"cannot target the same directory")
	require.NoError(t, validateDeclarationOutputArgs(BuilderArgs{
		TypeScriptDir: filepath.Join(dir, "ts"),
		JavaScriptDir: dir,
	}))
	require.ErrorContains(t, validateDeclarationOutputArgs(BuilderArgs{TypeScriptBarrel: true}),
		"--typescript-barrel requires --typescript")
}
