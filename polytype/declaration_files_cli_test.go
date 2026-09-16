package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/testutils"
)

// TestGenCommandDeclarationFilesForRecursiveTypes drives the built CLI over
// the declaration files from issue #129, each beside the issue's recursive
// types (codegen/testdata/recursive_declarations): the Compose call that used
// to exit 0 and break the build, the no-argument Declare[Tree]() that used to
// be rejected, and Declare(Tree.Schema), whose schema cannot be generated.
func TestGenCommandDeclarationFilesForRecursiveTypes(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)
	root := t.TempDir()
	module := filepath.Join(root, "recursive_declarations")
	require.NoError(t, testutils.CopyDir("../codegen/testdata/recursive_declarations", module))
	require.NoError(t, os.WriteFile(filepath.Join(module, "go.mod"), []byte(fmt.Sprintf(
		"module recursivedeclarations\n\ngo %s\n\nrequire github.com/tylergannon/polytype v0.0.0\n\nreplace github.com/tylergannon/polytype => %s\n",
		goDirective(t, repoRoot), repoRoot)), 0o644))
	runGo(t, module, "mod", "tidy")

	// generate runs the CLI in the package directory, as go:generate does.
	generate := func(t *testing.T, pkg string, args ...string) (int, string) {
		t.Helper()
		exit, stdout, stderr, err := testutils.RunCommand(cli, filepath.Join(module, pkg), args...)
		require.NoError(t, err)
		return exit, stdout + stderr
	}

	t.Run("Compose fails before writing anything", func(t *testing.T) {
		pkg := filepath.Join(module, "compose")
		runGo(t, module, "build", "./...")
		files, digests := names(t, pkg), snapshot(t, pkg)

		exit, output := generate(t, "compose", "--validate")
		require.NotEqual(t, 0, exit, output)
		require.Contains(t, output, "polytype.Compose at ")
		require.Contains(t, output, "compose/declare.go:7:9")

		require.Equal(t, files, names(t, pkg), "the failed run must not create a jsonschema directory or a generated file")
		require.Equal(t, digests, snapshot(t, pkg), "the failed run must not modify the package")
		runGo(t, module, "build", "./...")
	})

	t.Run("Declare[Tree]() generates codecs and TypeScript without a schema", func(t *testing.T) {
		pkg := filepath.Join(module, "noarg")
		files := names(t, pkg)
		exit, output := generate(t, "noarg", "--validate")
		require.NotEqual(t, 0, exit, output)
		require.Contains(t, output, "--validate cannot generate ValidateJSON for Tree")
		require.Equal(t, files, names(t, pkg), "a rejected run must not write")

		exit, output = generate(t, "noarg", "--typescript", "ts")
		require.Equal(t, 0, exit, output)
		require.Equal(t, []string{"declare.go", "polytype_gen.go", "roundtrip_test.go", "ts", "types.go"}, names(t, pkg))
		codec := string(read(t, filepath.Join(pkg, "polytype_gen.go")))
		require.NotContains(t, codec, "errNoDiscriminator")
		require.NotContains(t, codec, "go:embed")
		require.Contains(t, string(read(t, filepath.Join(pkg, "ts", "types.ts"))), `"kind": "branch"`)
		runGo(t, module, "test", "./noarg")
	})

	t.Run("GOFLAGS carrying the generation tag still reads declaration files", func(t *testing.T) {
		pkg := filepath.Join(module, "noarg")
		exit, stdout, stderr, err := testutils.RunCommandEnv(cli, pkg, []string{"GOFLAGS=-tags=jsonschema"}, "--typescript", "ts")
		require.NoError(t, err)
		require.Equal(t, 0, exit, stdout+stderr)
		require.FileExists(t, filepath.Join(pkg, "polytype_gen.go"))
		require.Contains(t, string(read(t, filepath.Join(pkg, "ts", "types.ts"))), `"kind": "branch"`,
			"the SealedUnion in the declaration file must still set the discriminator")
	})

	t.Run("a declaration in an ordinary file fails before writing", func(t *testing.T) {
		pkg := filepath.Join(module, "untagged")
		require.NoError(t, testutils.CopyDir(filepath.Join(module, "noarg"), pkg))
		for _, generated := range []string{"polytype_gen.go", "ts", "roundtrip_test.go"} {
			require.NoError(t, os.RemoveAll(filepath.Join(pkg, generated)))
		}
		for _, file := range []string{"types.go", "declare.go"} {
			path := filepath.Join(pkg, file)
			require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(read(t, path)), "package noarg", "package untagged")), 0o644))
		}
		// The union's discriminator moves into ordinary Go, where v1.0.2
		// through v1.0.3 silently ignored it and fell back to "type".
		declare := filepath.Join(pkg, "declare.go")
		require.NoError(t, os.WriteFile(declare, []byte(strings.ReplaceAll(string(read(t, declare)),
			"var _ = polytype.SealedUnion[Node](\"kind\", polytype.Snake)\n", "")), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(pkg, "union.go"), []byte(
			"package untagged\n\nimport \"github.com/tylergannon/polytype\"\n\nvar _ = polytype.SealedUnion[Node](\"kind\", polytype.Snake)\n"), 0o644))
		runGo(t, module, "build", "./untagged")
		files := names(t, pkg)

		exit, output := generate(t, "untagged", "--typescript", "ts")
		require.NotEqual(t, 0, exit, output)
		require.Contains(t, output, "polytype.SealedUnion at ")
		require.Contains(t, output, "untagged/union.go:5:9")
		require.Contains(t, output, "//go:build jsonschema")
		require.Equal(t, files, names(t, pkg), "a rejected run must not write")
	})

	t.Run("Declare(Tree.Schema) reports what JSON Schema cannot express", func(t *testing.T) {
		pkg := filepath.Join(module, "entrypoint")
		files := names(t, pkg)
		exit, output := generate(t, "entrypoint")
		require.NotEqual(t, 0, exit, output)
		require.Equal(t, 1, strings.Count(output, "JSON Schema cannot express the recursive type entrypoint.Node"), output)
		require.Contains(t, output, "Only the JSON Schema output has this limit")
		require.Contains(t, output, "codegen.GoJSON()")
		require.Contains(t, output, "polytype.Declare[Tree]()")
		require.NotContains(t, output, "rendering struct field")
		require.Equal(t, files, names(t, pkg))
	})
}
