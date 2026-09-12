package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerationRefusesToSilentlyRemoveValidationMethods(t *testing.T) {
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go": `package fixture

type Todo struct {
	Title string ` + "`json:\"title\"`" + `
}

func ValidateTodo(data []byte) error {
	return (Todo{}).ValidateJSON(data)
}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Todo) Schema() json.RawMessage { panic("not implemented") }
func (Todo) ValidateJSON([]byte) error { panic("not implemented") }

var _ = polytype.Declare(Todo.Schema)
`,
	})

	require.NoError(t, Run(BuilderArgs{TargetDir: dir, Validate: true}))
	generatedPath := filepath.Join(dir, "jsonschema_gen.go")
	schemaPath := filepath.Join(dir, "jsonschema", "Todo.json")
	generatedBefore, err := os.ReadFile(generatedPath)
	require.NoError(t, err)
	schemaBefore, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	require.Contains(t, string(generatedBefore), "func (Todo) ValidateJSON(data []byte) error")

	// Make a schema change so the failed run also proves that the guard fires
	// before any output is mutated.
	types := `package fixture

type Todo struct {
	Title string ` + "`json:\"title\"`" + `
	Done bool ` + "`json:\"done\"`" + `
}

func ValidateTodo(data []byte) error {
	return (Todo{}).ValidateJSON(data)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "types.go"), []byte(types), 0o644))

	err = Run(BuilderArgs{TargetDir: dir})
	require.ErrorContains(t, err, "refusing to remove previously generated ValidateJSON")
	require.ErrorContains(t, err, "--validate")
	require.ErrorContains(t, err, "--force")

	generatedAfter, err := os.ReadFile(generatedPath)
	require.NoError(t, err)
	schemaAfter, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	require.Equal(t, generatedBefore, generatedAfter)
	require.Equal(t, schemaBefore, schemaAfter)
}

func TestForceExplicitlyAllowsValidationMethodRemoval(t *testing.T) {
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go": `package fixture

type Todo struct {
	Title string ` + "`json:\"title\"`" + `
}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Todo) Schema() json.RawMessage { panic("not implemented") }
func (Todo) ValidateJSON([]byte) error { panic("not implemented") }

var _ = polytype.Declare(Todo.Schema)
`,
	})

	require.NoError(t, Run(BuilderArgs{TargetDir: dir, Validate: true}))
	require.NoError(t, Run(BuilderArgs{TargetDir: dir, Force: true}))

	generated, err := os.ReadFile(filepath.Join(dir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.NotContains(t, string(generated), "ValidateJSON")
}

func TestGenerationRefusesToSilentlyRemoveYAMLValidation(t *testing.T) {
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go": `package fixture

type Todo struct {
	Title string ` + "`json:\"title\"`" + `
}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Todo) Schema() json.RawMessage { panic("not implemented") }
func (Todo) ValidateJSON([]byte) error { panic("not implemented") }
func (Todo) ValidateYAML([]byte) error { panic("not implemented") }

var _ = polytype.Declare(Todo.Schema)
`,
	})

	require.NoError(t, Run(BuilderArgs{
		TargetDir:        dir,
		Validate:         true,
		UnmarshalFormats: UnmarshalFormatsBoth,
	}))

	err := Run(BuilderArgs{TargetDir: dir, Validate: true})
	require.ErrorContains(t, err, "refusing to remove previously generated ValidateYAML")
	require.ErrorContains(t, err, "--validate --formats=both")
}
