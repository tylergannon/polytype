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
	types, err := os.ReadFile(filepath.Join(cwd, "testfixtures", "union_codec", "types.go"))
	require.NoError(t, err)
	schema, err := os.ReadFile(filepath.Join(cwd, "testfixtures", "union_codec", "schema.go"))
	require.NoError(t, err)

	targetDir := newFixture(t, map[string]string{
		"types.go":  string(types),
		"schema.go": string(schema),
	})

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
