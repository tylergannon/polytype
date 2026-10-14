package goload_test

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tylergannon/polytype/goload"
	"golang.org/x/tools/go/packages"
)

func captureFixture(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e == nil {
			out[path] = b
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestPreparedViewReusesDiscoveredClosureWithoutGo(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	view := s.PrepareView(captureFixture(t, dir))
	t.Cleanup(func() {
		if err := view.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	a, err := view.Load(cfg, "./a")
	if err != nil || len(a) != 1 || value(a[0]) != 7 {
		t.Fatalf("initial load: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	common, err := view.Load(cfg, "./common")
	if err != nil || len(common) != 1 || value(common[0]) != 7 {
		t.Fatalf("cached closure requires Go: %v", err)
	}
	again, err := view.Load(cfg, "./common")
	if err != nil || again[0].Types != common[0].Types {
		t.Fatalf("source reuse: %v", err)
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.MetadataHits != 2 || st.PackageChecks != 2 || st.FileReads != 0 || st.FallbackLoads != 0 {
		t.Fatalf("unexpected work: %+v", st)
	}
}

func TestPreparedViewBodyAndDependencyAPIEditWithoutGo(t *testing.T) {
	dir := fixture(t)
	files := captureFixture(t, dir)
	var s goload.Session
	s.SetMaxUniverses(2)
	view := s.PrepareView(files)
	defer func() { _ = view.Close() }()
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	initial, err := view.Load(cfg, "./a", "./b")
	if err != nil {
		t.Fatal(err)
	}
	initialImports := s.Stats().ExportImports
	t.Setenv("PATH", t.TempDir())
	aFile := filepath.Join(dir, "a/root.go")
	files[aFile] = append(files[aFile], []byte("\nfunc body() int { const local = 23; return local }\n")...)
	if err := view.Update(files); err != nil {
		t.Fatal(err)
	}
	body, err := view.Load(cfg, "./a")
	if err != nil {
		t.Fatalf("body edit ran Go: %v", err)
	}
	found := false
	for n, obj := range body[0].TypesInfo.Defs {
		if n.Name == "local" {
			c, ok := obj.(*types.Const)
			found = ok && c.Val().ExactString() == "23"
		}
	}
	if !found || item(body[0]) != item(initial[0]) || s.Stats().ExportImports != initialImports {
		t.Fatal("body edit did not retain exact unchanged import objects")
	}
	commonFile := filepath.Join(dir, "common/common.go")
	files[commonFile] = []byte("package common\ntype Item struct { Name int }; const Value = 29\n")
	if err := view.Update(files); err != nil {
		t.Fatal(err)
	}
	a, err := view.Load(cfg, "./a")
	if err != nil {
		t.Fatalf("dependency edit ran Go: %v", err)
	}
	b, err := view.Load(cfg, "./b")
	if err != nil {
		t.Fatalf("reverse importer edit ran Go: %v", err)
	}
	if value(a[0]) != 29 || value(b[0]) != 29 || value(initial[0]) != 7 || value(initial[1]) != 7 {
		t.Fatal("dependency edit stale or old graphs mutated")
	}
	if item(a[0]) != item(b[0]) || item(a[0]) == item(initial[0]) {
		t.Fatal("changed dependency type identity is incoherent")
	}
	field := item(a[0]).Underlying().(*types.Struct).Field(0)
	if field.Type() != types.Typ[types.Int] {
		t.Fatalf("dependency API remained stale: %v", field.Type())
	}
	if a[0].ExportFile != "" || a[0].Imports["example.test/session/common"].ExportFile != "" {
		t.Fatal("source-checked revision advertised stale compiler artifact")
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.MetadataHits != 3 || st.FallbackLoads != 0 || st.FileReads != 0 || st.PackageChecks != 6 {
		t.Fatalf("unexpected hot work: %+v", st)
	}
}

func TestPreparedViewStructuralChangesRequireGo(t *testing.T) {
	for _, change := range []string{"import", "build", "module", "membership"} {
		t.Run(change, func(t *testing.T) {
			dir := fixture(t)
			files := captureFixture(t, dir)
			var s goload.Session
			view := s.PrepareView(files)
			defer func() { _ = view.Close() }()
			cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
			if _, err := view.Load(cfg, "./a"); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", t.TempDir())
			path := filepath.Join(dir, "a/root.go")
			switch change {
			case "import":
				files[path] = []byte("package a\nimport \"fmt\"\nvar Value=fmt.Sprint(1)\n")
			case "build":
				files[path] = append([]byte("//go:build custom\n\n"), files[path]...)
			case "module":
				files[filepath.Join(dir, "go.mod")] = []byte("module example.test/other\n\ngo 1.27\n")
			case "membership":
				files[filepath.Join(dir, "a/new.go")] = []byte("package a\nconst New=1\n")
			}
			if err := view.Update(files); err != nil {
				t.Fatal(err)
			}
			if _, err := view.Load(cfg, "./a"); err == nil {
				t.Fatal("structural change reused metadata despite unavailable Go")
			}
			if s.Stats().MetadataLoads != 2 || s.Stats().MetadataHits != 0 {
				t.Fatalf("structural change bypassed discovery: %+v", s.Stats())
			}
		})
	}
}

func TestPreparedViewCanonicalEnvironmentAndBoundedPositions(t *testing.T) {
	dir := fixture(t)
	files := captureFixture(t, dir)
	var s goload.Session
	s.SetMaxUniverses(2)
	view := s.PrepareView(files)
	defer func() { _ = view.Close() }()
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	first, err := view.Load(cfg, "./a")
	if err != nil {
		t.Fatal(err)
	}
	countFiles := func(p *packages.Package) int {
		n := 0
		p.Fset.Iterate(func(*token.File) bool { n++; return true })
		return n
	}
	initialFiles := countFiles(first[0])
	slices.Reverse(cfg.Env)
	t.Setenv("PATH", t.TempDir())
	again, err := view.Load(cfg, "./a")
	if err != nil || again[0].Types != first[0].Types {
		t.Fatalf("environment order invalidated view: %v", err)
	}
	file := filepath.Join(dir, "a/root.go")
	base := slices.Clone(files[file])
	for i := 0; i < 12; i++ {
		files[file] = append(slices.Clone(base), []byte(fmt.Sprintf("\nconst Edit = %d\n", i))...)
		if err := view.Update(files); err != nil {
			t.Fatal(err)
		}
		got, err := view.Load(cfg, "./a")
		if err != nil {
			t.Fatal(err)
		}
		if n := countFiles(got[0]); n != initialFiles {
			t.Fatalf("retained historical token files at edit%d: %d want%d", i, n, initialFiles)
		}
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.PackageChecks != 13 || st.UniverseEvictions != 11 {
		t.Fatalf("unbounded/reloaded edit history: %+v", st)
	}
	if value(first[0]) != 7 || countFiles(first[0]) != initialFiles {
		t.Fatal("old returned graph mutated")
	}
}

// This is the explicitly typed transitive constant regression found in the
// pinned gopls snapshot experiment for SKGO#311. Reachability that only visits
// the declared "int" type misses the initializer's storage.Code dependency.
func TestPreparedViewTypedConstantDiagnosticAndRepairWithoutGo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.test/constant\n\ngo 1.27\n")
	write(t, dir, "storage/storage.go", "package storage\nconst Code = 1\n")
	write(t, dir, "model/model.go", "package model\nimport \"example.test/constant/storage\"\nconst Explicit int = storage.Code\ntype Block [Explicit]byte\n")
	write(t, dir, "api/api.go", "package api\nimport \"example.test/constant/model\"\nfunc New() model.Block { return model.Block{} }\n")
	write(t, dir, "client/client.go", "package client\nimport \"example.test/constant/api\"\nvar Value [1]byte = api.New()\n")
	files := captureFixture(t, dir)
	var s goload.Session
	s.SetMaxUniverses(2)
	v := s.PrepareView(files)
	defer func() { _ = v.Close() }()
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	initial, err := v.Load(cfg, "./client")
	if err != nil || len(initial) != 1 || len(initial[0].Errors) > 0 {
		t.Fatalf("initial: %v %+v", err, initial)
	}
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(dir, "storage/storage.go")
	files[path] = []byte("package storage\nconst Code = 2\n")
	if err := v.Update(files); err != nil {
		t.Fatal(err)
	}
	bad, err := v.Load(cfg, "./client")
	if err != nil || len(bad) != 1 || len(bad[0].Errors) == 0 || !strings.Contains(bad[0].Errors[0].Msg, "[1]byte") {
		t.Fatalf("missing fresh array diagnostic: %v %+v", err, bad)
	}
	checks := s.Stats().PackageChecks
	again, err := v.Load(cfg, "./client")
	if err != nil || len(again[0].Errors) == 0 || s.Stats().PackageChecks != checks {
		t.Fatalf("invalid source diagnostic was not reused: %v %+v", err, s.Stats())
	}
	if len(initial[0].Errors) > 0 {
		t.Fatal("old valid graph mutated")
	}
	files[path] = []byte("package storage\nconst Code = 1\n")
	if err := v.Update(files); err != nil {
		t.Fatal(err)
	}
	fixed, err := v.Load(cfg, "./client")
	if err != nil || len(fixed[0].Errors) > 0 {
		t.Fatalf("repair failed: %v %+v", err, fixed[0].Errors)
	}
	if got := fixed[0].Types.Scope().Lookup("Value").Type().(*types.Array).Len(); got != 1 {
		t.Fatalf("array = %d, want 1", got)
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.FallbackLoads != 0 || st.FileReads != 0 {
		t.Fatalf("edit/repair launched discovery or reread: %+v", st)
	}
}

func TestPreparedViewGeneratedPhaseAndCapturedBodyEditsReuseMetadata(t *testing.T) {
	dir := fixture(t)
	files := captureFixture(t, dir)
	var s goload.Session
	s.SetMaxUniverses(4)
	v := s.PrepareView(files)
	defer func() { _ = v.Close() }()
	generated := filepath.Join(dir, "a/generated.go")
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off"), Overlay: map[string][]byte{generated: []byte("package a\nconst Generated = 17\n")}}
	first, err := v.Load(cfg, "./a")
	if err != nil || len(first[0].Errors) > 0 {
		t.Fatalf("initial: %v %+v", err, first[0].Errors)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatal("phase file was published")
	}
	t.Setenv("PATH", t.TempDir())
	cfg.Overlay[generated] = []byte("package a\nconst Generated = 29\n")
	next, err := v.Load(cfg, "./a")
	if err != nil {
		t.Fatal(err)
	}
	if got := next[0].Types.Scope().Lookup("Generated").(*types.Const).Val().ExactString(); got != "29" {
		t.Fatalf("phase edit = %s", got)
	}
	source := filepath.Join(dir, "a/root.go")
	files[source] = append(files[source], []byte("\nfunc body() int { return 41 }\n")...)
	if err := v.Update(files); err != nil {
		t.Fatal(err)
	}
	after, err := v.Load(cfg, "./a")
	if err != nil || len(after[0].Errors) > 0 {
		t.Fatalf("body edit: %v %+v", err, after[0].Errors)
	}
	if got := after[0].Types.Scope().Lookup("body"); got == nil {
		t.Fatal("captured body edit omitted")
	}
	if got := first[0].Types.Scope().Lookup("Generated").(*types.Const).Val().ExactString(); got != "17" {
		t.Fatalf("old phase mutated: %s", got)
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.FallbackLoads != 0 || st.FileReads != 0 {
		t.Fatalf("phase edit repeated metadata or read: %+v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg.Context = ctx
	if _, err := v.Load(cfg, "./a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled warm load: %v", err)
	}
}

func TestPreparedViewSyntaxFailureAndRepairWithoutGo(t *testing.T) {
	dir := fixture(t)
	files := captureFixture(t, dir)
	var s goload.Session
	s.SetMaxUniverses(2)
	v := s.PrepareView(files)
	defer func() { _ = v.Close() }()
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	if _, err := v.Load(cfg, "./a"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(dir, "a/root.go")
	original := slices.Clone(files[path])
	files[path] = append(slices.Clone(original), []byte("\nfunc broken() { !!! }\n")...)
	if err := v.Update(files); err != nil {
		t.Fatal(err)
	}
	broken, err := v.Load(cfg, "./a")
	if err != nil || len(broken[0].Errors) == 0 || broken[0].Errors[0].Kind != packages.ParseError {
		t.Fatalf("syntax diagnostic: %v %+v", err, broken)
	}
	files[path] = original
	if err := v.Update(files); err != nil {
		t.Fatal(err)
	}
	repaired, err := v.Load(cfg, "./a")
	if err != nil || len(repaired[0].Errors) > 0 || value(repaired[0]) != 7 {
		t.Fatalf("repair: %v %+v", err, repaired)
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.FallbackLoads != 0 {
		t.Fatalf("syntax error launched Go: %+v", st)
	}
}
