package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
)

// TestAsRefDefinitionNameCollisionFailsDuringGeneration proves that two
// distinct AsRef()'d types which resolve to the same bare "$defs" name
// (here: two different "Shared" types, one local and one imported under a
// selector-expression receiver) are rejected as a hard, generation-time
// error rather than silently colliding in the generated "$defs" map.
func TestAsRefDefinitionNameCollisionFailsDuringGeneration(t *testing.T) {
	t.Parallel()

	targetDir := writeAsRefCollisionFixture(t)
	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.Empty(t, pkgs[0].Errors)
	scan, err := syntax.LoadPackage(pkgs[0])
	require.NoError(t, err)
	require.NotEmpty(t, scan.SchemaMethods)

	_, err = New(pkgs[0])
	require.ErrorContains(t, err, "AsRef definition name collision")
	require.ErrorContains(t, err, `"Shared"`)
}

// writeAsRefCollisionFixture writes a two-package fixture module and returns
// the root package's directory. The "dep" subpackage carries no jsonschema
// build-tag constraints and exposes a "Shared" type with a Schema() method; it
// exists purely so the root package can register it as a second, distinct
// AsRef()'d type that happens to share its bare name with a locally declared
// "Shared" type.
func writeAsRefCollisionFixture(t *testing.T) string {
	t.Helper()

	moduleDir := newFixtureModule(t)
	writeFixturePackage(t, moduleDir, "dep", map[string]string{
		"shared.go": `package dep

import "encoding/json"

type Shared struct {
	Name string ` + "`json:\"name\"`" + `
}

func (Shared) Schema() json.RawMessage { panic("not implemented") }
`,
	})

	return writeFixturePackage(t, moduleDir, "", map[string]string{
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
	dep "` + fixtureModulePath + `/dep"
)

type Shared struct {
	Name string ` + "`json:\"name\"`" + `
}

func (Shared) Schema() json.RawMessage { panic("not implemented") }

type Container struct {
	Local  Shared     ` + "`json:\"local\"`" + `
	Remote dep.Shared ` + "`json:\"remote\"`" + `
}

func (Container) Schema() json.RawMessage { panic("not implemented") }

var (
	_ = polytype.NewJSONSchemaMethod(Shared.Schema, polytype.AsRef())
	_ = polytype.NewJSONSchemaMethod(dep.Shared.Schema, polytype.AsRef())
	_ = polytype.NewJSONSchemaMethod(Container.Schema)
)
`,
	})
}
