package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeSealedUnionFixture writes a package whose only registration is a bare
// Declare(Zoo.Schema), so every union in it must be inferred from the
// interface's sealing method alone.
func writeSealedUnionFixture(t *testing.T, types string) string {
	t.Helper()
	return newFixture(t, map[string]string{
		"types.go": "package fixture\n\n" + types,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Zoo) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.Declare(Zoo.Schema)
`,
	})
}

const sealedZooTypes = `type Animal interface {
	isAnimal()
}

type Dog struct {
	Name string ` + "`json:\"name\"`" + `
}

func (Dog) isAnimal() {}

type Cat struct {
	Name string ` + "`json:\"name\"`" + `
}

func (*Cat) isAnimal() {}

type Zoo struct {
	Resident Animal ` + "`json:\"resident\"`" + `
}
`

// TestSealedUnionInferredFromSealingMethod is the issue #87 acceptance
// example and its membership-drift golden. A single Declare(Zoo.Schema)
// yields a union of Dog (value variant) and Cat (pointer variant)
// discriminated by "type" with the concrete type names as values, with no
// field-level declaration; the exact variant list is then pinned across an
// added implementation and a renamed one, so membership drift is visible in
// review. How each variant's receiver is constructed by the codec is pinned
// in TestRenderGoCodeUnionHelpersConstructEachVariantByReceiver.
func TestSealedUnionInferredFromSealingMethod(t *testing.T) {
	targetDir := writeSealedUnionFixture(t, sealedZooTypes)
	union := loweredUnion(t, loadBuilder(t, targetDir), "Zoo", "resident")
	require.Equal(t, "type", union.Discriminator)
	require.Equal(t, []string{"Cat", "Dog"}, variantTags(union))
	require.True(t, union.Variants[0].Pointer, "Cat seals through a pointer receiver")
	require.False(t, union.Variants[1].Pointer, "Dog seals through a value receiver")

	added := sealedZooTypes + `
type Bird struct {
	Wingspan int ` + "`json:\"wingspan\"`" + `
}

func (Bird) isAnimal() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "types.go"), []byte("package fixture\n\n"+added), 0o644))
	require.Equal(t, []string{"Bird", "Cat", "Dog"}, variantTags(loweredUnion(t, loadBuilder(t, targetDir), "Zoo", "resident")))

	renamed := strings.ReplaceAll(sealedZooTypes, "Dog", "Hound")
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "types.go"), []byte("package fixture\n\n"+renamed), 0o644))
	require.Equal(t, []string{"Cat", "Hound"}, variantTags(loweredUnion(t, loadBuilder(t, targetDir), "Zoo", "resident")))
}

// TestSealedUnionDiagnosticsNameTheType covers every negative rule from
// issue #87. Each diagnostic names the offending type or field and nothing
// is written.
func TestSealedUnionDiagnosticsNameTheType(t *testing.T) {
	for _, test := range []struct {
		name  string
		types string
		want  []string
	}{
		{
			name: "reachable non-sealed interface",
			types: `type Animal interface { Speak() string }
type Dog struct { Name string ` + "`json:\"name\"`" + ` }
func (Dog) Speak() string { return "woof" }
type Zoo struct { Resident Animal ` + "`json:\"resident\"`" + ` }
`,
			want: []string{"field Zoo.Resident at ", "interface Animal at ", "is not sealed"},
		},
		{
			name: "sealing method obtained by embedding",
			types: `type sealed interface { isAnimal() }
type Animal interface { sealed }
type Dog struct { Name string ` + "`json:\"name\"`" + ` }
func (Dog) isAnimal() {}
type Zoo struct { Resident Animal ` + "`json:\"resident\"`" + ` }
`,
			want: []string{"field Zoo.Resident at ", "interface Animal at ", "acquires its sealing method isAnimal by embedding"},
		},
		{
			name: "variant excluded for inheriting the sealing method",
			types: `type Animal interface { isAnimal() }
type Dog struct { Name string ` + "`json:\"name\"`" + ` }
func (Dog) isAnimal() {}
type Puppy struct {
	Dog
	Age int ` + "`json:\"age\"`" + `
}
type Zoo struct { Resident Animal ` + "`json:\"resident\"`" + ` }
`,
			want: []string{"type Puppy satisfies sealed interface Animal", "only through an embedded field and is excluded"},
		},
		{
			name: "zero supported variants",
			types: `type Animal interface { isAnimal() }
type Zoo struct { Resident Animal ` + "`json:\"resident\"`" + ` }
`,
			want: []string{"field Zoo.Resident at ", "sealed interface Animal at ", "has no supported variants"},
		},
		{
			name: "invalid direct candidate that does not implement the complete interface",
			types: `type Animal interface { isAnimal(); Speak() string }
type Dog struct { Name string ` + "`json:\"name\"`" + ` }
func (Dog) isAnimal() {}
func (*Dog) Speak() string { return "woof" }
type Zoo struct { Resident Animal ` + "`json:\"resident\"`" + ` }
`,
			want: []string{"type Dog declares the sealing method of Animal", "Dog does not implement the complete interface"},
		},
		{
			name: "embedded interface payload",
			types: `type Animal interface { isAnimal() }
type Dog struct { Name string ` + "`json:\"name\"`" + ` }
func (Dog) isAnimal() {}
type Zoo struct {
	Animal
	Name string ` + "`json:\"name\"`" + `
}
`,
			want: []string{"embedded interface Animal at ", "is unsupported as a payload"},
		},
		{
			name: "discriminator payload collision",
			types: `type Animal interface { isAnimal() }
type Dog struct {
	Type string ` + "`json:\"type\"`" + `
	Name string ` + "`json:\"name\"`" + `
}
func (Dog) isAnimal() {}
type Zoo struct { Resident Animal ` + "`json:\"resident\"`" + ` }
`,
			want: []string{"variant Dog of sealed interface Animal", `payload property "type" that collides with the discriminator property`},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetDir := writeSealedUnionFixture(t, test.types)
			err := Run(BuilderArgs{TargetDir: targetDir})
			require.Error(t, err)
			for _, want := range test.want {
				require.ErrorContains(t, err, want)
			}
			_, statErr := os.Stat(filepath.Join(targetDir, "jsonschema_gen.go"))
			require.True(t, os.IsNotExist(statErr), "generation must not write output on a sealed-union diagnostic")
		})
	}
}
