package codegen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
	"github.com/tylergannon/polytype/goload"
)

func TestSourceIdentityUsesCurrentTypesAndPersistentReplay(t *testing.T) {
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/sourceidentity\n\ngo 1.27\n")
	write("types.go", "package model\ntype Row struct { Name string `json:\"name\"` }; type Turn struct { Count int `json:\"count\"` }\n")
	config := polytype.Compose(polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/sourceidentity", Name: "Row"}), polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/sourceidentity", Name: "Turn", Pointer: true}))
	opts := codegen.Options{TargetDir: dir, TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts")}}
	assert := func(want string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "ts/types.ts"))
		if err != nil || !bytes.Contains(b, []byte(want)) {
			t.Fatalf("want %q: %v %s", want, err, b)
		}
	}
	if err := codegen.Generate(config, opts); err != nil {
		t.Fatal(err)
	}
	assert(`"name": string`)
	assert(`"count": number`)
	var s goload.Session
	opts.Session = &s
	if err := os.RemoveAll(opts.TypeScript.Dir); err != nil {
		t.Fatal(err)
	}
	if err := codegen.Generate(config, opts); err != nil {
		t.Fatal(err)
	}
	if s.Stats() != (goload.Stats{}) {
		t.Fatalf("replay performed analysis: %+v", s.Stats())
	}
	assert(`"name": string`)
	write("types.go", "package model\ntype Row struct { Ready bool `json:\"ready\"` }; type Turn struct { Count int `json:\"count\"` }\n")
	if err := codegen.Generate(config, opts); err != nil {
		t.Fatal(err)
	}
	assert(`"ready": boolean`)
	if s.Stats().PackageChecks == 0 {
		t.Fatal("edited source was not analyzed")
	}
	bad := polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/sourceidentity", Name: "Missing"})
	if err := codegen.Generate(bad, opts); err == nil || !strings.Contains(err.Error(), "Missing") {
		t.Fatalf("missing source type diagnostic: %v", err)
	}
}

func TestDeclareTypeRejectsInvalidIdentity(t *testing.T) {
	for _, typ := range []polytype.TypeSpec{{Name: "Row"}, {PackagePath: "example.test/model", Name: "_"}, {PackagePath: "example.test/model", Name: "*Row"}, {PackagePath: "example.test/model", Name: "type"}} {
		if _, err := polytype.ResolveConfiguration(polytype.DeclareType(typ)); err == nil {
			t.Fatalf("accepted %#v", typ)
		}
	}
}
