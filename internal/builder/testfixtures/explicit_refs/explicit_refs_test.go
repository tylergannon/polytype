package explicit_refs

import (
	"encoding/json"
	"reflect"
	"testing"
)

func roundTrip[T any](t *testing.T, want T, wantJSON string) {
	t.Helper()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var gotWire, wantWire any
	if err := json.Unmarshal(encoded, &gotWire); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantWire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotWire, wantWire) {
		t.Fatalf("encoded %s, want %s", encoded, wantJSON)
	}
	var got T
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded %#v, want %#v", got, want)
	}
}

// The explicit ref decides the schema; the Go type still decides the wire.
func TestRefTaggedFieldsKeepTheirCodecs(t *testing.T) {
	roundTrip(t, Owner{S: Circle{Radius: 1}, L: High}, `{"s":{"type":"Circle","radius":1},"l":"High"}`)
	roundTrip(t, Linked{H: Holder{S: Circle{Radius: 2}}}, `{"h":{"s":{"type":"Circle","radius":2}}}`)
}

func TestExplicitRefWinsInSchema(t *testing.T) {
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(Owner{}.Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	for name, ref := range map[string]string{"s": "#/definitions/Shape", "l": "#/definitions/Level"} {
		if got := schema.Properties[name]; !reflect.DeepEqual(got, map[string]any{"$ref": ref}) {
			t.Errorf("property %s = %v, want $ref %s", name, got, ref)
		}
	}
}
