package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tylergannon/polytype"
	example "github.com/tylergannon/polytype/examples/optionality"
)

type caseResult struct {
	Name        string         `json:"name"`
	Input       string         `json:"input"`
	Valid       bool           `json:"valid"`
	DecodeOK    bool           `json:"decode_ok"`
	Unchanged   bool           `json:"unchanged_after_error,omitempty"`
	State       map[string]any `json:"state,omitempty"`
	Remarshaled string         `json:"remarshaled,omitempty"`
}

type transcript struct {
	Required   []string                   `json:"required"`
	Properties map[string]json.RawMessage `json:"properties"`
	Cases      []caseResult               `json:"cases"`
	Rejected   []rejectedResult           `json:"rejected_shapes"`
}

type rejectedResult struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	actual, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		panic(err)
	}
	actual = append(actual, '\n')
	_, source, _, _ := runtime.Caller(0)
	expectedPath := filepath.Join(filepath.Dir(source), "..", "..", "proof", "expected.json")
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !bytes.Equal(actual, expected) {
		fmt.Print(string(actual))
		fmt.Fprintln(os.Stderr, "proof transcript differs from examples/optionality/proof/expected.json")
		os.Exit(1)
	}
	fmt.Print(string(actual))
}

func run() (transcript, error) {
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(example.Config{}.Schema(), &schema); err != nil {
		return transcript{}, err
	}

	inputs := []struct {
		name  string
		input string
	}{
		{"missing optional", `{"name":"base","timeout":null,"detail":null}`},
		{"present zero and empty", `{"name":"zero","max_retries":0,"nickname":"","metadata":{"message":""},"backup":{"message":"saved"},"tags":[],"timeout":0,"detail":{"message":"now"}}`},
		{"null optional", `{"name":"bad","max_retries":null,"timeout":null,"detail":null}`},
		{"missing nullable", `{"name":"bad"}`},
		{"optional interface", `{"name":"pet","pet":{"!kind":"Dog","name":"Rex"},"timeout":null,"detail":null}`},
		{"unknown interface", `{"name":"bad","pet":{"!kind":"Bird"},"timeout":null,"detail":null}`},
	}

	result := transcript{Required: schema.Required, Properties: schema.Properties}
	for _, item := range inputs {
		initial := example.Config{Name: "unchanged", MaxRetries: presentInt(9)}
		value := initial
		validationErr := (example.Config{}).ValidateJSON([]byte(item.input))
		decodeErr := json.Unmarshal([]byte(item.input), &value)
		entry := caseResult{
			Name:      item.name,
			Input:     item.input,
			Valid:     validationErr == nil,
			DecodeOK:  decodeErr == nil,
			Unchanged: decodeErr != nil && sameConfig(value, initial),
		}
		if decodeErr == nil {
			entry.State = state(value)
			encoded, err := json.Marshal(value)
			if err != nil {
				return transcript{}, fmt.Errorf("marshal %s: %w", item.name, err)
			}
			entry.Remarshaled = string(encoded)
		}
		result.Cases = append(result.Cases, entry)
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	negative := []struct {
		name, dir, reason string
	}{
		{"optional without omitzero", "optional_without_omitzero", `requires json:",omitzero"`},
		{"nullable slice", "nullable_slice", "does not support arrays/slices"},
		{"nested wrapper", "nested_wrapper", "supported only as the complete type of a direct named struct field"},
		{"wrapper in container", "wrapper_in_container", "supported only as the complete type of a direct named struct field"},
		{"wrapper alias", "wrapper_alias", "supported only as the complete type of a direct named struct field"},
		{"defined wrapper", "defined_wrapper", "supported only as the complete type of a direct named struct field"},
		{"embedded wrapper", "embedded_wrapper", "embedded polytype.Optional is unsupported"},
		{"wrapper root", "wrapper_root", "supported only as the complete type of a direct named struct field"},
		{"nullable interface", "nullable_interface", "does not support sealed interfaces"},
		{"nullable ref", "nullable_ref", "does not support explicit refs"},
		{"nullable provider", "nullable_provider", "does not support providers"},
	}
	// The negative cases used to reach the command through `go run ./polytype`,
	// which relinks the CLI on every invocation: eleven refusals paid eleven
	// links to run the same binary. It is built once here instead.
	cli, cleanup, err := buildCLI(root)
	if err != nil {
		return transcript{}, err
	}
	defer cleanup()

	for _, item := range negative {
		target := filepath.Join(root, "examples", "optionality", "negative", item.dir)
		cmd := exec.Command(cli, "gen", "--target", target)
		cmd.Dir = root
		output, commandErr := cmd.CombinedOutput()
		if commandErr == nil || !strings.Contains(string(output), item.reason) {
			return transcript{}, fmt.Errorf("negative generator case %q: wanted failure containing %q; err=%v output=%s", item.name, item.reason, commandErr, output)
		}
		result.Rejected = append(result.Rejected, rejectedResult{Name: item.name, Reason: item.reason})
	}
	return result, nil
}

// buildCLI builds the polytype command into a temp directory and returns its
// path along with a function that removes the directory.
func buildCLI(root string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "polytype-proof")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	cli := filepath.Join(dir, "polytype")
	build := exec.Command("go", "build", "-o", cli, "./polytype")
	build.Dir = root
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("building the polytype CLI: %w\n%s", buildErr, output)
	}
	return cli, cleanup, nil
}

func presentInt(value int) polytype.Optional[int] {
	return polytype.Optional[int]{Present: true, Value: value}
}

func sameConfig(a, b example.Config) bool {
	return a.Name == b.Name && a.MaxRetries == b.MaxRetries &&
		a.Nickname == b.Nickname && a.Metadata == b.Metadata &&
		a.Backup.Present == b.Backup.Present && a.Backup.Value == b.Backup.Value &&
		a.Tags.Present == b.Tags.Present && a.Pet.Present == b.Pet.Present &&
		a.Timeout == b.Timeout && a.Detail == b.Detail
}

func state(value example.Config) map[string]any {
	petType := ""
	if value.Pet.Present {
		petType = fmt.Sprintf("%T", value.Pet.Value)
	}
	return map[string]any{
		"max_retries": map[string]any{"present": value.MaxRetries.Present, "value": value.MaxRetries.Value},
		"nickname":    map[string]any{"present": value.Nickname.Present, "value": value.Nickname.Value},
		"metadata":    map[string]any{"present": value.Metadata.Present, "value": value.Metadata.Value.Message},
		"backup":      map[string]any{"present": value.Backup.Present, "non_nil": value.Backup.Value != nil},
		"tags":        map[string]any{"present": value.Tags.Present, "length": len(value.Tags.Value)},
		"pet":         map[string]any{"present": value.Pet.Present, "type": petType},
		"timeout":     map[string]any{"present": value.Timeout.Present, "value": value.Timeout.Value},
		"detail":      map[string]any{"present": value.Detail.Present, "value": value.Detail.Value.Message},
	}
}
