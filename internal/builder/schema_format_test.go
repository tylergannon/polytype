package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/schema"
	"github.com/tylergannon/polytype/internal/syntax"
)

func TestWriteSchemaUsesSchemaHardlines(t *testing.T) {
	t.Parallel()
	typeID := syntax.TypeID{PkgPath: "example.com/test", TypeName: "Example"}
	builder := SchemaBuilder{schemas: map[string]schema.JSONSchema{
		typeID.TypeName: schema.ObjectNode{
			Desc: "A deliberately long description, with punctuation, stays on one semantic line.",
			Properties: schema.ObjectPropSet{
				{
					Name: "name",
					Schema: schema.PropertyNode[string]{
						Typ:  "string",
						Desc: "Display name.",
					},
				},
			},
		},
	}}

	targetDir := t.TempDir()
	changed, err := builder.writeSchema(typeID, targetDir, false)
	require.NoError(t, err)
	require.True(t, changed)

	generated, err := os.ReadFile(filepath.Join(targetDir, "Example.json"))
	require.NoError(t, err)
	require.Equal(t, `{"type":"object",
"description":"A deliberately long description, with punctuation, stays on one semantic line.","properties":{
"name":{"type":"string","description":"Display name."}
},"required":["name"],"additionalProperties":false}
`, string(generated))

	changed, err = builder.writeSchema(typeID, targetDir, false)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestWriteTemplateSchemaUsesSchemaHardlines(t *testing.T) {
	t.Parallel()
	typeID := syntax.TypeID{PkgPath: "example.com/test", TypeName: "Example"}
	builder := SchemaBuilder{
		schemas: map[string]schema.JSONSchema{
			typeID.TypeName: schema.ObjectNode{
				Properties: schema.ObjectPropSet{
					{Name: "dynamic", Schema: schema.TemplateHoleNode{Name: "dynamic"}},
				},
			},
		},
		TypeProvidersMap: map[string][]FieldProvider{
			"Example": nil,
		},
	}

	targetDir := t.TempDir()
	changed, err := builder.writeSchema(typeID, targetDir, false)
	require.NoError(t, err)
	require.True(t, changed)

	generated, err := os.ReadFile(filepath.Join(targetDir, "Example.json.tmpl"))
	require.NoError(t, err)
	require.Equal(t, `{"type":"object",
"properties":{
"dynamic":{{.dynamic}}
},"required":["dynamic"],"additionalProperties":false}
`, string(generated))
}
