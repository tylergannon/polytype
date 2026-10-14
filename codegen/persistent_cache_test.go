package codegen_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

type CachedRow struct{ CompiledField string }
type CachedTurn struct{ CompiledCount int }

func TestProgrammaticPersistentCache(t *testing.T) {
	if os.Getenv("POLYTYPE_CACHE_PROGRAMMATIC_CHILD") == "1" {
		dir := os.Getenv("POLYTYPE_CACHE_PROGRAMMATIC_DIR")
		config := polytype.Compose(polytype.Declare[CachedRow](), polytype.Declare[CachedTurn]())
		if os.Getenv("POLYTYPE_CACHE_PROGRAMMATIC_SELECTION") == "row" {
			config = polytype.Compose(polytype.Declare[CachedRow]())
		}
		outDir := "ts" + os.Getenv("POLYTYPE_CACHE_PROGRAMMATIC_OUTPUT_SUFFIX")
		err := codegen.Generate(config, codegen.Options{TargetDir: dir, TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, outDir)}})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	dir, cache := t.TempDir(), t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+reflect.TypeFor[CachedRow]().PkgPath()+"\n\ngo 1.27\n")
	write("types.go", "package codegen_test\n// CachedRow is projected from live source.\ntype CachedRow struct { Name string `json:\"name\"` }\ntype CachedTurn struct { Count int `json:\"count\"` }\n")
	run := func(want string, success bool, env ...string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestProgrammaticPersistentCache$")
		cmd.Env = append(os.Environ(), "POLYTYPE_CACHE_PROGRAMMATIC_CHILD=1", "POLYTYPE_CACHE_PROGRAMMATIC_DIR="+dir, "POLYTYPE_CACHE_DIR="+cache, "POLYTYPE_CACHE_TRACE=1", "GOMAXPROCS=2")
		cmd.Env = append(cmd.Env, env...)
		start := time.Now()
		out, err := cmd.CombinedOutput()
		t.Logf("%s wall=%s %s", want, time.Since(start), out)
		if (err == nil) != success || !bytes.Contains(out, []byte(want)) {
			t.Fatalf("want success=%v %q: %v %s", success, want, err, out)
		}
		if want == "hit" && bytes.Contains(out, []byte("polytype cache: load ")) {
			t.Fatalf("warm package load: %s", out)
		}
	}
	assert := func(expected string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "ts/types.ts"))
		if err != nil || !bytes.Contains(b, []byte(expected)) {
			t.Fatalf("missing %q: %v %s", expected, err, b)
		}
	}
	run("stored", true)
	assert(`"name": string`)
	assert(`"count": number`)
	assert("CachedRow is projected from live source.")
	run("hit", true)
	// Independent requests for one source package retain separate slots, while
	// changing only the publication directory reuses the same semantic request.
	run("stored", true, "POLYTYPE_CACHE_PROGRAMMATIC_SELECTION=row")
	assert(`"name": string`)
	rowOnly, err := os.ReadFile(filepath.Join(dir, "ts/types.ts"))
	if err != nil || bytes.Contains(rowOnly, []byte(`"count": number`)) {
		t.Fatalf("row-only request retained Turn: %v %s", err, rowOnly)
	}
	run("hit", true)
	assert(`"count": number`)
	run("hit", true, "POLYTYPE_CACHE_PROGRAMMATIC_SELECTION=row")
	run("hit", true, "POLYTYPE_CACHE_PROGRAMMATIC_OUTPUT_SUFFIX=-alternate")
	other, err := os.ReadFile(filepath.Join(dir, "ts-alternate/types.ts"))
	if err != nil || !bytes.Contains(other, []byte(`"count": number`)) {
		t.Fatalf("alternate output directory not restored: %v %s", err, other)
	}
	if err := os.RemoveAll(filepath.Join(dir, "ts")); err != nil {
		t.Fatal(err)
	}
	run("hit", true)
	assert(`"name": string`)
	assert(`"count": number`)
	write("types.go", "package codegen_test\ntype CachedRow struct { Active bool `json:\"active\"` }\ntype CachedTurn struct { Count int `json:\"count\"` }\n")
	run("stored", true)
	assert(`"active": boolean`)
	entries, err := filepath.Glob(filepath.Join(cache, "polytype-emission-v1", "*.json"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("want two request slots despite source revision and destination change: %v %v", entries, err)
	}
	// An unsupported live source shape must not be hidden by a stale emission.
	write("types.go", "package codegen_test\ntype CachedRow struct { Broken chan int }\ntype CachedTurn struct { Count int }\n")
	run("unhandled expression *dst.ChanType", false)
}
