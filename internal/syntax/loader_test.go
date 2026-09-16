package syntax

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadUsesTargetAsPackageWorkingDirectory(t *testing.T) {
	t.Parallel()

	originalConfigDir := DefaultPackageCfg.Dir
	target := filepath.Join(t.TempDir(), "independent-module")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(target, "go.mod"),
		[]byte("module example.com/independent\n\ngo 1.24\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(target, "types.go"),
		[]byte("package independent\n\ntype Example struct { Name string `json:\"name\"` }\n"),
		0o644,
	))

	packages, err := Load(target)
	require.NoError(t, err)
	require.Len(t, packages, 1)
	require.Equal(t, "independent", packages[0].Name)
	require.Equal(t, "example.com/independent", packages[0].PkgPath)
	require.Equal(t, originalConfigDir, DefaultPackageCfg.Dir, "Load must not mutate the shared default config")
}

// TestIsProductionGoFileIgnoresTheGenerationTag proves a declaration file is
// never mistaken for production code when GOFLAGS carries the generation tag,
// as some editor setups do; otherwise every declaration would be skipped.
// Other custom tags from GOFLAGS still apply.
func TestIsProductionGoFileIgnoresTheGenerationTag(t *testing.T) {
	dir := t.TempDir()
	declarations := filepath.Join(dir, "schema.go")
	require.NoError(t, os.WriteFile(declarations, []byte("//go:build "+BuildTag+"\n\npackage fixture\n"), 0o644))
	custom := filepath.Join(dir, "custom.go")
	require.NoError(t, os.WriteFile(custom, []byte("//go:build custom\n\npackage fixture\n"), 0o644))

	for _, goflags := range []string{"-tags=" + BuildTag + ",custom", "-tags " + BuildTag + ",custom"} {
		t.Run(goflags, func(t *testing.T) {
			t.Setenv("GOFLAGS", goflags)
			production, err := IsProductionGoFile(declarations)
			require.NoError(t, err)
			require.False(t, production)
			production, err = IsProductionGoFile(custom)
			require.NoError(t, err)
			require.True(t, production)
		})
	}
}
