package v1_interfaces_options

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/tylergannon/polytype"
	yaml "go.yaml.in/yaml/v4"
)

func TestGeneratedYAMLUnmarshalSimple(t *testing.T) {
	var got Plain
	if err := yaml.Load([]byte(`
tags: [one, two]
inner:
  a: alpha
  b: beta
count: 2
`), &got, yaml.WithV4Defaults()); err != nil {
		t.Fatal(err)
	}
	want := Plain{Tags: []string{"one", "two"}, Inner: polytype.Nullable[*PlainInner]{Present: true, Value: &PlainInner{A: "alpha", B: "beta"}}, Count: 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded plain value = %#v, want %#v", got, want)
	}

	original := Plain{Tags: []string{"keep-a", "keep-b"}, Inner: polytype.Nullable[*PlainInner]{Present: true, Value: &PlainInner{A: "keep-a", B: "keep-b"}}, Count: 5}
	sharedTags := original.Tags
	sharedInner := original.Inner
	got = original
	err := yaml.Load([]byte(`
tags: [clobbered]
inner:
  a: clobbered
count: not-an-int
`), &got, yaml.WithV4Defaults())
	if err == nil {
		t.Fatal("invalid count unexpectedly decoded")
	}
	if !reflect.DeepEqual(got, original) || !reflect.DeepEqual(sharedTags, []string{"keep-a", "keep-b"}) ||
		*sharedInner.Value != (PlainInner{A: "keep-a", B: "keep-b"}) {
		t.Fatalf("failed decode mutated caller state: got %#v, tags %#v, inner %#v", got, sharedTags, sharedInner)
	}

	got = original
	if err := yaml.Load([]byte("count: 2\n"), &got, yaml.WithV4Defaults()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Plain{Count: 2}) {
		t.Fatalf("replacement decode = %#v, want omitted fields reset", got)
	}
}

func TestGeneratedYAMLUnmarshalComprehensive(t *testing.T) {
	input := []byte(`
defaults: &defaults
  if:
    "!kind": Impl1
    x: merged
<<: *defaults
ifs:
  - "!kind": Impl1
    x: one
  - "!kind": Impl2
    y: 2
optional_if:
  "!kind": Impl2
  y: 3
label: ""
timeout: 0
`)
	var got Owner
	if err := yaml.Load(input, &got, yaml.WithV4Defaults()); err != nil {
		t.Fatal(err)
	}
	if len(got.IFaces) != 2 {
		t.Fatalf("interfaces = %#v, want two values", got.IFaces)
	}
	required, requiredOK := got.IF.(Impl1)
	first, firstOK := got.IFaces[0].(Impl1)
	second, secondOK := got.IFaces[1].(Impl2)
	optional, optionalOK := got.OptionalIF.Value.(Impl2)
	if !requiredOK || required.X != "json:merged" ||
		!firstOK || first.X != "json:one" || !secondOK || second.Y != 2 ||
		!got.OptionalIF.Present || !optionalOK || optional.Y != 3 ||
		!got.Label.Present || got.Label.Value != "" ||
		!got.Timeout.Present || got.Timeout.Value != 0 {
		t.Fatalf("decoded owner = %#v", got)
	}

	var nullish Owner
	if err := yaml.Load([]byte(`
if:
  "!kind": Impl1
  x: required
ifs: []
timeout: null
`), &nullish, yaml.WithV4Defaults()); err != nil {
		t.Fatal(err)
	}
	if nullish.Label.Present || nullish.Timeout.Present {
		t.Fatalf("absent optional and null nullable = %#v", nullish)
	}

	original := Owner{
		IF:      Impl1{X: "original"},
		IFaces:  []IFace{Impl2{Y: 7}},
		Label:   polytype.Optional[string]{Present: true, Value: "original"},
		Timeout: polytype.Nullable[int]{Present: true, Value: 9},
	}
	got = original
	bad := []byte(`
if:
  "!kind": Impl1
  x: replacement
ifs:
  - "!kind": Impl2
    y: 2
  - "!kind": unknown
`)
	err := yaml.Load(bad, &got, yaml.WithV4Defaults())
	if err == nil || !strings.Contains(err.Error(), "ifs[1]") {
		t.Fatalf("error = %v, want indexed ifs failure", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("failed decode mutated destination: got %#v, want %#v", got, original)
	}

	got = original
	err = yaml.Load([]byte(`
if:
  "!kind": Impl1
  x: replacement
ifs: []
label: null
timeout: null
`), &got, yaml.WithV4Defaults())
	if err == nil || !strings.Contains(err.Error(), "Optional value cannot be JSON null") {
		t.Fatalf("error = %v, want null Optional failure", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("failed Optional decode mutated destination: got %#v, want %#v", got, original)
	}
}

func TestGeneratedYAMLValidationUsesJSONSchema(t *testing.T) {
	valid := []byte(`
if:
  "!kind": Impl1
  x: required
ifs: []
timeout: null
`)
	if err := (Owner{}).ValidateYAML(valid); err != nil {
		t.Fatalf("valid YAML rejected: %v", err)
	}

	unknown := append(valid, []byte("surprise: true\n")...)
	if err := (Owner{}).ValidateYAML(unknown); err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("unknown-property error = %v, want surprise", err)
	}

	yamlNames := []byte(`
yaml_if:
  "!kind": Impl1
  x: required
yaml_ifs: []
timeout: null
`)
	if err := (Owner{}).ValidateYAML(yamlNames); err == nil || !strings.Contains(err.Error(), "if") {
		t.Fatalf("yaml-tag property error = %v, want schema property failure", err)
	}

	nullOptional := []byte(`
if:
  "!kind": Impl1
  x: required
ifs: []
label: null
timeout: null
`)
	if err := (Owner{}).ValidateYAML(nullOptional); err == nil || !strings.Contains(err.Error(), "label") {
		t.Fatalf("null Optional validation error = %v, want label", err)
	}
}

func TestInterfaceSliceDecode(t *testing.T) {
	var got Owner
	input := []byte(`{"if":{"!kind":"Impl1","x":"required"},"ifs":[{"!kind":"Impl1","x":"one"},{"!kind":"Impl2","y":2}]}`)
	if err := json.Unmarshal(input, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.IFaces) != 2 {
		t.Fatalf("interfaces = %#v, want two values", got.IFaces)
	}
	first, firstOK := got.IFaces[0].(Impl1)
	second, secondOK := got.IFaces[1].(Impl2)
	if !firstOK || first.X != "json:one" || !secondOK || second.Y != 2 {
		t.Fatalf("interfaces = %#v", got.IFaces)
	}
}

func TestOptionalInterfaceDecodeIsTransactional(t *testing.T) {
	original := Owner{IF: Impl1{X: "original"}}
	got := original
	if err := json.Unmarshal([]byte(`{"if":{"!kind":"Impl2","y":2},"optional_if":{"!kind":"unknown"}}`), &got); err == nil {
		t.Fatal("unknown optional interface discriminator unexpectedly succeeded")
	}
	if current, ok := got.IF.(Impl1); !ok || current.X != "original" || got.OptionalIF.Present {
		t.Fatalf("failed decode mutated destination: %#v", got)
	}
}

func TestOptionalInterfaceStates(t *testing.T) {
	var missing Owner
	if err := json.Unmarshal([]byte(`{"if":{"!kind":"Impl1","x":"required"}}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.OptionalIF.Present {
		t.Fatal("missing optional interface is present")
	}

	var present Owner
	if err := json.Unmarshal([]byte(`{"if":{"!kind":"Impl1","x":"required"},"optional_if":{"!kind":"Impl2","y":0}}`), &present); err != nil {
		t.Fatal(err)
	}
	value, ok := present.OptionalIF.Value.(Impl2)
	if !present.OptionalIF.Present || !ok || value.Y != 0 {
		t.Fatalf("present optional interface = %#v", present.OptionalIF)
	}

	got := Owner{IF: Impl1{X: "original"}}
	if err := json.Unmarshal([]byte(`{"if":{"!kind":"Impl1","x":"required"},"optional_if":null}`), &got); err == nil {
		t.Fatal("null optional interface unexpectedly succeeded")
	}
	if current, ok := got.IF.(Impl1); !ok || current.X != "original" || got.OptionalIF.Present {
		t.Fatalf("null decode mutated destination: %#v", got)
	}
}
