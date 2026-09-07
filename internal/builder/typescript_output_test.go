package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/typescript"
)

func generatedTypeScriptFile(name, body string) typescript.File {
	return typescript.File{
		Name:    name,
		Content: []byte(typescript.GeneratedHeader + body),
	}
}

func TestTypeScriptBarrelRequiresOutputDirectory(t *testing.T) {
	t.Parallel()

	err := Run(BuilderArgs{TypeScriptBarrel: true})
	require.EqualError(t, err, "--typescript-barrel requires --typescript")
}

func TestTypeScriptOutputCreatesRequestedFilesDeterministically(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "custom", "generated")
	files := []typescript.File{
		generatedTypeScriptFile("types.ts", "export interface Example {}\n"),
		generatedTypeScriptFile("index.ts", "export type { Example } from \"./types.js\";\n"),
	}
	plan, err := prepareTypeScriptOutput(dir, files, true)
	require.NoError(t, err)
	require.True(t, plan.changed())
	require.ElementsMatch(t, []string{
		filepath.Join(dir, "types.ts"),
		filepath.Join(dir, "index.ts"),
	}, plan.changedPaths())
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist, "preflight must not create the output directory")

	require.NoError(t, plan.apply(false))
	for _, file := range files {
		actual, readErr := os.ReadFile(filepath.Join(dir, file.Name))
		require.NoError(t, readErr)
		require.Equal(t, file.Content, actual)
	}

	second, err := prepareTypeScriptOutput(dir, files, true)
	require.NoError(t, err)
	require.False(t, second.changed())
	require.Empty(t, second.changedPaths())
}

func TestTypeScriptOutputRefusesUnownedCollisions(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"types.ts", "index.ts"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(path, []byte("// maintained by the application\n"), 0o644))
			files := []typescript.File{generatedTypeScriptFile("types.ts", "export type T = string;\n")}
			barrel := name == "index.ts"
			if barrel {
				files = append(files, generatedTypeScriptFile("index.ts", "export type { T } from \"./types.js\";\n"))
			}

			_, err := prepareTypeScriptOutput(dir, files, barrel)
			require.ErrorContains(t, err, "refusing to overwrite unowned TypeScript output")
			actual, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.Equal(t, "// maintained by the application\n", string(actual))
		})
	}
}

func TestTypeScriptOutputReplacesOwnedStaleFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "types.ts")
	require.NoError(t, os.WriteFile(path, []byte(typescript.GeneratedHeader+"stale\n"), 0o644))
	want := generatedTypeScriptFile("types.ts", "export type Fresh = boolean;\n")

	plan, err := prepareTypeScriptOutput(dir, []typescript.File{want}, false)
	require.NoError(t, err)
	require.True(t, plan.changed())
	require.NoError(t, plan.apply(false))
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want.Content, actual)
}

func TestTypeScriptOutputDisablingBarrelRemovesOnlyOwnedIndex(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		index       string
		wantRemoved bool
	}{
		{name: "owned", index: typescript.GeneratedHeader + "export {};\n", wantRemoved: true},
		{name: "unowned", index: "export { applicationValue };\n", wantRemoved: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			types := generatedTypeScriptFile("types.ts", "export type T = string;\n")
			require.NoError(t, os.WriteFile(filepath.Join(dir, "types.ts"), types.Content, 0o644))
			indexPath := filepath.Join(dir, "index.ts")
			require.NoError(t, os.WriteFile(indexPath, []byte(tc.index), 0o644))

			plan, err := prepareTypeScriptOutput(dir, []typescript.File{types}, false)
			require.NoError(t, err)
			require.Equal(t, tc.wantRemoved, plan.changed())
			require.NoError(t, plan.apply(false))
			_, err = os.Stat(indexPath)
			if tc.wantRemoved {
				require.ErrorIs(t, err, os.ErrNotExist)
			} else {
				require.NoError(t, err)
				actual, readErr := os.ReadFile(indexPath)
				require.NoError(t, readErr)
				require.Equal(t, tc.index, string(actual))
			}
		})
	}
}

func TestTypeScriptOutputPreflightRejectsInvalidGeneratorResults(t *testing.T) {
	t.Parallel()

	validTypes := generatedTypeScriptFile("types.ts", "export {};\n")
	for _, tc := range []struct {
		name   string
		files  []typescript.File
		barrel bool
		want   string
	}{
		{name: "missing types", want: "did not produce types.ts"},
		{name: "missing barrel", files: []typescript.File{validTypes}, barrel: true, want: "did not produce index.ts"},
		{name: "unexpected name", files: []typescript.File{generatedTypeScriptFile("../types.ts", "")}, want: "unexpected TypeScript output filename"},
		{name: "duplicate", files: []typescript.File{validTypes, validTypes}, want: "duplicate TypeScript output filename"},
		{name: "missing ownership header", files: []typescript.File{{Name: "types.ts", Content: []byte("export {};\n")}}, want: "missing the ownership header"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := prepareTypeScriptOutput(filepath.Join(t.TempDir(), "output"), tc.files, tc.barrel)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestTypeScriptOutputApplyRechecksOwnership(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "output")
	file := generatedTypeScriptFile("types.ts", "export {};\n")
	plan, err := prepareTypeScriptOutput(dir, []typescript.File{file}, false)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "types.ts")
	require.NoError(t, os.WriteFile(path, []byte("// created after preflight\n"), 0o644))

	err = plan.apply(false)
	require.ErrorContains(t, err, "refusing to overwrite unowned TypeScript output")
	actual, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, "// created after preflight\n", string(actual))
}

func TestTypeScriptOutputApplyReconcilesFileRemovedAfterPreflight(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := generatedTypeScriptFile("types.ts", "export {};\n")
	path := filepath.Join(dir, file.Name)
	require.NoError(t, os.WriteFile(path, file.Content, 0o644))
	plan, err := prepareTypeScriptOutput(dir, []typescript.File{file}, false)
	require.NoError(t, err)
	require.False(t, plan.changed())
	require.NoError(t, os.Remove(path))

	require.NoError(t, plan.apply(false))
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, file.Content, actual)
}
