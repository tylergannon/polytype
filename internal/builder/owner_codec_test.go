package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/internal/testutils"
)

func TestOwnerCodecRejectsProductionJSONMethodBeforeWriting(t *testing.T) {
	for _, method := range []string{"MarshalJSON", "UnmarshalJSON"} {
		t.Run(method, func(t *testing.T) {
			targetDir := writeOwnerCollisionFixture(t, "")
			declaration := `func (*Owner) UnmarshalJSON([]byte) error { return nil }`
			if method == "MarshalJSON" {
				declaration = `func (Owner) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }`
			}
			require.NoError(t, os.WriteFile(filepath.Join(targetDir, "types.go"), []byte(ownerCollisionTypes+declaration), 0o644))

			err := Run(BuilderArgs{TargetDir: targetDir})
			require.ErrorContains(t, err, "handwritten production "+method)
			assertOwnerCollisionSentinels(t, targetDir)
		})
	}
}

func TestOwnerCodecAllowsGenerationOnlyDeclarationStub(t *testing.T) {
	targetDir := writeOwnerCollisionFixture(t, `func (Owner) MarshalJSON() ([]byte, error) { panic("not implemented") }`)
	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	_, err = New(pkgs[0])
	require.NoError(t, err)
}

func TestOwnerCodecRejectsPromotedProductionJSONMethod(t *testing.T) {
	targetDir := writeOwnerCollisionFixture(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "types.go"), []byte(`package fixture

type Value interface{ value() }
type First struct{}
func (First) value() {}

type Hook struct{}
func (Hook) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

type Owner struct {
	Hook
	Value Value `+"`json:\"value\"`"+`
}
`), 0o644))
	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "handwritten production MarshalJSON already declared or promoted")
	assertOwnerCollisionSentinels(t, targetDir)
}

func TestOwnerCodecRejectsPromotedGeneratedOwner(t *testing.T) {
	targetDir := writeOwnerCollisionFixture(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "types.go"), []byte(`package fixture

type Value interface{ value() }
type First struct{}
func (First) value() {}

type Embedded struct {
	Value Value `+"`json:\"value\"`"+`
}
type Owner struct { Embedded }
`), 0o644))
	schemaPath := filepath.Join(targetDir, "schema.go")
	schema, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	schema = append(schema, []byte(`
func (Embedded) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.NewJSONSchemaMethod(Embedded.Schema)
`)...)
	require.NoError(t, os.WriteFile(schemaPath, schema, 0o644))
	err = Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "embedded type Embedded also requires generated owner codecs")
	assertOwnerCollisionSentinels(t, targetDir)
}

func TestOwnerCodecRejectsAmbiguousPromotedInterfaceFieldsBeforeWriting(t *testing.T) {
	targetDir := writeOwnerCollisionFixture(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "types.go"), []byte(`package fixture

type Value interface{ value() }
type First struct{}
func (First) value() {}

type Left struct {
	Value Value `+"`json:\"value\"`"+`
}

type Right struct {
	Value Value `+"`json:\"value\"`"+`
}

type Owner struct {
	Left
	Right
}
`), 0o644))

	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, `promoted registered interface fields Owner.Left.Value and Owner.Right.Value are ambiguous because they share Go field name "Value"`)
	assertOwnerCollisionSentinels(t, targetDir)
}

func TestOwnerCodecRejectsForeignEmbeddedGeneratedOwnerBeforeWriting(t *testing.T) {
	moduleDir := newFixtureModule(t)
	writeFixturePackage(t, moduleDir, "dep", map[string]string{
		"types.go": `package dep

type Value interface{ value() }
type First struct{}
func (First) value() {}
type Embedded struct { Value Value ` + "`json:\"value\"`" + ` }
`,
		"schema.go": `//go:build jsonschema

package dep

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Embedded) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.NewJSONSchemaMethod(Embedded.Schema)
`,
		"jsonschema_gen.go": `//go:build !jsonschema

package dep

func (Embedded) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (*Embedded) UnmarshalJSON([]byte) error { return nil }
`,
	})
	targetDir := writeFixturePackage(t, moduleDir, "", map[string]string{
		"types.go": `package fixture

import dep "` + fixtureModulePath + `/dep"

type Value interface{ value() }
type First struct{}
func (First) value() {}
type Owner struct {
	dep.Embedded
	Local Value ` + "`json:\"local\"`" + `
}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Owner) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
	})
	writeOwnerCollisionSentinels(t, targetDir)

	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "foreign embedded type dep.Embedded has generated production JSON codecs")
	assertOwnerCollisionSentinels(t, targetDir)
}

func TestLegacyHelpersUseResolvedPackageIdentity(t *testing.T) {
	moduleDir := newFixtureModule(t)
	baseImport := fixtureModulePath

	packages := []struct {
		dir  string
		name string
	}{
		{dir: "left", name: "events"},
		{dir: "middle", name: "events1"},
		{dir: "right", name: "events"},
		{dir: "reserved", name: "json"},
	}
	for _, pkg := range packages {
		writeFixturePackage(t, moduleDir, pkg.dir, map[string]string{
			"types.go": `package ` + pkg.name + `

type Event interface{ isEvent() }
type Created struct { Name string ` + "`json:\"name\"`" + ` }
func (Created) isEvent() {}
`,
		})
	}

	targetDir := writeFixturePackage(t, moduleDir, "", map[string]string{
		"types.go": `package fixture

import (
	left "` + baseImport + `/left"
	middle "` + baseImport + `/middle"
	right "` + baseImport + `/right"
	reserved "` + baseImport + `/reserved"
)

type Owner struct {
	Left left.Event ` + "`json:\"left\"`" + `
	Middle middle.Event ` + "`json:\"middle\"`" + `
	Right right.Event ` + "`json:\"right\"`" + `
	Reserved reserved.Event ` + "`json:\"reserved\"`" + `
}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Owner) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		"codec_test.go": `package fixture

import (
	"encoding/json"
	"testing"
	left "` + baseImport + `/left"
	middle "` + baseImport + `/middle"
	right "` + baseImport + `/right"
	reserved "` + baseImport + `/reserved"
)

func TestDistinctSameNamedInterfacesRoundTrip(t *testing.T) {
	want := Owner{
		Left: left.Created{Name: "left"},
		Middle: middle.Created{Name: "middle"},
		Right: right.Created{Name: "right"},
		Reserved: reserved.Created{Name: "reserved"},
	}
	data, err := json.Marshal(want)
	if err != nil { t.Fatal(err) }
	var got Owner
	if err := json.Unmarshal(data, &got); err != nil { t.Fatal(err) }
	if value, ok := got.Left.(left.Created); !ok || value.Name != "left" { t.Fatalf("left = %#v", got.Left) }
	if value, ok := got.Middle.(middle.Created); !ok || value.Name != "middle" { t.Fatalf("middle = %#v", got.Middle) }
	if value, ok := got.Right.(right.Created); !ok || value.Name != "right" { t.Fatalf("right = %#v", got.Right) }
	if value, ok := got.Reserved.(reserved.Created); !ok || value.Name != "reserved" { t.Fatalf("reserved = %#v", got.Reserved) }
}
`,
	})

	require.NoError(t, Run(BuilderArgs{TargetDir: targetDir}))
	generated, err := os.ReadFile(filepath.Join(targetDir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(generated), "func __jsonUnmarshal__events__Event__"))
	require.Equal(t, 1, strings.Count(string(generated), "func __jsonUnmarshal__events1__Event__"))
	require.Equal(t, 1, strings.Count(string(generated), "func __jsonUnmarshal__json__Event__"))
	require.Contains(t, string(generated), `events2 "`+baseImport+`/right"`)
	require.Contains(t, string(generated), `json1 "`+baseImport+`/reserved"`)
	exit, stdout, stderr, err := testutils.RunCommand("go", targetDir, "test", "./...")
	require.NoError(t, err)
	require.Equal(t, 0, exit, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
}

const ownerCollisionTypes = `package fixture

type Value interface{ value() }
type First struct{}
func (First) value() {}
type Owner struct { Value Value ` + "`json:\"value\"`" + ` }
`

func writeOwnerCollisionFixture(t *testing.T, stub string) string {
	t.Helper()
	targetDir := newFixture(t, map[string]string{
		"types.go": ownerCollisionTypes,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Owner) Schema() json.RawMessage { panic("not implemented") }
` + stub + `
var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
	})
	writeOwnerCollisionSentinels(t, targetDir)
	return targetDir
}

func writeOwnerCollisionSentinels(t *testing.T, targetDir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "jsonschema"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "jsonschema", "Owner.json"), []byte("sentinel-schema"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "jsonschema_gen.go"), []byte("//go:build !jsonschema\n\npackage fixture\n\nvar sentinel = true\n"), 0o644))
}

func assertOwnerCollisionSentinels(t *testing.T, targetDir string) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(targetDir, "jsonschema", "Owner.json"))
	require.NoError(t, err)
	require.Equal(t, "sentinel-schema", string(schema))
	generated, err := os.ReadFile(filepath.Join(targetDir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.Equal(t, "//go:build !jsonschema\n\npackage fixture\n\nvar sentinel = true\n", string(generated))
}
