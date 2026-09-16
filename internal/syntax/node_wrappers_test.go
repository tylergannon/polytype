package syntax

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/require"
)

func TestStructFieldWrapper(t *testing.T) {
	importSpec := &dst.ImportSpec{
		Name: dst.NewIdent("schema"),
		Path: &dst.BasicLit{Kind: token.STRING, Value: `"` + SchemaPackagePath + `"`},
	}
	file := &dst.File{Imports: []*dst.ImportSpec{importSpec}}

	tests := []struct {
		name      string
		typeExpr  dst.Expr
		wantKind  WrapperKind
		wantInner string
	}{
		{
			name: "optional through import alias",
			typeExpr: &dst.IndexExpr{X: &dst.SelectorExpr{
				X: dst.NewIdent("schema"), Sel: dst.NewIdent("Optional"),
			}, Index: dst.NewIdent("int")},
			wantKind: WrapperOptional, wantInner: "int",
		},
		{
			name: "nullable through decorated path",
			typeExpr: &dst.IndexExpr{X: &dst.SelectorExpr{
				X: &dst.Ident{Name: "anything", Path: SchemaPackagePath}, Sel: dst.NewIdent("Nullable"),
			}, Index: dst.NewIdent("string")},
			wantKind: WrapperNullable, wantInner: "string",
		},
		{
			name:     "decorated generic root",
			typeExpr: &dst.IndexExpr{X: &dst.Ident{Name: "Optional", Path: SchemaPackagePath}, Index: dst.NewIdent("int")},
			wantKind: WrapperOptional, wantInner: "int",
		},
		{
			name: "same name from another package",
			typeExpr: &dst.IndexExpr{X: &dst.SelectorExpr{
				X: &dst.Ident{Name: "other", Path: "example.com/other"}, Sel: dst.NewIdent("Optional"),
			}, Index: dst.NewIdent("int")},
			wantKind: WrapperNone, wantInner: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typeSpec := &TypeSpec{file: file}
			field := StructField{
				TypeSpec: typeSpec, Excerpt: tt.typeExpr,
				Field: &dst.Field{Names: []*dst.Ident{dst.NewIdent("Value")}, Type: tt.typeExpr},
			}
			kind, inner, err := field.Wrapper()
			require.NoError(t, err)
			require.Equal(t, tt.wantKind, kind)
			if tt.wantInner == "" {
				return
			}
			ident, ok := inner.(*dst.Ident)
			require.True(t, ok)
			require.Equal(t, tt.wantInner, ident.Name)
		})
	}
}

func TestStructFieldRequiredAndJSONOptions(t *testing.T) {
	wrapper := &dst.IndexExpr{X: &dst.SelectorExpr{
		X: &dst.Ident{Name: "jsonschema", Path: SchemaPackagePath}, Sel: dst.NewIdent("Optional"),
	}, Index: dst.NewIdent("int")}
	field := StructField{
		TypeSpec: &TypeSpec{}, Excerpt: wrapper,
		Field: &dst.Field{
			Names: []*dst.Ident{dst.NewIdent("Value")}, Type: wrapper,
			Tag: &dst.BasicLit{Kind: token.STRING, Value: "`json:\"value,omitzero\" jsonschema:\"optional\"`"},
		},
	}
	require.False(t, field.Required())
	require.True(t, field.HasJSONOption("omitzero"))

	field.Field.Type = dst.NewIdent("int")
	field.Excerpt = field.Field.Type
	require.True(t, field.Required(), "legacy optional tag must be inert")
}

func TestStructFieldPropNames(t *testing.T) {
	tests := []struct {
		name      string
		tag       string
		wantNames []string
		wantSkip  bool
	}{
		{
			name:      "omitted name with omitzero",
			tag:       `json:",omitzero"`,
			wantNames: []string{"MaxRetries"},
		},
		{
			name:      "omitted name with omitempty",
			tag:       `json:",omitempty"`,
			wantNames: []string{"MaxRetries"},
		},
		{
			name:      "explicit name",
			tag:       `json:"max_retries,omitzero"`,
			wantNames: []string{"max_retries"},
		},
		{
			name:      "untagged exported field",
			wantNames: []string{"MaxRetries"},
		},
		{
			name:      "empty tag value",
			tag:       `json:""`,
			wantNames: []string{"MaxRetries"},
		},
		{
			name:      "skipped field",
			tag:       `json:"-"`,
			wantNames: []string{"-"},
			wantSkip:  true,
		},
		{
			// encoding/json ignores only the exact tag "-"; "-," names a
			// property "-".
			name:      "property named dash",
			tag:       `json:"-,"`,
			wantNames: []string{"-"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := StructField{Field: &dst.Field{
				Names: []*dst.Ident{dst.NewIdent("MaxRetries")},
				Type:  dst.NewIdent("int"),
			}}
			if tt.tag != "" {
				field.Field.Tag = &dst.BasicLit{
					Kind:  token.STRING,
					Value: "`" + tt.tag + "`",
				}
			}

			require.Equal(t, tt.wantNames, field.PropNames())
			require.Equal(t, tt.wantSkip, field.Skip())
		})
	}
}

func TestEmbeddedStructFieldSkip(t *testing.T) {
	tests := []struct {
		name     string
		typ      dst.Expr
		tag      string
		wantSkip bool
	}{
		{name: "untagged", typ: dst.NewIdent("Base")},
		{name: "renamed", typ: dst.NewIdent("Base"), tag: `json:"base"`},
		{name: "ignored", typ: dst.NewIdent("Base"), tag: `json:"-"`, wantSkip: true},
		{name: "ignored pointer", typ: &dst.StarExpr{X: dst.NewIdent("Base")}, tag: `json:"-"`, wantSkip: true},
		{name: "property named dash", typ: dst.NewIdent("Base"), tag: `json:"-,"`},
		{name: "unexported", typ: dst.NewIdent("base"), wantSkip: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := StructField{Field: &dst.Field{Type: tt.typ}}
			if tt.tag != "" {
				field.Field.Tag = &dst.BasicLit{Kind: token.STRING, Value: "`" + tt.tag + "`"}
			}
			require.Equal(t, tt.wantSkip, field.Skip())
		})
	}
}

func TestStructFieldNamedVisibility(t *testing.T) {
	tests := []struct {
		name      string
		names     []string
		wantNames []string
		wantSkip  bool
	}{
		{name: "exported", names: []string{"Exported"}, wantNames: []string{"Exported"}},
		{name: "unexported", names: []string{"unexported"}, wantSkip: true},
		{name: "mixed group", names: []string{"unexported", "Exported"}, wantNames: []string{"Exported"}},
		{name: "unexported group", names: []string{"first", "second"}, wantSkip: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := StructField{Field: &dst.Field{Type: dst.NewIdent("string")}}
			for _, name := range tt.names {
				field.Field.Names = append(field.Field.Names, dst.NewIdent(name))
			}

			require.Equal(t, tt.wantNames, field.PropNames())
			require.Equal(t, tt.wantSkip, field.Skip())
		})
	}
}
