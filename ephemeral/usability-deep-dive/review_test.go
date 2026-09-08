// These probes characterize the reviewed revision. Assertions about defects
// are evidence of current behavior, not the desired product contract.
package review_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tylergannon/polytype/devalue/codegen"
	"github.com/tylergannon/polytype/grammar"
	"github.com/tylergannon/polytype/typescript"
)

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, fields string, registered bool) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, "go.mod"), "module example.com/fixture\n\ngo 1.27\n\nrequire github.com/tylergannon/polytype v0.0.0\nreplace github.com/tylergannon/polytype => "+repo+"\n")
	put(t, filepath.Join(dir, "types.go"), "package fixture\ntype Order struct { "+fields+" }\n")
	if registered {
		put(t, filepath.Join(dir, "schema.go"), `//go:build jsonschema

package fixture
import (
 "encoding/json"
 "github.com/tylergannon/polytype"
)
func (Order) Schema() json.RawMessage { panic("stub") }
func (Order) ValidateJSON([]byte) error { panic("stub") }
var _ = polytype.Declare(Order.Schema)
`)
	}
	return dir
}

func cli(t *testing.T, dir string, flags ...string) ([]byte, error) {
	t.Helper()
	bin, err := filepath.Abs("bin/polytype")
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--target", dir}, flags...)
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	return cmd.CombinedOutput()
}

func generate(t *testing.T, dir string, flags ...string) {
	t.Helper()
	if out, err := cli(t, dir, flags...); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}
}

func TestMarkerFreeLibraryGenerationExists(t *testing.T) {
	dir := fixture(t, "ID string `json:\"id\"`", false)
	pkg, err := grammar.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	defs, roots, err := pkg.Lower([]grammar.Root{{Type: pkg.Types().Scope().Lookup("Order").Type()}})
	if err != nil {
		t.Fatal(err)
	}
	ts, err := typescript.Generate(defs, typescript.Options{})
	if err != nil {
		t.Fatal(err)
	}
	goSource, err := codegen.Generate(defs, roots, codegen.Options{PackageName: "codec", ImportPath: "example.com/fixture/codec"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ts.Files) != 1 || !bytes.Contains(goSource, []byte("func EncodeOrder")) {
		t.Fatal("expected generated output")
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f.Name(), "schema") {
			t.Fatalf("unexpected schema artifact %s", f.Name())
		}
	}
	t.Log("grammar.Load/Lower generated TypeScript and devalue Go source without schema.go or writes to the model package")
}

func TestByteSliceSchemaDisagreesWithGoAndTypeScript(t *testing.T) {
	dir := fixture(t, "Data []uint8 `json:\"data\"`", true)
	generate(t, dir, "--validate")
	put(t, filepath.Join(dir, "consumer_test.go"), `package fixture
import ("encoding/json"; "testing")
func TestWire(t *testing.T) {
 b, err := json.Marshal(Order{Data: []byte{1,2}})
 if err != nil { t.Fatal(err) }
 err = (Order{}).ValidateJSON(b)
 if err == nil { t.Fatal("expected generated schema to reject actual Go wire bytes") }
 t.Logf("json.Marshal=%s; generated ValidateJSON rejects it: %v", b, err)
}
`)
	cmd := exec.Command("go", "test", "-mod=mod", "-v", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("consumer: %v\n%s", err, out)
	}
	t.Log(string(out))
	out, err = cli(t, dir, "--typescript", filepath.Join(dir, "ts"))
	if err == nil || !bytes.Contains(out, []byte("byte-like slices")) {
		t.Fatalf("expected TypeScript refusal: %v\n%s", err, out)
	}
	t.Logf("adding --typescript fails: %s", out)
}

func TestFixedArrayValidationAcceptsTruncation(t *testing.T) {
	dir := fixture(t, "Values [2]int `json:\"values\"`", true)
	generate(t, dir, "--validate")
	put(t, filepath.Join(dir, "consumer_test.go"), `package fixture
import ("encoding/json"; "testing")
func TestWire(t *testing.T) {
 b := []byte("{\"values\":[1,2,3]}")
 if err := (Order{}).ValidateJSON(b); err != nil { t.Fatal(err) }
 var v Order
 if err := json.Unmarshal(b, &v); err != nil { t.Fatal(err) }
 if v.Values != [2]int{1,2} { t.Fatal(v) }
 t.Logf("ValidateJSON accepted %s; json.Unmarshal silently retained only %v", b, v.Values)
}
`)
	cmd := exec.Command("go", "test", "-mod=mod", "-v", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("consumer: %v\n%s", err, out)
	}
	t.Log(string(out))
}

func TestNoChangesSilentlyRepairsTamperedAndMissingSchema(t *testing.T) {
	dir := fixture(t, "ID string `json:\"id\"`", true)
	generate(t, dir)
	path := filepath.Join(dir, "jsonschema", "Order.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, "{}\n")
	generate(t, dir, "--no-changes")
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("expected observed repair: %v", err)
	}
	t.Log("--no-changes exited 0 and rewrote altered Order.json while its .sum was unchanged")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	generate(t, dir, "--no-changes")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	t.Log("--no-changes exited 0 and recreated missing Order.json while its .sum remained")
}

func TestFailureLeavesPartialOutput(t *testing.T) {
	dir := fixture(t, "ID string `json:\"id\"`", true)
	if err := os.Mkdir(filepath.Join(dir, "jsonschema_gen.go"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err := cli(t, dir)
	if err == nil {
		t.Fatal("expected output-path collision")
	}
	for _, name := range []string{"Order.json", "Order.json.sum"} {
		if _, err := os.Stat(filepath.Join(dir, "jsonschema", name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("generation failed after writing schema and sum: %s", out)
}
