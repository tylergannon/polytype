package builder

import (
	"testing"

	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// loadBuilder is how every lowering test starts: load the fixture package
// once and build it in process. Nothing is generated, so a test asserts on
// the lowered definitions and codec plans, never on rendered output.
func loadBuilder(t *testing.T, targetDir string) SchemaBuilder {
	t.Helper()
	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	return builderFor(t, pkgs[0])
}

// builderFor is loadBuilder against an already-loaded package, for tests that
// batch every case of a table into a single decorator.Load (loadFixtureCases).
func builderFor(t *testing.T, pkg *decorator.Package) SchemaBuilder {
	t.Helper()
	require.Empty(t, pkg.Errors)
	builder, err := New(pkg)
	require.NoError(t, err)
	return builder
}

// loweredObject returns the object definition typeName lowered to.
func loweredObject(t *testing.T, b SchemaBuilder, typeName string) *typegrammar.Object {
	t.Helper()
	def := requireDefinition(t, b.lowered.defs, typeName)
	object, ok := def.Type.(*typegrammar.Object)
	require.True(t, ok, "%s lowered to %T, not an object", typeName, def.Type)
	return object
}

// loweredUnion returns the union a field of owner lowers to, whichever of
// the three admitted interface forms the field takes.
func loweredUnion(t *testing.T, b SchemaBuilder, owner, jsonName string) typegrammar.Union {
	t.Helper()
	field := requireField(t, loweredObject(t, b, owner).Fields, jsonName)
	switch value := field.Value.(type) {
	case *typegrammar.Union:
		return *value
	case *typegrammar.OptionalUnion:
		return value.Union
	case *typegrammar.UnionSlice:
		return value.Union
	}
	require.FailNow(t, "not a union field", "%s.%s lowered to %T", owner, jsonName, field.Value)
	return typegrammar.Union{}
}

// variantTags lists a union's discriminator values in variant order.
func variantTags(u typegrammar.Union) []string {
	tags := make([]string, 0, len(u.Variants))
	for _, variant := range u.Variants {
		tags = append(tags, variant.Tag)
	}
	return tags
}

// loweredEnum resolves the enum a field carries, through Optional and
// Nullable wrappers, collections and references.
func loweredEnum(t *testing.T, b SchemaBuilder, value typegrammar.FieldValue) *typegrammar.Enum {
	t.Helper()
	var typ typegrammar.Type
	switch field := value.(type) {
	case *typegrammar.Required:
		typ = field.Type
	case *typegrammar.Optional:
		typ = field.Type
	case *typegrammar.Nullable:
		typ = field.Type
	default:
		require.FailNow(t, "not a typed field", "%T", value)
	}
	for {
		switch n := typ.(type) {
		case *typegrammar.Enum:
			return n
		case *typegrammar.Slice:
			typ = n.Element
		case *typegrammar.Array:
			typ = n.Element
		case *typegrammar.Pointer:
			typ = n.Element
		case *typegrammar.Ref:
			typ = requireDefinition(t, b.lowered.defs, n.Target.Name).Type
		default:
			require.FailNow(t, "no enum reachable", "%T", typ)
		}
	}
}
