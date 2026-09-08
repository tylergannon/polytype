package model

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestGeneratedJSONCodecs(t *testing.T) {
	want := Envelope{Message: "hello", Count: 2, State: StateReady, Event: HTTPEventCreated{ID: "evt-1"}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"kind":"wire_http_event_created"`)) {
		t.Fatalf("custom discriminator inflection missing from %s", data)
	}
	var got Envelope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	created, ok := got.Event.(HTTPEventCreated)
	if !ok || created.ID != "evt-1" || got.State != StateReady {
		t.Fatalf("unexpected round trip: %#v", got)
	}
}
