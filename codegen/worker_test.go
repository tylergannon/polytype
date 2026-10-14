package codegen_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
	"github.com/tylergannon/polytype/goload"
)

func TestWorkerEditsRepairAndReplay(t *testing.T) {
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/worker\n\ngo 1.27\n")
	write("dep/value.go", "package dep\nconst Size int = 1\ntype Item struct { Name string `json:\"name\"` }\n")
	root := `package model
import "example.test/worker/dep"
type Row struct { Item dep.Item ` + "`json:\"item\"`" + `; Block [dep.Size]byte ` + "`json:\"block\"`" + ` }
var valid [1]byte = [dep.Size]byte{}
`
	write("model/row.go", root)
	write("unrelated/unused.go", "package unrelated\nconst Value = 7\n")
	config := polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/worker/model", Name: "Row"})
	var session goload.Session
	var worker codegen.Worker
	defer func() {
		if err := worker.Close(); err != nil {
			t.Error(err)
		}
	}()
	opts := codegen.Options{Session: &session, TargetDir: filepath.Join(dir, "model"), TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts")}}
	run := func() {
		t.Helper()
		if err := worker.Generate(config, opts); err != nil {
			t.Fatal(err)
		}
	}
	output := func() []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "ts/types.ts"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	assert := func(want string) {
		t.Helper()
		if !bytes.Contains(output(), []byte(want)) {
			t.Fatalf("missing %q: %s", want, output())
		}
	}
	run()
	assert(`"name": string`)
	initial := session.Stats()
	if initial.MetadataLoads == 0 {
		t.Fatal("worker did not analyze source")
	}
	write("unrelated/unused.go", "package unrelated\nconst Value = 29\n")
	write("ts/types.ts", "broken output")
	run()
	assert(`"name": string`)
	if session.Stats() != initial {
		t.Fatalf("unrelated edit or output repair analyzed source: %+v", session.Stats())
	}
	write("model/row.go", root+"\nfunc body() int { return 41 }\n")
	run()
	assert(`"name": string`)
	if session.Stats().MetadataLoads != initial.MetadataLoads || session.Stats().PackageChecks <= initial.PackageChecks {
		t.Fatalf("body edit did not use warm analysis: %+v", session.Stats())
	}
	write("dep/value.go", "package dep\nconst Size int = 1\ntype Item struct { Ready bool `json:\"ready\"` }\n")
	run()
	assert(`"ready": boolean`)
	if session.Stats().MetadataLoads != initial.MetadataLoads {
		t.Fatalf("dependency API edit rediscovered: %+v", session.Stats())
	}
	good := output()
	write("dep/value.go", "package dep\nconst Size int = 2\ntype Item struct { Ready bool `json:\"ready\"` }\n")
	for range 2 {
		if err := worker.Generate(config, opts); err == nil || !strings.Contains(err.Error(), "[1]byte") {
			t.Fatalf("stale typed constant accepted: %v", err)
		}
		if !bytes.Equal(good, output()) {
			t.Fatal("broken edit published output")
		}
	}
	write("dep/value.go", "package dep\nconst Size int = 1\ntype Item struct { Count int `json:\"count\"` }\n")
	run()
	assert(`"count": number`)
	if session.Stats().MetadataLoads != initial.MetadataLoads {
		t.Fatalf("repair rediscovered: %+v", session.Stats())
	}
	write("dep/extra.go", "package dep\ntype Other struct{ Flag bool }\n")
	run()
	if session.Stats().MetadataLoads <= initial.MetadataLoads {
		t.Fatal("new member did not refresh metadata")
	}
}

func TestWorkerStartsWithPersistentReplayThenAnalyzesCurrentEdit(t *testing.T) {
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	write := func(src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/replay\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	write("package model\ntype Row struct{Name string `json:\"name\"`}\n")
	config := polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/replay", Name: "Row"})
	var session goload.Session
	opts := codegen.Options{Session: &session, TargetDir: dir, TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts")}}
	var seed codegen.Worker
	if err := seed.Generate(config, opts); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	session = goload.Session{}
	var worker codegen.Worker
	defer func() { _ = worker.Close() }()
	if err := os.RemoveAll(opts.TypeScript.Dir); err != nil {
		t.Fatal(err)
	}
	if err := worker.Generate(config, opts); err != nil {
		t.Fatal(err)
	}
	if session.Stats() != (goload.Stats{}) {
		t.Fatalf("persistent replay analyzed: %+v", session.Stats())
	}
	write("package model\ntype Row struct{Ready bool `json:\"ready\"`}\n")
	if err := worker.Generate(config, opts); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(opts.TypeScript.Dir, "types.ts"))
	if err != nil || !bytes.Contains(b, []byte(`"ready": boolean`)) {
		t.Fatalf("stale persistent output: %v %s", err, b)
	}
	if session.Stats().PackageChecks == 0 {
		t.Fatal("changed input not checked")
	}
}

func TestWorkerLocalReplacementsAndModuleEdit(t *testing.T) {
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := `module example.test/app

go 1.27
require (
 example.test/dep v0.0.0
 example.test/trans v0.0.0
)
replace example.test/dep => ../dep
replace example.test/trans => ../right
`
	write("app/go.mod", main)
	write("app/types.go", "package app\nimport \"example.test/dep\"\ntype Row struct { Item dep.Item `json:\"item\"` }\n")
	write("dep/go.mod", "module example.test/dep\n\ngo 1.27\nrequire example.test/trans v0.0.0\nreplace example.test/trans => ../wrong\n")
	write("dep/types.go", "package dep\nimport \"example.test/trans\"\ntype Item = trans.Item\n")
	for _, name := range []string{"right", "wrong", "other"} {
		write(name+"/go.mod", "module example.test/trans\n\ngo 1.27\n")
	}
	write("right/types.go", "package trans\ntype Item struct { Name string `json:\"name\"` }\n")
	write("wrong/types.go", "package trans\ntype Item struct { Wrong bool `json:\"wrong\"` }\n")
	write("other/types.go", "package trans\ntype Item struct { Ready bool `json:\"ready\"` }\n")
	var worker codegen.Worker
	defer func() { _ = worker.Close() }()
	var session goload.Session
	opts := codegen.Options{Session: &session, TargetDir: filepath.Join(dir, "app"), TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts")}}
	config := polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/app", Name: "Row"})
	run := func(want string) {
		t.Helper()
		if err := worker.Generate(config, opts); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "ts/types.ts"))
		if err != nil || !bytes.Contains(b, []byte(want)) {
			t.Fatalf("want %q: %v %s", want, err, b)
		}
	}
	run(`"name": string`)
	initial := session.Stats().MetadataLoads
	write("right/types.go", "package trans\ntype Item struct { Count int `json:\"count\"` }\n")
	run(`"count": number`)
	if session.Stats().MetadataLoads != initial {
		t.Fatal("replacement source edit rediscovered")
	}
	write("app/go.mod", strings.Replace(main, "../right", "../other", 1))
	run(`"ready": boolean`)
	if session.Stats().MetadataLoads <= initial {
		t.Fatal("module selection edit failed to rediscover")
	}
}

func TestWorkerKeepsWholeLocalGraphAcrossLeafEdits(t *testing.T) {
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/chain\n\ngo 1.27\n")
	for i := 7; i >= 0; i-- {
		src := fmt.Sprintf("package p%d\ntype Row struct { Name string }\n", i)
		if i < 7 {
			src = fmt.Sprintf("package p%d\nimport \"example.test/chain/p%d\"\ntype Row struct { Next p%d.Row }\n", i, i+1, i+1)
		}
		write(fmt.Sprintf("p%d/types.go", i), src)
	}
	var session goload.Session
	var worker codegen.Worker
	defer func() { _ = worker.Close() }()
	cfg := polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/chain/p0", Name: "Row"})
	opts := codegen.Options{Session: &session, TargetDir: filepath.Join(dir, "p0"), TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts")}}
	run := func() {
		t.Helper()
		if err := worker.Generate(cfg, opts); err != nil {
			t.Fatal(err)
		}
	}
	run()
	if got := session.Stats().PackageChecks; got != 8 {
		t.Fatalf("initial graph checks=%d, want8", got)
	}
	for i := range 4 {
		before := session.Stats()
		write("p0/types.go", fmt.Sprintf("package p0\nimport \"example.test/chain/p1\"\ntype Row struct { Next p1.Row }\nfunc body() int {return %d}\n", 41+i))
		run()
		after := session.Stats()
		if after.PackageChecks-before.PackageChecks != 1 || after.MetadataLoads != before.MetadataLoads {
			t.Fatalf("edit%d should recheck only root without metadata: before=%+v after=%+v", i, before, after)
		}
	}
	before := session.Stats()
	write("p0/types.go", "package p0\nimport (\"example.test/chain/p1\"; \"example.test/chain/p2\")\ntype Row struct { Next p1.Row; Extra p2.Row }\n")
	run()
	after := session.Stats()
	if after.MetadataLoads <= before.MetadataLoads || after.PackageChecks-before.PackageChecks != 1 {
		t.Fatalf("import change should rediscover and retain unchanged packages: before=%+v after=%+v", before, after)
	}
	b, err := os.ReadFile(filepath.Join(dir, "ts/types.ts"))
	if err != nil || !bytes.Contains(b, []byte(`"Name": string`)) || !bytes.Contains(b, []byte(`"Extra":`)) {
		t.Fatalf("fresh literal types: %v %s", err, b)
	}
	if got := worker.Stats(); got != (codegen.WorkerStats{Analyses: 6, Emissions: 6}) {
		t.Fatalf("worker stats: %+v", got)
	}
}

func TestWorkerCapturesLinkedSourceBytes(t *testing.T) {
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "authored.go")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/linked\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	write := func(src string) {
		t.Helper()
		if err := os.WriteFile(target, []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package linked\ntype Row struct {Name string}\n")
	if err := os.Symlink(target, filepath.Join(dir, "types.go")); err != nil {
		t.Fatal(err)
	}
	var worker codegen.Worker
	var session goload.Session
	defer func() { _ = worker.Close() }()
	cfg := polytype.DeclareType(polytype.TypeSpec{PackagePath: "example.test/linked", Name: "Row"})
	opts := codegen.Options{Session: &session, TargetDir: dir, TypeScript: &codegen.TypeScriptOptions{Dir: filepath.Join(dir, "ts")}}
	if err := worker.Generate(cfg, opts); err != nil {
		t.Fatal(err)
	}
	before := session.Stats()
	write("package linked\ntype Row struct {Ready bool}\n")
	if err := worker.Generate(cfg, opts); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "ts/types.ts"))
	if err != nil || !bytes.Contains(b, []byte(`"Ready": boolean`)) {
		t.Fatalf("linked edit stale: %v %s", err, b)
	}
	if session.Stats().MetadataLoads != before.MetadataLoads {
		t.Fatal("linked ordinary edit rediscovered")
	}
}
