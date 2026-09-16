package builder

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// recursiveTypes are the types from issue #129: a sealed union that recurses
// through a slice, and a directly self-recursive struct.
const recursiveTypes = `package fixture

type Node interface{ node() }

type Leaf struct {
	Name string ` + "`json:\"name\"`" + `
}

func (Leaf) node() {}

type Branch struct {
	Name string ` + "`json:\"name\"`" + `
	Body []Node ` + "`json:\"body\"`" + `
}

func (Branch) node() {}

type Watcher struct {
	Name     string    ` + "`json:\"name\"`" + `
	Watchers []Watcher ` + "`json:\"watchers\"`" + `
}

type Tree struct {
	Body     []Node    ` + "`json:\"body\"`" + `
	Watchers []Watcher ` + "`json:\"watchers\"`" + `
}
`

// declarationFile returns a jsonschema-tagged file whose var block starts on
// line 7.
func declarationFile(decls string) string {
	return "//go:build jsonschema\n\npackage fixture\n\nimport \"github.com/tylergannon/polytype\"\n\n" + decls
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func TestDeclarationFileRejectsNonMarkerCallsBeforeWriting(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ decls, want string }{
		"Compose": {
			decls: "var _ = polytype.Compose(\n\tpolytype.Declare[Tree](),\n\tpolytype.SealedUnion[Node](\"kind\"),\n)\n",
			want:  "polytype.Compose at ",
		},
		"other polytype function": {
			decls: "var _ = polytype.Snake(\"Tree\")\n",
			want:  "polytype.Snake at ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeMultiFileFixture(t, map[string]string{"types.go": recursiveTypes, "schema.go": declarationFile(test.decls)})
			before := dirNames(t, dir)
			err := Run(BuilderArgs{TargetDir: dir, Validate: true})
			require.ErrorContains(t, err, test.want)
			require.ErrorContains(t, err, "schema.go:7:9")
			require.Equal(t, before, dirNames(t, dir))
		})
	}
}

// TestOrdinaryFileBlankDeclarationIsRefusedBeforeWriting proves a
// declaration written in ordinary Go fails instead of being ignored (#151).
// A blank var cannot be configuration, so the author meant it for the CLI;
// before this refusal, moving past v1.0.1 silently dropped it, which changed
// discriminators and deleted generated output.
func TestOrdinaryFileBlankDeclarationIsRefusedBeforeWriting(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ decls, want string }{
		"Declare with an entrypoint": {
			decls: "var _ = polytype.Declare(Watcher.Schema)\n\nfunc (Watcher) Schema() json.RawMessage { panic(\"x\") }\n",
			want:  "polytype.Declare at ",
		},
		"Declare without an entrypoint": {
			decls: "var _ = polytype.Declare[Watcher]()\n",
			want:  "polytype.Declare at ",
		},
		"chained Declare": {
			decls: "var _ = polytype.Declare(Watcher.Schema).RenderProviders()\n\nfunc (Watcher) Schema() json.RawMessage { panic(\"x\") }\n",
			want:  "polytype.Declare at ",
		},
		"SealedUnion": {
			decls: "var _ = polytype.SealedUnion[Node](\"kind\")\n",
			want:  "polytype.SealedUnion at ",
		},
		"legacy method marker": {
			decls: "var _ = polytype.NewJSONSchemaMethod(Watcher.Schema)\n\nfunc (Watcher) Schema() json.RawMessage { panic(\"x\") }\n",
			want:  "polytype.NewJSONSchemaMethod at ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := writeMultiFileFixture(t, map[string]string{
				"types.go":  recursiveTypes,
				"config.go": "package fixture\n\nimport (\n\t\"encoding/json\"\n\n\t\"github.com/tylergannon/polytype\"\n)\n\nvar _ json.RawMessage\n\n" + test.decls,
				"schema.go": declarationFile("var _ = polytype.Declare[Tree]()\n"),
			})
			before := dirNames(t, dir)
			err := Run(BuilderArgs{TargetDir: dir})
			require.ErrorContains(t, err, test.want)
			require.ErrorContains(t, err, "config.go:11:9")
			require.ErrorContains(t, err, "//go:build jsonschema")
			require.Equal(t, before, dirNames(t, dir))
		})
	}
}

// ordinaryConfiguration is executable configuration in an ordinary Go file,
// as a generator program would import it: a Compose value, a root, and a
// sealed union whose inflector only executable configuration may supply.
const ordinaryConfiguration = `package fixture

import (
	"strings"

	"github.com/tylergannon/polytype"
)

type Other struct {
	Name string ` + "`json:\"name\"`" + `
}

var OtherConfig = polytype.Declare[Other]()

var NodeUnion = polytype.SealedUnion[Node]("tag", strings.ToLower)

var Config = polytype.Compose(polytype.Declare[Tree](), NodeUnion)
`

// TestOrdinaryFileIsNotReadAsDeclarations proves only a declaration file is
// read as declarations: configuration values in ordinary Go neither fail the
// CLI nor join its roots or unions.
func TestOrdinaryFileIsNotReadAsDeclarations(t *testing.T) {
	t.Parallel()
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go":  recursiveTypes,
		"config.go": ordinaryConfiguration,
		"schema.go": declarationFile("var _ = polytype.Declare[Tree]()\n"),
	})
	require.NoError(t, Run(BuilderArgs{TargetDir: dir, TypeScriptDir: filepath.Join(dir, "ts")}))
	generated, err := os.ReadFile(filepath.Join(dir, "ts", "types.ts"))
	require.NoError(t, err)
	require.NotContains(t, string(generated), "Other")
	require.Contains(t, string(generated), `"type": "Branch";`)
}

func TestEntrypointlessDeclarationGeneratesCodecsWithoutSchema(t *testing.T) {
	t.Parallel()
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go":  recursiveTypes,
		"schema.go": declarationFile("var _ = polytype.Declare[Tree]()\nvar _ = polytype.SealedUnion[Node](\"kind\", polytype.Snake)\n"),
	})
	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))
	require.Equal(t, []string{"go.mod", "polytype_gen.go", "schema.go", "types.go"}, dirNames(t, dir))

	generated, err := os.ReadFile(filepath.Join(dir, "polytype_gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(generated), "func (b Branch) MarshalJSON() ([]byte, error) {")
	require.Contains(t, string(generated), "func (t *Tree) UnmarshalJSON(data []byte) (err error) {")
	require.Contains(t, string(generated), `case "branch":`)
	require.NotContains(t, string(generated), "errNoDiscriminator")
	require.NotContains(t, string(generated), "go:embed")
}

func TestEntrypointlessDeclarationRejectsSchemaOnlyOptions(t *testing.T) {
	t.Parallel()
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go":  recursiveTypes,
		"schema.go": declarationFile("var _ = polytype.Declare[Tree]()\n"),
	})
	err := Run(BuilderArgs{TargetDir: dir, Validate: true})
	require.ErrorContains(t, err, "--validate cannot generate ValidateJSON for Tree: it is declared without a schema entrypoint")
	require.Equal(t, []string{"go.mod", "schema.go", "types.go"}, dirNames(t, dir))

	dir = writeMultiFileFixture(t, map[string]string{
		"types.go":  recursiveTypes,
		"schema.go": declarationFile("var _ = polytype.Declare[Tree]().RenderProviders()\n"),
	})
	err = Run(BuilderArgs{TargetDir: dir})
	require.ErrorContains(t, err, "polytype.Declare[Tree]().RenderProviders renders a JSON Schema and requires a schema entrypoint")
	require.ErrorContains(t, err, "schema.go:7:9")
}

// TestEntrypointlessRootIsLoweredWhateverItsType proves a Declare[T]() root
// whose type cannot carry a method still reaches TypeScript: a pointer type
// is emitted, and an interface fails as a codegen root does instead of being
// silently dropped.
func TestEntrypointlessRootIsLoweredWhateverItsType(t *testing.T) {
	t.Parallel()
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go":  recursiveTypes + "\ntype TreePtr *Tree\n",
		"schema.go": declarationFile("var _ = polytype.Declare[TreePtr]()\n"),
	})
	require.NoError(t, Run(BuilderArgs{TargetDir: dir, TypeScriptDir: filepath.Join(dir, "ts")}))
	generated, err := os.ReadFile(filepath.Join(dir, "ts", "types.ts"))
	require.NoError(t, err)
	require.Contains(t, string(generated), "export type TreePtr = Tree;")

	dir = writeMultiFileFixture(t, map[string]string{
		"types.go":  recursiveTypes,
		"schema.go": declarationFile("var _ = polytype.Declare[Node]()\n"),
	})
	err = Run(BuilderArgs{TargetDir: dir, TypeScriptDir: filepath.Join(dir, "ts")})
	require.ErrorContains(t, err, "registered interface example.com/typegrammarfixture.Node is valid only as a configured direct field")
	require.Equal(t, []string{"go.mod", "schema.go", "types.go"}, dirNames(t, dir))
}

// TestSwitchingSchemaOutputReplacesGeneratedFile proves the generated file
// follows the declaration between schema and codec-only output without ever
// leaving both files, whose methods would collide, and without removing
// another generator's file of the same name.
func TestSwitchingSchemaOutputReplacesGeneratedFile(t *testing.T) {
	t.Parallel()
	const types = `package fixture

type Shape interface{ shape() }

type Circle struct {
	Radius int ` + "`json:\"radius\"`" + `
}

func (Circle) shape() {}

type Drawing struct {
	Shapes []Shape ` + "`json:\"shapes\"`" + `
}
`
	withSchema := "//go:build jsonschema\n\npackage fixture\n\nimport (\n\t\"encoding/json\"\n\n\t\"github.com/tylergannon/polytype\"\n)\n\nfunc (Drawing) Schema() json.RawMessage { panic(\"not implemented\") }\n\nvar _ = polytype.Declare(Drawing.Schema)\n"
	dir := writeMultiFileFixture(t, map[string]string{"types.go": types, "schema.go": withSchema})

	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))
	require.Equal(t, []string{"go.mod", "jsonschema", "jsonschema_gen.go", "schema.go", "types.go"}, dirNames(t, dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.go"), []byte(declarationFile("var _ = polytype.Declare[Drawing]()\n")), 0o644))
	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))
	require.Equal(t, []string{"go.mod", "jsonschema", "polytype_gen.go", "schema.go", "types.go"}, dirNames(t, dir))
	require.Empty(t, dirNames(t, filepath.Join(dir, "jsonschema")), "the orphaned schema and checksum are pruned")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.go"), []byte(withSchema), 0o644))
	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))
	require.Equal(t, []string{"go.mod", "jsonschema", "jsonschema_gen.go", "schema.go", "types.go"}, dirNames(t, dir))

	other := "// Code generated by some-other-tool. DO NOT EDIT.\n\npackage fixture\n\nconst OtherToolOutput = \"keep me\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "jsonschema_gen.go"), []byte(other), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.go"), []byte(declarationFile("var _ = polytype.Declare[Drawing]()\n")), 0o644))
	require.NoError(t, Run(BuilderArgs{TargetDir: dir}))
	kept, err := os.ReadFile(filepath.Join(dir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.Equal(t, other, string(kept))
	require.FileExists(t, filepath.Join(dir, "polytype_gen.go"))
}

func TestRecursiveSchemaErrorNamesTheOutputOnce(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ types, cycle string }{
		"sealed union through a slice": {types: recursiveTypes, cycle: "fixture.Node"},
		"self-recursive struct": {
			types: strings.Replace(recursiveTypes, "Body     []Node    `json:\"body\"`\n", "", 1),
			cycle: "fixture.Watcher",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeMultiFileFixture(t, map[string]string{
				"types.go":  test.types,
				"schema.go": "//go:build jsonschema\n\npackage fixture\n\nimport (\n\t\"encoding/json\"\n\n\t\"github.com/tylergannon/polytype\"\n)\n\nfunc (Tree) Schema() json.RawMessage { panic(\"not implemented\") }\n\nvar _ = polytype.Declare(Tree.Schema)\n",
			})
			err := Run(BuilderArgs{TargetDir: dir})
			require.Error(t, err)
			message := err.Error()
			require.True(t, strings.HasPrefix(message, "JSON Schema cannot express the recursive type "+test.cycle+" ("), message)
			require.Equal(t, 1, strings.Count(message, "cannot express"), message)
			require.Contains(t, message, "reached from root Tree")
			require.Contains(t, message, "Only the JSON Schema output has this limit; Go JSON codecs, TypeScript and devalue support recursive types and are unaffected")
			require.Contains(t, message, "select codegen.GoJSON() instead of codegen.JSONSchema()")
			require.Contains(t, message, "polytype.Declare[Tree]()")
		})
	}
}

// TestProgrammaticLoadIgnoresRootDeclarations proves a programmatic run takes
// its roots only from its configuration: declaration-file roots, even a
// Compose call the CLI rejects, neither fail nor join it, and neither does
// configuration in ordinary Go, while a declaration file's SealedUnion marker
// still sets its interface's discriminator.
func TestProgrammaticLoadIgnoresRootDeclarations(t *testing.T) {
	t.Parallel()
	dir := writeMultiFileFixture(t, map[string]string{
		"types.go":  recursiveTypes,
		"config.go": ordinaryConfiguration,
		"schema.go": declarationFile("var _ = polytype.Compose(polytype.Declare[Tree]())\n" +
			"var _ = polytype.Declare[Watcher]()\n" +
			"var _ = polytype.SealedUnion[Node](\"kind\")\n"),
	})
	b, err := LoadProgrammatic(dir, ProgrammaticConfig{
		Declarations: []ConfiguredDeclaration{{PackagePath: "example.com/typegrammarfixture", TypeName: "Tree"}},
	}, true, false)
	require.NoError(t, err)
	require.Len(t, b.Scan.SchemaMethods, 1)
	require.Equal(t, "Tree", b.Scan.SchemaMethods[0].Receiver.TypeName)
	require.Equal(t, "kind", b.Scan.Interfaces["Node"].Discriminator)
}
