package typescript

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrinterHonorsUnionAndIntersectionPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr typeExpr
		want string
	}{
		{
			name: "union within intersection",
			expr: intersection(
				union(keywordType("string"), keywordType("null")),
				referenceType("Tagged"),
			),
			want: "(string | null) & Tagged",
		},
		{
			name: "intersection within union",
			expr: union(
				intersection(referenceType("Payload"), referenceType("Tag")),
				keywordType("null"),
			),
			want: "Payload & Tag | null",
		},
		{
			name: "generic argument owns its expression",
			expr: genericType{name: "Array", arguments: []typeExpr{
				union(literalType(`"a"`), literalType(`"b"`)),
			}},
			want: `Array<"a" | "b">`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			writeType(&out, tc.expr, 0, "")
			require.Equal(t, tc.want, out.String())
		})
	}
}

func TestPrinterQuotesPropertiesAndSanitizesComments(t *testing.T) {
	t.Parallel()

	module := string(printModule([]typeAlias{{
		name:        "Escaped",
		description: "bad */ separator\u2028tail",
		typeExpr: objectType{properties: []property{{
			name:        "quote\"slash\\\n雪",
			description: "line one\rline two\u0001",
			typeExpr:    keywordType("string"),
		}}},
	}}))

	require.Contains(t, module, `bad *\/ separator\u2028tail`)
	require.Contains(t, module, "line one\n   * line two\\u0001")
	require.Contains(t, module, `"quote\"slash\\\n雪": string;`)
}
