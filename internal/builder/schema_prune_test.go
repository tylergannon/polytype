package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerationPrunesOrphanedOwnedSchemas(t *testing.T) {
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go": `package fixture

type Current struct {
	Name string ` + "`json:\"name\"`" + `
}

type Removed struct {
	Value int ` + "`json:\"value\"`" + `
}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Current) Schema() json.RawMessage { panic("not implemented") }
func (Removed) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Current.Schema)
var _ = polytype.Declare(Removed.Schema)
`,
	})

	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))
	schemaDir := filepath.Join(dir, "jsonschema")
	removedPath := filepath.Join(schemaDir, "Removed.json")
	removedSumPath := removedPath + ".sum"
	removedSchema, err := os.ReadFile(removedPath)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "application.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.go"), []byte(`//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Current) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Current.Schema)
`), 0o644))

	err = Run(BuilderArgs{TargetDir: dir, NoChanges: true})
	require.ErrorContains(t, err, "orphaned generated schema artifacts detected")
	require.ErrorContains(t, err, "Removed.json")
	require.FileExists(t, removedPath)
	require.FileExists(t, removedSumPath)

	currentPath := filepath.Join(schemaDir, "Current.json")
	currentSchema, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(removedPath, []byte("modified by application\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "types.go"), []byte(`package fixture

type Current struct {
	Name string `+"`json:\"name\"`"+`
	Count int `+"`json:\"count\"`"+`
}

type Removed struct {
	Value int `+"`json:\"value\"`"+`
}
`), 0o644))
	err = Run(BuilderArgs{TargetDir: dir})
	require.ErrorContains(t, err, "refusing to remove modified orphaned schema Removed.json")
	require.FileExists(t, removedPath)
	require.FileExists(t, removedSumPath)
	currentAfterFailure, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	require.Equal(t, currentSchema, currentAfterFailure)

	require.NoError(t, os.WriteFile(removedPath, removedSchema, 0o644))
	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))
	require.NoFileExists(t, removedPath)
	require.NoFileExists(t, removedSumPath)
	require.FileExists(t, currentPath)
	require.FileExists(t, filepath.Join(schemaDir, "Current.json.sum"))
	require.FileExists(t, filepath.Join(schemaDir, "application.json"))
}
