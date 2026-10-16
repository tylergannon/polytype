package codegen_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

func TestGenerationOutputResults(t *testing.T) {
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	for _, retained := range []bool{false, true} {
		name := "ordinary"
		if retained {
			name = "worker"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/results\n\ngo 1.27\n"), 0600))
			source := filepath.Join(dir, "model.go")
			write := func(s string) {
				t.Helper()
				require.NoError(t, os.WriteFile(source, []byte("package model\n"+s), 0600))
			}
			write("type Row struct{Name string `json:\"name\"`}\n")
			config := polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/results", Name: "Row"})
			opts := codegen.Options{TargetDir: dir, TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts"), Barrel: true}}
			run := codegen.GenerateResult
			var worker codegen.Worker
			defer func() { require.NoError(t, worker.Close()) }()
			if retained {
				run = worker.GenerateResult
			}
			check := func(want bool) {
				t.Helper()
				changed, err := run(config, opts)
				require.NoError(t, err)
				require.Equal(t, want, changed)
			}
			check(true)
			output := filepath.Join(dir, "ts", "types.ts")
			stamp := time.Unix(1234567890, 0)
			require.NoError(t, os.Chtimes(output, stamp, stamp))
			check(false)
			if retained {
				require.NoError(t, worker.Close())
				check(false)
			}
			opts.Force = true
			check(false)
			opts.Force = false
			info, err := os.Stat(output)
			require.NoError(t, err)
			require.True(t, info.ModTime().Equal(stamp), "equal output mtime changed")
			write("type Row struct{Name string `json:\"name\"`}\nfunc body()int{return 42}\n")
			check(false)
			info, err = os.Stat(output)
			require.NoError(t, err)
			require.True(t, info.ModTime().Equal(stamp))
			require.NoError(t, os.Remove(output))
			check(true)
			check(false)
			require.NoError(t, os.WriteFile(output, []byte("damaged"), 0600))
			check(true)
			write("type Row struct{Ready bool `json:\"ready\"`}\n")
			check(true)
			write("type Row struct{Ready bool `json:\"ready\"`}\nfunc broken(\n")
			failedChanged, failedErr := run(config, opts)
			require.Error(t, failedErr)
			require.False(t, failedChanged)
			write("type Row struct{Ready bool `json:\"ready\"`}\n")
			check(false)
			data, err := os.ReadFile(output)
			require.NoError(t, err)
			require.Contains(t, string(data), `"ready": boolean`)
			// The first file is repaired before the second output fails publication.
			require.NoError(t, os.Remove(output))
			barrel := filepath.Join(dir, "ts", "index.ts")
			require.NoError(t, os.Remove(barrel))
			require.NoError(t, os.Mkdir(barrel, 0700))
			changed, err := run(config, opts)
			require.Error(t, err)
			require.True(t, changed)
			require.NoError(t, os.Remove(barrel))
			check(true)
			check(false)
		})
	}
}

func TestOrdinarySchemaDevalueAndGoResults(t *testing.T) {
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	dir := copyProgrammaticFixture(t, root)
	config := polytype.DeclareType(polytype.TypeSpec{PackagePath: "programmaticfixture/model", Name: "Envelope"})
	// Fixture package identity is supplied literally, independently of generated output.
	opts := codegen.Options{TargetDir: filepath.Join(dir, "model"), JSONSchema: true, GoCode: true,
		TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts")},
		Devalue:    &codegen.DevalueOptions{File: filepath.Join(dir, "codec", "codec_gen.go"), PackageName: "codec", ImportPath: "programmaticfixture/model"}}
	run := func(want bool) {
		t.Helper()
		changed, err := codegen.GenerateResult(config, opts)
		require.NoError(t, err)
		require.Equal(t, want, changed)
	}
	run(true)
	paths := []string{filepath.Join(dir, "model", "jsonschema", "Envelope.json"), filepath.Join(dir, "model", "jsonschema", "Envelope.json.sum"), filepath.Join(dir, "model", "polytype_gen.go"), opts.Devalue.File, filepath.Join(dir, "ts", "types.ts")}
	stamp := time.Unix(1234567890, 0)
	for _, p := range paths {
		require.NoError(t, os.Chtimes(p, stamp, stamp))
	}
	run(false)
	opts.Force = true
	run(false)
	opts.Force = false
	for _, p := range paths {
		info, err := os.Stat(p)
		require.NoError(t, err)
		require.True(t, info.ModTime().Equal(stamp), p)
	}
	for _, p := range paths {
		require.NoError(t, os.Remove(p))
		run(true)
		run(false)
	}
	// A stale checksum must not hide damaged schema bytes.
	schema := paths[0]
	require.NoError(t, os.WriteFile(schema, []byte("damaged"), 0600))
	run(true)
	// Requesting fewer roots removes checksum-owned schemas, preserving unrelated files.
	orphan := filepath.Join(dir, "model", "jsonschema", "Obsolete.json")
	data, err := os.ReadFile(schema)
	require.NoError(t, err)
	sum, err := os.ReadFile(schema + ".sum")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(orphan, data, 0600))
	require.NoError(t, os.WriteFile(orphan+".sum", sum, 0600))
	run(true)
	require.NoFileExists(t, orphan)
	run(false)
}
