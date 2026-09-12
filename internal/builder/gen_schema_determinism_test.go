package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
)

func TestRenderGoCodeTwiceUsesFreshCodecProjection(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	targetDir, err := os.MkdirTemp(filepath.Join(cwd, "testfixtures"), "render_twice_")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(targetDir)) })
	for _, name := range []string{"types.go", "schema.go"} {
		data, readErr := os.ReadFile(filepath.Join(cwd, "testfixtures", "union_codec", name))
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, name), data, 0o644))
	}

	packages, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, packages, 1)
	builder, err := New(packages[0])
	require.NoError(t, err)
	builder.Validate = true

	require.NoError(t, builder.RenderGoCode())
	generatedPath := filepath.Join(targetDir, "jsonschema_gen.go")
	first, err := os.ReadFile(generatedPath)
	require.NoError(t, err)

	require.NoError(t, builder.RenderGoCode())
	second, err := os.ReadFile(generatedPath)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Equal(t, 1, strings.Count(string(second), "func (e Envelope) MarshalJSON()"))
	require.Equal(t, 1, strings.Count(string(second), "func (e *Envelope) UnmarshalJSON(data []byte)"))
}
