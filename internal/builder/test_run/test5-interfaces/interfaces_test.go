package interfaces

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestLegacyInterfaceSliceDecode(t *testing.T) {
	var got FancyStruct
	input := []byte(`{"iface":{"type":"TestInterface1","field1":"one"},"ifaces":[{"type":"TestInterface2","fork3":3},{"type":"PointerToTestInterface","fork99":99}]}`)
	if err := json.Unmarshal(input, &got); err != nil {
		t.Fatal(err)
	}
	want := []TestInterface{TestInterface2{Fork3: 3}, &PointerToTestInterface{Fork99: 99}}
	if !reflect.DeepEqual(got.IFaces, want) {
		t.Fatalf("ifaces = %#v, want %#v", got.IFaces, want)
	}
}

func TestLegacyInterfaceMarshalCarriesDerivedDiscriminators(t *testing.T) {
	value := FancyStruct{
		IFace:  TestInterface1{Field1: "one"},
		IFaces: []TestInterface{TestInterface2{Fork3: 3}, &PointerToTestInterface{Fork99: 99}},
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		IFace  map[string]json.RawMessage   `json:"iface"`
		IFaces []map[string]json.RawMessage `json:"ifaces"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		object map[string]json.RawMessage
		value  string
	}{
		{object: wire.IFace, value: "TestInterface1"},
		{object: wire.IFaces[0], value: "TestInterface2"},
		{object: wire.IFaces[1], value: "PointerToTestInterface"},
	}
	for _, item := range want {
		var got string
		if err := json.Unmarshal(item.object["type"], &got); err != nil {
			t.Fatal(err)
		}
		if got != item.value {
			t.Fatalf("type = %q, want %q in %s", got, item.value, encoded)
		}
	}
	var decoded FancyStruct
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.EnumVal == nil || decoded.Details == nil || len(decoded.EnumVal) != 0 || len(decoded.Details) != 0 {
		t.Fatalf("normalized ordinary slices = enumVal %#v, details %#v; want non-nil empty slices", decoded.EnumVal, decoded.Details)
	}
	value.EnumVal = decoded.EnumVal
	value.Details = decoded.Details
	if !reflect.DeepEqual(decoded, value) {
		t.Fatalf("round trip = %#v, want %#v", decoded, value)
	}
}

func TestLegacyInterfaceSliceErrorIsIndexedAndTransactional(t *testing.T) {
	original := FancyStruct{IFace: TestInterface1{Field1: "original"}, IFaces: []TestInterface{TestInterface2{Fork3: 7}}}
	got := original
	input := []byte(`{"iface":{"type":"TestInterface1","field1":"replacement"},"ifaces":[{"type":"TestInterface2","fork3":3},{"type":"unknown"}]}`)
	err := json.Unmarshal(input, &got)
	if err == nil || !strings.Contains(err.Error(), "ifaces[1]") {
		t.Fatalf("error = %v, want indexed failure", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("failed decode mutated destination: got %#v, want %#v", got, original)
	}
}
