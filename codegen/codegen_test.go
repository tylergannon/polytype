package codegen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
	"github.com/tylergannon/polytype/internal/testutils"
)

func TestProgrammaticGenerationSelectsOutputsWithoutRegistration(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	t.Run("transport outputs do not generate schema", func(t *testing.T) {
		fixture := copyProgrammaticFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/transport")

		require.FileExists(t, filepath.Join(fixture, "generated", "typescript", "types.ts"))
		require.FileExists(t, filepath.Join(fixture, "generated", "typescript", "index.ts"))
		require.FileExists(t, filepath.Join(fixture, "generated", "codec", "codec_gen.go"))
		typescriptSource, err := os.ReadFile(filepath.Join(fixture, "generated", "typescript", "types.ts"))
		require.NoError(t, err)
		require.Contains(t, string(typescriptSource), `"kind": "http_event_created"`)
		devalueSource, err := os.ReadFile(filepath.Join(fixture, "generated", "codec", "codec_gen.go"))
		require.NoError(t, err)
		require.Contains(t, string(devalueSource), `case "http_event_created":`)
		_, err = os.Stat(filepath.Join(fixture, "model", "jsonschema"))
		require.ErrorIs(t, err, os.ErrNotExist)
		_, err = os.Stat(filepath.Join(fixture, "model", "jsonschema_gen.go"))
		require.ErrorIs(t, err, os.ErrNotExist)
		runGo(t, fixture, "test", "./generated/codec")
	})

	t.Run("schema files do not require a schema method", func(t *testing.T) {
		fixture := copyProgrammaticFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/schema")

		require.FileExists(t, filepath.Join(fixture, "model", "jsonschema", "Envelope.json"))
		schema, err := os.ReadFile(filepath.Join(fixture, "model", "jsonschema", "Envelope.json"))
		require.NoError(t, err)
		require.Contains(t, string(schema), `"const": "httpEventCreated"`)
		_, err = os.Stat(filepath.Join(fixture, "model", "jsonschema_gen.go"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Go JSON codecs do not generate schema", func(t *testing.T) {
		fixture := copyProgrammaticFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/gojson")

		require.FileExists(t, filepath.Join(fixture, "model", "polytype_gen.go"))
		require.NoFileExists(t, filepath.Join(fixture, "model", "jsonschema_gen.go"))
		_, err := os.Stat(filepath.Join(fixture, "model", "jsonschema"))
		require.ErrorIs(t, err, os.ErrNotExist)
		runGo(t, fixture, "test", "./model")
	})
}

func TestRecursiveTypesGenerateCodecsWithoutSchema(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	t.Run("GoJSON+TypeScript+Devalue succeeds", func(t *testing.T) {
		fixture := copyRecursiveFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/generate")

		require.FileExists(t, filepath.Join(fixture, "model", "polytype_gen.go"))
		require.NoFileExists(t, filepath.Join(fixture, "model", "jsonschema_gen.go"))
		require.FileExists(t, filepath.Join(fixture, "generated", "typescript", "types.ts"))
		require.FileExists(t, filepath.Join(fixture, "generated", "codec", "codec_gen.go"))
		_, err := os.Stat(filepath.Join(fixture, "model", "jsonschema"))
		require.ErrorIs(t, err, os.ErrNotExist)
		runGo(t, fixture, "test", "./generated/codec")
	})

	t.Run("repeated generation is byte-identical", func(t *testing.T) {
		fixture := copyRecursiveFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/generate")

		read := func(rel string) []byte {
			t.Helper()
			data, err := os.ReadFile(filepath.Join(fixture, rel))
			require.NoError(t, err)
			return data
		}
		goJSON1 := read("model/polytype_gen.go")
		ts1 := read("generated/typescript/types.ts")
		dv1 := read("generated/codec/codec_gen.go")
		committed, err := os.ReadFile(filepath.Join("testdata", "recursive", "generated", "codec", "codec_gen.go"))
		require.NoError(t, err)
		require.Equal(t, string(committed), string(dv1), "committed devalue snapshot differs from generator output")

		runGo(t, fixture, "run", "./cmd/generate")

		require.Equal(t, goJSON1, read("model/polytype_gen.go"), "Go JSON output changed")
		require.Equal(t, ts1, read("generated/typescript/types.ts"), "TypeScript output changed")
		require.Equal(t, dv1, read("generated/codec/codec_gen.go"), "devalue output changed")
	})

	t.Run("TypeScript compiles with strict mode", func(t *testing.T) {
		tsc := filepath.Join(repoRoot, "node_modules", ".bin", "tsc")
		if _, statErr := os.Stat(tsc); statErr != nil {
			t.Skip("tsc not installed (run npm ci at repo root)")
		}
		fixture := copyRecursiveFixture(t, repoRoot)
		runGo(t, fixture, "run", "./cmd/generate")
		exit, stdout, stderr, err := testutils.RunCommand(
			tsc, fixture, "--strict", "--noEmit",
			filepath.Join(fixture, "generated", "typescript", "types.ts"),
		)
		require.NoError(t, err)
		require.Equal(t, 0, exit, fmt.Sprintf("tsc failed:\n%s\n%s", stdout, stderr))
	})

	t.Run("JSONSchema rejects recursive types before writing", func(t *testing.T) {
		fixture := copyRecursiveFixture(t, repoRoot)
		exit, _, stderr, err := testutils.RunCommand("go", fixture, "run", "./cmd/schema")
		require.NoError(t, err)
		require.NotEqual(t, 0, exit, "expected schema generation to fail for recursive types")
		require.Contains(t, stderr, "JSON Schema cannot express the recursive type model.")
		_, statErr := os.Stat(filepath.Join(fixture, "model", "jsonschema"))
		require.ErrorIs(t, statErr, os.ErrNotExist, "schema directory should not be created")
	})
}

// TestGenWithDeclarationFilesPresent runs the generator program from issue
// #129 (testdata/recursive_declarations/gen) against packages whose
// declaration files hold Declare[Tree](), a Compose call, and
// Declare(Tree.Schema). A programmatic configuration does not read them, so
// every output succeeds beside each one: the TypeScript compiles in strict
// mode, and the Go JSON and devalue codecs round-trip the issue's value. JSON
// Schema alone fails, once, naming the output and the option that avoids it.
func TestGenWithDeclarationFilesPresent(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	fixture := copyNamedFixture(t, repoRoot, "testdata/recursive_declarations")
	tsc := filepath.Join(repoRoot, "node_modules", ".bin", "tsc")
	gen := filepath.Join(t.TempDir(), "gen")
	runGo(t, fixture, "build", "-o", gen, "./gen")
	run := func(t *testing.T, target, out string) (int, string) {
		t.Helper()
		t.Setenv("TARGET", target)
		t.Setenv("OUT", out)
		exit, stdout, stderr, err := testutils.RunCommand(gen, fixture)
		require.NoError(t, err)
		return exit, stdout + stderr
	}

	for _, target := range []string{"noarg", "compose", "entrypoint"} {
		t.Run(target, func(t *testing.T) {
			for _, out := range []string{"", "ts", "devalue"} {
				exit, output := run(t, target, out)
				require.Equal(t, 0, exit, "OUT=%q: %s", out, output)
			}
			pkg := filepath.Join(fixture, target)
			codec, err := os.ReadFile(filepath.Join(pkg, "polytype_gen.go"))
			require.NoError(t, err)
			require.Contains(t, string(codec), `"kind"`)
			require.NotContains(t, string(codec), "errNoDiscriminator")
			require.NoFileExists(t, filepath.Join(pkg, "jsonschema_gen.go"))
			require.NoDirExists(t, filepath.Join(pkg, "jsonschema"))
			require.FileExists(t, filepath.Join(pkg, "devalue_gen.go"))
			types, err := os.ReadFile(filepath.Join(pkg, "ts", "types.ts"))
			require.NoError(t, err)
			require.Contains(t, string(types), `"kind": "branch";`)
			require.Contains(t, string(types), `"kind": "leaf";`)
			t.Run("TypeScript compiles with strict mode", func(t *testing.T) {
				if _, statErr := os.Stat(tsc); statErr != nil {
					t.Skip("tsc not installed (run npm ci at repo root)")
				}
				exit, stdout, stderr, err := testutils.RunCommand(tsc, fixture, "--strict", "--noEmit", filepath.Join(pkg, "ts", "types.ts"))
				require.NoError(t, err)
				require.Equal(t, 0, exit, fmt.Sprintf("tsc failed:\n%s\n%s", stdout, stderr))
			})

			exit, output := run(t, target, "schema")
			require.NotEqual(t, 0, exit, output)
			require.Equal(t, 1, strings.Count(output, "JSON Schema cannot express the recursive type "+target+".Node"), output)
			require.Contains(t, output, "Only the JSON Schema output has this limit")
			require.Contains(t, output, "select codegen.GoJSON() instead of codegen.JSONSchema()")
			require.NotContains(t, output, "rendering struct field")
		})
	}
	// The Go JSON and devalue codecs generated through codegen.Gen round-trip
	// the issue's value.
	runGo(t, fixture, "test", "./...")
}

// TestGenWithoutOutputNamesDeclaredType proves a declaration without a schema
// entrypoint and no selected output is rejected by name.
func TestGenWithoutOutputNamesDeclaredType(t *testing.T) {
	type Tree struct{}
	err := codegen.Gen(polytype.Declare[Tree]())
	require.ErrorContains(t, err, "no output selected for Tree")
	require.ErrorContains(t, err, "must select at least one output")
}

func TestCodecDiscoveryPackageQualifiedCollision(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	fixture := copyNamedFixture(t, repoRoot, "testdata/collision")
	runGo(t, fixture, "run", "./cmd/generate")
	require.FileExists(t, filepath.Join(fixture, "model", "polytype_gen.go"))

	exit, stdout, stderr, err := testutils.RunCommand("go", fixture, "run", "./cmd/prove")
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("prove failed (discriminators absent):\n%s\n%s", stdout, stderr))
	require.Contains(t, stdout, "ok")
}

func TestCodecDiscoveryNestedErrorPropagation(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	fixture := copyNamedFixture(t, repoRoot, "testdata/discovery_error")
	exit, stdout, stderr, err := testutils.RunCommand("go", fixture, "run", "./cmd/prove")
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("prove failed:\n%s\n%s", stdout, stderr))
	require.Contains(t, stdout, "generation correctly rejected")
}

func TestRecursiveEmbeddingTerminates(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	fixture := copyNamedFixture(t, repoRoot, "testdata/recursive_embed")
	exit, stdout, stderr, err := testutils.RunCommand("go", fixture, "run", "./cmd/prove")
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("prove failed (recursive embedding):\n%s\n%s", stdout, stderr))
	require.Contains(t, stdout, "generation correctly rejected recursive embedding")
}

func TestRecursiveDevalueJSInterop(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	devalueJS := filepath.Join(repoRoot, "node_modules", "devalue", "index.js")
	if _, statErr := os.Stat(devalueJS); statErr != nil {
		t.Skip("devalue JS not installed (run npm ci at repo root)")
	}
	exit, _, _, err := testutils.RunCommand("node", ".", "--version")
	if err != nil || exit != 0 {
		t.Skip("node not available")
	}

	fixture := copyRecursiveFixture(t, repoRoot)
	runGo(t, fixture, "run", "./cmd/generate")

	tmpDir := t.TempDir()
	goWireFile := filepath.Join(tmpDir, "go-wire.json")
	jsWireFile := filepath.Join(tmpDir, "js-wire.json")
	goConsumedFile := filepath.Join(tmpDir, "go-consumed.json")

	// Step 1: Go emits devalue + JSON wire
	exit, goWire, stderr, err := testutils.RunCommand("go", fixture, "run", "./cmd/interop", "emit")
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("emit:\n%s", stderr))
	require.NoError(t, os.WriteFile(goWireFile, []byte(goWire), 0o644))

	// Step 2: JS parses Go devalue, verifies JSON wire, emits JS devalue
	jsInterop := fmt.Sprintf(`
import fs from 'node:fs';
import assert from 'node:assert/strict';
import { parse, stringify } from %q;

const expectedTree = {name:'root', children:[{name:'child', children:[{name:'grandchild', children:[], parent:null}], parent:null}], parent:null};
const expectedDocument = {title:'interop', content:[{text:'intro', type:'Paragraph'}, {heading:'chapter', children:[{text:'body', type:'Paragraph'}, {heading:'sub', children:[], type:'Section'}], type:'Section'}]};

const go = JSON.parse(fs.readFileSync(%q, 'utf8'));
assert.deepStrictEqual(parse(go.tree_devalue), expectedTree);
assert.deepStrictEqual(parse(go.document_devalue), expectedDocument);
assert.deepStrictEqual(JSON.parse(JSON.stringify(JSON.parse(JSON.stringify(go.document_json)))), expectedDocument);

fs.writeFileSync(%q, JSON.stringify({tree_devalue:stringify(expectedTree), document_devalue:stringify(expectedDocument)}));
console.log('js-ok');
`, devalueJS, goWireFile, jsWireFile)

	jsFile := filepath.Join(tmpDir, "interop.mjs")
	require.NoError(t, os.WriteFile(jsFile, []byte(jsInterop), 0o644))

	exit, stdout, stderr, err := testutils.RunCommand("node", ".", jsFile)
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("JS interop failed:\n%s\n%s", stdout, stderr))
	require.Contains(t, stdout, "js-ok")

	// Step 3: Go consumes JS devalue wire via shell pipe
	consumeScript := filepath.Join(tmpDir, "consume.sh")
	require.NoError(t, os.WriteFile(consumeScript, []byte(fmt.Sprintf(
		"cd %q && go run ./cmd/interop consume < %q > %q",
		fixture, jsWireFile, goConsumedFile,
	)), 0o644))
	exit, _, stderr, err = testutils.RunCommand("bash", ".", consumeScript)
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("Go consume failed:\n%s", stderr))

	// Step 4: Verify consumed values match expected
	jsVerify := fmt.Sprintf(`
import fs from 'node:fs';
import assert from 'node:assert/strict';

const expectedTree = {name:'root', children:[{name:'child', children:[{name:'grandchild', children:[], parent:null}], parent:null}], parent:null};
const expectedDocument = {title:'interop', content:[{text:'intro', type:'Paragraph'}, {heading:'chapter', children:[{text:'body', type:'Paragraph'}, {heading:'sub', children:[], type:'Section'}], type:'Section'}]};

const got = JSON.parse(fs.readFileSync(%q, 'utf8'));
assert.deepStrictEqual(got.tree, expectedTree);
assert.deepStrictEqual(got.document, expectedDocument);
console.log('verify-ok');
`, goConsumedFile)

	jsVerifyFile := filepath.Join(tmpDir, "verify.mjs")
	require.NoError(t, os.WriteFile(jsVerifyFile, []byte(jsVerify), 0o644))
	exit, stdout, stderr, err = testutils.RunCommand("node", ".", jsVerifyFile)
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("verify failed:\n%s\n%s", stdout, stderr))
	require.Contains(t, stdout, "verify-ok")
}

func copyNamedFixture(t *testing.T, repoRoot, src string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, testutils.CopyDir(src, dir))
	goMod := filepath.Join(dir, "go.mod")
	contents, err := os.ReadFile(goMod)
	require.NoError(t, err)
	rewritten := strings.Replace(string(contents), "=> ../../../", "=> "+repoRoot, 1)
	require.NotEqual(t, string(contents), rewritten)
	require.NoError(t, os.WriteFile(goMod, []byte(rewritten), 0o644))
	runGo(t, dir, "mod", "tidy")
	return dir
}

func copyRecursiveFixture(t *testing.T, repoRoot string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, testutils.CopyDir("testdata/recursive", dir))
	goMod := filepath.Join(dir, "go.mod")
	contents, err := os.ReadFile(goMod)
	require.NoError(t, err)
	rewritten := strings.Replace(string(contents), "=> ../../../", "=> "+repoRoot, 1)
	require.NotEqual(t, string(contents), rewritten)
	require.NoError(t, os.WriteFile(goMod, []byte(rewritten), 0o644))
	runGo(t, dir, "mod", "tidy")
	return dir
}

func copyProgrammaticFixture(t *testing.T, repoRoot string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, testutils.CopyDir("testdata/programmatic", dir))
	goMod := filepath.Join(dir, "go.mod")
	contents, err := os.ReadFile(goMod)
	require.NoError(t, err)
	rewritten := strings.Replace(string(contents), "=> ../../../", "=> "+repoRoot, 1)
	require.NotEqual(t, string(contents), rewritten)
	require.NoError(t, os.WriteFile(goMod, []byte(rewritten), 0o644))
	runGo(t, dir, "mod", "tidy")
	return dir
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	exit, stdout, stderr, err := testutils.RunCommand("go", dir, args...)
	require.NoError(t, err)
	require.Equal(t, 0, exit, fmt.Sprintf("go %v:\n%s\n%s", args, stdout, stderr))
}
