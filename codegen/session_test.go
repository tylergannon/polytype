package codegen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
	"github.com/tylergannon/polytype/goload"
)

func TestSessionFollowsPersistentLookupAndSharesDistinctConfigurations(t *testing.T) {
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+reflect.TypeFor[CachedRow]().PkgPath()+"\n\ngo 1.27\n")
	write("types.go", "package codegen_test\ntype CachedRow struct { Name string `json:\"name\"` }; type CachedTurn struct { Count int `json:\"count\"` }\n")
	opts := codegen.Options{TargetDir: dir, TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts")}}
	both := polytype.Compose(polytype.Declare[CachedRow](), polytype.Declare[CachedTurn]())
	if err := codegen.Generate(both, opts); err != nil {
		t.Fatal(err)
	}
	var warm goload.Session
	opts.Session = &warm
	if err := os.RemoveAll(opts.TypeScript.Dir); err != nil {
		t.Fatal(err)
	}
	if err := codegen.Generate(both, opts); err != nil {
		t.Fatal(err)
	}
	if warm.Stats() != (goload.Stats{}) {
		t.Fatalf("persistent hit touched session: %+v", warm.Stats())
	}
	assert := func(want string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(opts.TypeScript.Dir, "types.ts"))
		if err != nil || !bytes.Contains(b, []byte(want)) {
			t.Fatalf("want literal %s: %v %s", want, err, b)
		}
	}
	assert(`"name": string`)
	assert(`"count": number`)
	var shared goload.Session
	opts.Session = &shared
	if err := codegen.Generate(polytype.Declare[CachedRow](), opts); err != nil {
		t.Fatal(err)
	}
	assert(`"name": string`)
	if err := codegen.Generate(polytype.Declare[CachedTurn](), opts); err != nil {
		t.Fatal(err)
	}
	assert(`"count": number`)
	if got := shared.Stats(); got.PackageChecks != 1 || got.PackageHits != 1 || got.FileReads != 0 || got.FallbackLoads != 0 {
		t.Fatalf("distinct configuration misses did not reuse captured analysis: %+v", got)
	}
	write("types.go", "package codegen_test\ntype CachedRow struct { Name string `json:\"name\"` }; type CachedTurn struct { Ready bool `json:\"ready\"` }\n")
	if err := codegen.Generate(polytype.Declare[CachedTurn](), opts); err != nil {
		t.Fatal(err)
	}
	assert(`"ready": boolean`)
	if got := shared.Stats(); got.PackageChecks != 2 || got.FallbackLoads != 0 {
		t.Fatalf("source edit not rechecked: %+v", got)
	}
	t.Logf("two configured misses + authored edit: %+v", shared.Stats())
}
