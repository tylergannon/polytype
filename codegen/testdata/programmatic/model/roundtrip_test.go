package model

import (
	"encoding/json"
	"testing"
)

func TestGeneratedJSONCodecs(t *testing.T) {
	want := Envelope{Message: "hello", Count: 2, State: StateReady, Event: Created{ID: "evt-1"}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	created, ok := got.Event.(Created)
	if !ok || created.ID != "evt-1" || got.State != StateReady {
		t.Fatalf("unexpected round trip: %#v", got)
	}
}
