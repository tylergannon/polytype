package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeSealedUnionDiscriminatorFixture writes a package with a sealed Animal
// union used from two owners, plus whatever build-tagged declarations the
// test supplies next to Declare(Zoo.Schema) and Declare(Shelter.Schema).
func writeSealedUnionDiscriminatorFixture(t *testing.T, types, declarations string) string {
	t.Helper()
	return newFixture(t, map[string]string{
		"types.go": "package fixture\n\n" + types,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Zoo) Schema() json.RawMessage     { panic("not implemented") }
func (Shelter) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.Declare(Zoo.Schema)
var _ = polytype.Declare(Shelter.Schema)
` + declarations + `
`,
	})
}

const sealedTwoOwnerTypes = sealedZooTypes + `
type Shelter struct {
	Residents []Animal ` + "`json:\"residents\"`" + `
}
`

// TestSealedUnionDiscriminatorAppliesToEveryUse proves that one
// SealedUnion[Animal]("kind") declaration changes the discriminator property
// for every use of Animal - a scalar field on one owner and a slice on
// another - while values stay the concrete type names, and that every
// owner's codec plan carries the declaration its helper reads (rendered in
// TestRenderGoCodeUnionHelpersUseTheDeclaredDiscriminator).
func TestSealedUnionDiscriminatorAppliesToEveryUse(t *testing.T) {
	builder := loadBuilder(t, writeSealedUnionDiscriminatorFixture(t, sealedTwoOwnerTypes, `var _ = polytype.SealedUnion[Animal]("kind")`))
	for _, use := range []struct{ owner, field string }{{"Zoo", "resident"}, {"Shelter", "residents"}} {
		union := loweredUnion(t, builder, use.owner, use.field)
		require.Equal(t, "kind", union.Discriminator, use.owner)
		require.Equal(t, []string{"Cat", "Dog"}, variantTags(union), use.owner)
		require.Equal(t, "kind", builder.ownerCodecs[use.owner].UnionFields[0].DiscPropName, use.owner)
	}
}

func TestSealedUnionNamedInflectionAppliesToEveryUse(t *testing.T) {
	builder := loadBuilder(t, writeSealedUnionDiscriminatorFixture(t, sealedTwoOwnerTypes, `var _ = polytype.SealedUnion[Animal]("kind", polytype.Snake)`))
	for _, use := range []struct{ owner, field string }{{"Zoo", "resident"}, {"Shelter", "residents"}} {
		union := loweredUnion(t, builder, use.owner, use.field)
		require.Equal(t, "kind", union.Discriminator, use.owner)
		require.Equal(t, []string{"cat", "dog"}, variantTags(union), use.owner)
		require.Equal(t, []string{"cat", "dog"}, variantTags(builder.ownerCodecs[use.owner].UnionFields[0].Union), "the codec switches on the inflected tags")
	}
}

// TestSealedUnionDiscriminatorDiagnosticsNameTheInterface covers every
// negative rule from issue #88 that lives in one package.
func TestSealedUnionDiscriminatorDiagnosticsNameTheInterface(t *testing.T) {
	for _, test := range []struct {
		name         string
		types        string
		declarations string
		want         []string
	}{
		{
			name:         "duplicate declaration",
			types:        sealedTwoOwnerTypes,
			declarations: "var _ = polytype.SealedUnion[Animal](\"kind\")\nvar _ = polytype.SealedUnion[Animal](\"other\")",
			want:         []string{"polytype.SealedUnion[Animal] at ", "duplicates the declaration at "},
		},
		{
			name: "declaration for a non-sealed interface",
			types: sealedTwoOwnerTypes + `
type Open interface { Speak() string }
`,
			declarations: `var _ = polytype.SealedUnion[Open]("kind")`,
			want:         []string{"polytype.SealedUnion[Open] at ", "interface Open at ", "is not sealed"},
		},
		{
			name:         "declaration for a non-interface type",
			types:        sealedTwoOwnerTypes,
			declarations: `var _ = polytype.SealedUnion[Dog]("kind")`,
			want:         []string{"polytype.SealedUnion[Dog] at ", "Dog is not an interface type"},
		},
		{
			name:         "non-literal argument",
			types:        sealedTwoOwnerTypes + "\nconst kindProperty = \"kind\"\n",
			declarations: `var _ = polytype.SealedUnion[Animal](kindProperty)`,
			want:         []string{"polytype.SealedUnion[Animal] at ", "must be a string literal"},
		},
		{
			name:         "invalid property name",
			types:        sealedTwoOwnerTypes,
			declarations: `var _ = polytype.SealedUnion[Animal]("")`,
			want:         []string{"polytype.SealedUnion[Animal] at ", "nonempty valid UTF-8 property name"},
		},
		{
			name:         "unsupported source inflector",
			types:        sealedTwoOwnerTypes + "\nfunc custom(name string) string { return name }\n",
			declarations: `var _ = polytype.SealedUnion[Animal]("kind", custom)`,
			want:         []string{"polytype.SealedUnion[Animal] at ", "arbitrary functions are supported by executable codegen configuration"},
		},
		{
			name: "payload collision with the custom name",
			types: `type Animal interface { isAnimal() }
type Dog struct {
	Kind string ` + "`json:\"kind\"`" + `
	Name string ` + "`json:\"name\"`" + `
}
func (Dog) isAnimal() {}
type Zoo struct { Resident Animal ` + "`json:\"resident\"`" + ` }
type Shelter struct { Residents []Animal ` + "`json:\"residents\"`" + ` }
`,
			declarations: `var _ = polytype.SealedUnion[Animal]("kind")`,
			want:         []string{"variant Dog of sealed interface Animal", `payload property "kind" that collides with the discriminator property`},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetDir := writeSealedUnionDiscriminatorFixture(t, test.types, test.declarations)
			err := Run(BuilderArgs{TargetDir: targetDir})
			require.Error(t, err)
			for _, want := range test.want {
				require.ErrorContains(t, err, want)
			}
			_, statErr := os.Stat(filepath.Join(targetDir, "jsonschema_gen.go"))
			require.True(t, os.IsNotExist(statErr), "generation must not write output on a SealedUnion diagnostic")
		})
	}
}

// TestSealedUnionDeclarationOutsideInterfacePackageIsRejected proves the
// same-package rule: a SealedUnion declaration for an interface declared in
// another package is a diagnostic naming the interface and the offending
// location.
func TestSealedUnionDeclarationOutsideInterfacePackageIsRejected(t *testing.T) {
	baseImport := fixtureModulePath

	moduleDir := newFixtureModule(t)
	writeFixturePackage(t, moduleDir, "animals", map[string]string{
		"types.go": "package animals\n\n" + sealedZooTypes,
	})
	targetDir := writeFixturePackage(t, moduleDir, "", map[string]string{
		"types.go": `package fixture

import animals "` + baseImport + `/animals"

type Owner struct {
	Resident animals.Animal ` + "`json:\"resident\"`" + `
}
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
	animals "` + baseImport + `/animals"
)

func (Owner) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.Declare(Owner.Schema)
var _ = polytype.SealedUnion[animals.Animal]("kind")
`,
	})

	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "polytype.SealedUnion[Animal] at ")
	require.ErrorContains(t, err, "must be declared in package "+baseImport+"/animals")
	require.ErrorContains(t, err, filepath.Join(targetDir, "schema.go"))
}
