package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/testutils"
)

func writeNamedContainerFixture(t *testing.T, types string) string {
	t.Helper()
	return newFixture(t, map[string]string{
		"types.go": "package fixture\n\n" + types,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Root) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.Declare(Root.Schema)
`,
	})
}

// TestNamedContainerTraversesReachableCodecs is the issue #127 regression:
// a named slice type (type Items []Item) must not stop codec discovery. The
// plan is asserted directly; TestNestedNamedContainerTraversal is the
// runtime round trip through the same traversal.
func TestNamedContainerTraversesReachableCodecs(t *testing.T) {
	types := `type Root struct {
	Items Items ` + "`json:\"items\"`" + `
}

type Items []Item

type Item struct {
	Blocks []Block ` + "`json:\"blocks\"`" + `
}

type Block interface{ block() }

type Leaf struct {
	Text string ` + "`json:\"text\"`" + `
}

func (Leaf) block() {}
`
	builder := loadBuilder(t, writeNamedContainerFixture(t, types))
	require.NotContains(t, builder.ownerCodecs, "Root", "Root declares no union field of its own")
	item, ok := builder.ownerCodecs["Item"]
	require.True(t, ok, "codec discovery must reach Item through named container Items")
	require.Len(t, item.UnionFields, 1)
	require.True(t, item.UnionFields[0].Repeated)
	require.Equal(t, []string{"Leaf"}, variantTags(item.UnionFields[0].Union))
}

// TestNestedNamedContainerTraversal verifies codec discovery through multiple
// layers of named containers: type Outer []Inner, type Inner []Wrapper where
// Wrapper contains a sealed union.
func TestNestedNamedContainerTraversal(t *testing.T) {
	types := `type Root struct {
	Data Outer ` + "`json:\"data\"`" + `
}

type Outer []Inner

type Inner struct {
	Value Shape ` + "`json:\"value\"`" + `
}

type Shape interface{ shape() }

type Circle struct {
	Radius int ` + "`json:\"radius\"`" + `
}

func (Circle) shape() {}

type Square struct {
	Side int ` + "`json:\"side\"`" + `
}

func (Square) shape() {}
`
	targetDir := writeNamedContainerFixture(t, types)
	require.NoError(t, Run(BuilderArgs{TargetDir: targetDir}))

	generated, err := os.ReadFile(filepath.Join(targetDir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(generated), `case "Circle":`)
	require.Contains(t, string(generated), `case "Square":`)

	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "codec_test.go"), []byte(`package fixture

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNestedNamedContainerRoundTrip(t *testing.T) {
	want := Root{Data: Outer{{Value: Circle{Radius: 5}}, {Value: Square{Side: 3}}}}
	data, err := json.Marshal(want)
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(data), `+"`"+`"type":"Circle"`+"`"+`) {
		t.Fatalf("missing Circle discriminator: %s", data)
	}
	if !strings.Contains(string(data), `+"`"+`"type":"Square"`+"`"+`) {
		t.Fatalf("missing Square discriminator: %s", data)
	}
	var got Root
	if err := json.Unmarshal(data, &got); err != nil { t.Fatal(err) }
	if len(got.Data) != 2 { t.Fatalf("data len = %d", len(got.Data)) }
	if c, ok := got.Data[0].Value.(Circle); !ok || c.Radius != 5 {
		t.Fatalf("circle = %#v", got.Data[0].Value)
	}
	if s, ok := got.Data[1].Value.(Square); !ok || s.Side != 3 {
		t.Fatalf("square = %#v", got.Data[1].Value)
	}
}
`), 0o644))

	exit, stdout, stderr, err := testutils.RunCommand("go", targetDir, "test", "./...")
	require.NoError(t, err)
	require.Equal(t, 0, exit, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
}

// TestDiscoverCodecPlansNamedContainer verifies the schema-free codec-only path
// (discoverCodecs=true, mapSchemas=false) traverses named containers. This
// is the exact code path the codegen.Gen(GoJSON()) API uses.
func TestDiscoverCodecPlansNamedContainer(t *testing.T) {
	types := `type Root struct {
	Items Items ` + "`json:\"items\"`" + `
}

type Items []Item

type Item struct {
	Blocks   []Block ` + "`json:\"blocks\"`" + `
	Children Items   ` + "`json:\"children\"`" + `
}

type Block interface{ block() }

type Leaf struct {
	Text string ` + "`json:\"text\"`" + `
}

func (Leaf) block() {}
`
	targetDir := writeNamedContainerFixture(t, types)

	b, err := LoadProgrammatic(targetDir, ProgrammaticConfig{
		Declarations: []ConfiguredDeclaration{{
			PackagePath: fixtureModulePath,
			TypeName:    "Root",
		}},
	}, true, false)
	require.NoError(t, err)
	require.Contains(t, b.ownerCodecs, "Item", "codec discovery must reach Item through named container Items")
	require.NotEmpty(t, b.ownerCodecs["Item"].UnionFields, "Item must have union fields discovered")
}
