package enums

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tylergannon/polytype"
)

// TestMarkedEnumRoundTripAndValidation proves that a type declaring
// func (T) enum() round-trips through encoding/json with its constant
// values on the wire, and that the generated validator rejects a value
// outside the constant set.
func TestMarkedEnumRoundTripAndValidation(t *testing.T) {
	in := Task{
		ID:          "t-1",
		Name:        "ship",
		Description: polytype.Optional[string]{Present: true, Value: "ship it"},
		Status:      StatusInProgress,
		Priority:    PriorityHigh,
		Tags:        polytype.Optional[[]string]{Present: true, Value: []string{"release"}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Task{}).ValidateJSON(data); err != nil {
		t.Fatalf("valid task rejected: %v\n%s", err, data)
	}
	var out Task
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip mismatch: got %+v want %+v", out, in)
	}

	bad := []byte(`{"id":"t-1","name":"ship","description":"ship it","status":"not-a-status","priority":"high","tags":["release"]}`)
	if err := (Task{}).ValidateJSON(bad); err == nil {
		t.Fatal("expected a value outside the Status constant set to be rejected")
	}
}
