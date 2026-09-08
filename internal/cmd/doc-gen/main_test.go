package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadManifestRejectsDuplicateIDs(t *testing.T) {
	path := writeTestFile(t, "manifest.json", `{"examples":[
		{"id":"same","title":"First","when":"Now.","sections":[{"source":"a.go","declarations":["A"]}]},
		{"id":"same","title":"Second","when":"Later.","sections":[{"source":"b.go","declarations":["B"]}]}
	]}`)
	_, err := loadManifest(path)
	if err == nil || !strings.Contains(err.Error(), `duplicate example id "same"`) {
		t.Fatalf("loadManifest() error = %v, want duplicate ID error", err)
	}
}

func TestExtractSelectsDeclarationsAndRegistrationInSourceOrder(t *testing.T) {
	path := writeTestFile(t, "example.go", `package fixture

type Status string
const (
	Pending Status = "pending"
	Done Status = "done"
)
type Task struct { Status Status }
func (Task) Schema() []byte { return nil }
var _ = polytype.NewJSONSchemaMethod(Task.Schema, polytype.WithStringerEnum(Task{}.Status))
`)
	got, err := extract(path, section{
		Source:        "example.go",
		Declarations:  []string{"Status", "Pending", "Task"},
		Registrations: []string{"Task.Schema"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := string(bytes.Join(got, []byte("\n")))
	for _, want := range []string{"type Status string", "const (", "type Task struct", "NewJSONSchemaMethod"} {
		if !strings.Contains(joined, want) {
			t.Errorf("output missing %q:\n%s", want, joined)
		}
	}
	if strings.Index(joined, "type Status") > strings.Index(joined, "type Task") {
		t.Fatalf("declarations not preserved in source order:\n%s", joined)
	}
}

func TestExtractSelectsFluentChainRegistration(t *testing.T) {
	path := writeTestFile(t, "example.go", `package fixture

type Task struct { Status string }
func (Task) Schema() []byte { return nil }
var _ = polytype.Declare(Task.Schema).StringerEnum(polytype.Field[Task, Status]("Status"))
`)
	got, err := extract(path, section{Source: "example.go", Registrations: []string{"Task.Schema"}})
	if err != nil {
		t.Fatal(err)
	}
	joined := string(bytes.Join(got, []byte("\n")))
	if !strings.Contains(joined, "polytype.Declare(Task.Schema).StringerEnum(polytype.Field[Task, Status](\"Status\"))") {
		t.Errorf("output missing fluent chain:\n%s", joined)
	}
}

func TestExtractRejectsMissingSelections(t *testing.T) {
	path := writeTestFile(t, "example.go", "package fixture\ntype Present struct{}\n")
	_, err := extract(path, section{Source: "example.go", Declarations: []string{"Missing"}})
	if err == nil || !strings.Contains(err.Error(), "missing declarations in example.go: Missing") {
		t.Fatalf("extract() error = %v, want missing declaration error", err)
	}
}

func TestExtractRejectsMissingRegistration(t *testing.T) {
	path := writeTestFile(t, "example.go", "package fixture\ntype Present struct{}\n")
	_, err := extract(path, section{Source: "example.go", Registrations: []string{"Present.Schema"}})
	if err == nil || !strings.Contains(err.Error(), "missing registrations in example.go: Present.Schema") {
		t.Fatalf("extract() error = %v, want missing registration error", err)
	}
}

func TestExtractRejectsMissingFile(t *testing.T) {
	_, err := extract(filepath.Join(t.TempDir(), "missing.go"), section{Source: "missing.go", Declarations: []string{"Missing"}})
	if err == nil || !strings.Contains(err.Error(), "parse missing.go") {
		t.Fatalf("extract() error = %v, want missing file parse error", err)
	}
}

func TestWriteIfChangedPreservesModTime(t *testing.T) {
	path := writeTestFile(t, "output.md", "same")
	before := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, before, before); err != nil {
		t.Fatal(err)
	}
	if err := writeIfChanged(path, []byte("same")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(before) {
		t.Fatalf("modification time changed: got %v, want %v", info.ModTime(), before)
	}
}

func TestCheckedInReferenceIsCurrent(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	want, err := generate(root, filepath.Join(root, manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, outputPath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; run go generate ./...", outputPath)
	}
}

func writeTestFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
