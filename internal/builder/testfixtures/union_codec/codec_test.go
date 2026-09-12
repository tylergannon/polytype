package union_codec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/tylergannon/polytype"
)

func validEnvelope() Envelope {
	return Envelope{
		Primary:   Created{Name: "first"},
		Events:    []Event{Created{Name: "second"}, &Deleted{ID: "gone"}},
		Optional:  polytype.Optional[Event]{Present: true, Value: &Deleted{ID: "optional"}},
		Alternate: polytype.Optional[Event]{Present: true, Value: Created{Name: "alternate"}},
		Single:    polytype.Optional[Event]{Present: true, Value: Created{Name: "single"}},
		Hook:      polytype.Optional[Event]{Present: true, Value: Hooked{Name: "hook"}},
		ValueHook: polytype.Optional[Event]{Present: true, Value: PointerHookValue{Name: "value-hook"}},
		Nested:    Nested{Event: &Deleted{ID: "nested"}},
		Ordinary:  Ordinary{Value: "ordinary"},
		State:     StateClosed,
		Label:     "label",
	}
}

func TestGeneratedUnionMarshalValidateDecodeRoundTrip(t *testing.T) {
	want := validEnvelope()

	hookMarshalCalls = 0
	ordinaryMarshalCalls = 0
	pointerValueHookMarshalCalls = 0
	encodedValue, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if hookMarshalCalls != 1 || ordinaryMarshalCalls != 1 || pointerValueHookMarshalCalls != 1 {
		t.Fatalf("nested marshal calls = hook %d, ordinary %d, pointer value %d; want one each", hookMarshalCalls, ordinaryMarshalCalls, pointerValueHookMarshalCalls)
	}
	if err := (Envelope{}).ValidateJSON(encodedValue); err != nil {
		t.Fatalf("generated schema rejected generated JSON: %v\n%s", err, encodedValue)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(encodedValue, &root); err != nil {
		t.Fatal(err)
	}
	if string(root["tags"]) != "[]" || string(root["groups"]) != "[]" {
		t.Fatalf("nil ordinary slices = tags %s, groups %s; want [] through a v1 caller of the generated codec", root["tags"], root["groups"])
	}

	hookMarshalCalls = 0
	ordinaryMarshalCalls = 0
	pointerValueHookMarshalCalls = 0
	encodedPointer, err := json.Marshal(&want)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(encodedPointer, encodedValue) {
		t.Fatalf("pointer encoding = %s, value encoding = %s", encodedPointer, encodedValue)
	}
	if hookMarshalCalls != 1 || ordinaryMarshalCalls != 1 || pointerValueHookMarshalCalls != 1 {
		t.Fatalf("pointer nested marshal calls = hook %d, ordinary %d, pointer value %d; want one each", hookMarshalCalls, ordinaryMarshalCalls, pointerValueHookMarshalCalls)
	}

	assertDiscriminators(t, encodedValue)

	var decoded Envelope
	if err := json.Unmarshal(encodedValue, &decoded); err != nil {
		t.Fatal(err)
	}
	assertEnvelopeMeaning(t, decoded)

	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Envelope{}).ValidateJSON(reencoded); err != nil {
		t.Fatalf("generated schema rejected re-encoded JSON: %v\n%s", err, reencoded)
	}
	var decodedAgain Envelope
	if err := json.Unmarshal(reencoded, &decodedAgain); err != nil {
		t.Fatal(err)
	}
	assertEnvelopeMeaning(t, decodedAgain)
}

func TestOptionalUnionAbsenceAndEmptySlice(t *testing.T) {
	value := validEnvelope()
	value.Events = []Event{}
	value.Optional = polytype.Optional[Event]{}
	value.Alternate = polytype.Optional[Event]{}
	value.Single = polytype.Optional[Event]{}
	value.Hook = polytype.Optional[Event]{}
	value.ValueHook = polytype.Optional[Event]{}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"optional", "alternate", "single", "hook", "value_hook", "omitted"} {
		if _, ok := object[field]; ok {
			t.Fatalf("absent optional field %q was encoded: %s", field, encoded)
		}
	}
	if string(object["events"]) != "[]" {
		t.Fatalf("empty events = %s, want []", object["events"])
	}
}

func TestUnionMarshalErrors(t *testing.T) {
	var typedNil *Deleted
	tests := []struct {
		name string
		edit func(*Envelope)
		want string
	}{
		{name: "nil scalar", edit: func(v *Envelope) { v.Primary = nil }, want: "field primary: cannot marshal nil registered interface"},
		{name: "typed nil scalar", edit: func(v *Envelope) { v.Primary = typedNil }, want: "typed nil registered implementation"},
		{name: "nil slice", edit: func(v *Envelope) { v.Events = nil }, want: "field events: nil registered interface slice"},
		{name: "nil slice element", edit: func(v *Envelope) { v.Events[1] = nil }, want: "field events[1]: cannot marshal nil registered interface"},
		{name: "typed nil slice element", edit: func(v *Envelope) { v.Events[1] = typedNil }, want: "field events[1]: cannot marshal typed nil"},
		{name: "present nil optional", edit: func(v *Envelope) { v.Optional = polytype.Optional[Event]{Present: true} }, want: "field optional: cannot marshal nil registered interface"},
		{name: "custom conflict", edit: func(v *Envelope) { v.Hook.Value = Hooked{Name: "x", Behavior: "conflict"} }, want: "is \"other\", want registered value \"Hooked\""},
		{name: "custom non-string discriminator", edit: func(v *Envelope) { v.Hook.Value = Hooked{Name: "x", Behavior: "non-string"} }, want: "discriminator property \"!kind\" must be a string"},
		{name: "custom null", edit: func(v *Envelope) { v.Hook.Value = Hooked{Name: "x", Behavior: "null"} }, want: "must encode as a JSON object, got null"},
		{name: "custom array", edit: func(v *Envelope) { v.Hook.Value = Hooked{Name: "x", Behavior: "array"} }, want: "must encode as a JSON object"},
		{name: "custom string", edit: func(v *Envelope) { v.Hook.Value = Hooked{Name: "x", Behavior: "string"} }, want: "must encode as a JSON object"},
		{name: "custom malformed JSON", edit: func(v *Envelope) { v.Hook.Value = Hooked{Name: "x", Behavior: "malformed"} }, want: "error calling MarshalJSON"},
		{name: "custom error", edit: func(v *Envelope) { v.Hook.Value = Hooked{Name: "x", Behavior: "error"} }, want: "hook failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validEnvelope()
			test.edit(&value)
			_, err := json.Marshal(value)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNullDiscriminatorIsRejected(t *testing.T) {
	var decoded Envelope
	nullInput := []byte(`{"primary":{"!kind":null,"name":"rejected"}}`)
	if err := json.Unmarshal(nullInput, &decoded); err == nil || !strings.Contains(err.Error(), "JSON null is not a string") {
		t.Fatalf("null discriminator error = %v", err)
	}
}

func TestCustomUnionHookAcceptsMissingAndMatchingDiscriminator(t *testing.T) {
	for _, behavior := range []string{"", "matching"} {
		t.Run(behavior, func(t *testing.T) {
			value := validEnvelope()
			value.Hook.Value = Hooked{Name: "accepted", Behavior: behavior}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Envelope
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			hooked, ok := decoded.Hook.Value.(Hooked)
			if !ok || hooked.Name != "accepted" || !hooked.SawDiscriminator {
				t.Fatalf("decoded custom hook = %#v", decoded.Hook.Value)
			}
		})
	}
}

func TestGeneratedDecodeErrorIsTransactionalAndSuccessReplaces(t *testing.T) {
	original := validEnvelope()
	got := original
	err := json.Unmarshal([]byte(`{"primary":{"!kind":"Created","name":"replacement"},"events":[{"!kind":"Created","name":"ok"},{"!kind":"unknown"}]}`), &got)
	if err == nil || !strings.Contains(err.Error(), "events[1]") {
		t.Fatalf("error = %v, want indexed failure", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("failed decode mutated destination: got %#v, want %#v", got, original)
	}

	got = original
	input := []byte(`{"primary":{"!kind":"Created","name":"replacement"},"events":[],"nested":{"event":{"!kind":"Created","name":"nested"}},"ordinary":{"value":"new"},"state":"StateOpen","label":"new","tags":[],"groups":[]}`)
	if err := (Envelope{}).ValidateJSON(input); err != nil {
		t.Fatalf("manual replacement input failed schema validation: %v", err)
	}
	if err := json.Unmarshal(input, &got); err != nil {
		t.Fatal(err)
	}
	if got.Optional.Present || got.Alternate.Present || got.Single.Present || got.Hook.Present || got.ValueHook.Present || got.Omitted.Present {
		t.Fatalf("omitted fields retained old values: %#v", got)
	}
	if got.Events == nil || len(got.Events) != 0 {
		t.Fatalf("events = %#v, want non-nil empty replacement", got.Events)
	}
	if got.State != StateOpen {
		t.Fatalf("state = %v, want StateOpen", got.State)
	}
}

func assertDiscriminators(t *testing.T, data []byte) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	assertObjectString(t, root["primary"], "!kind", "Created")
	assertObjectString(t, root["optional"], "!kind", "Deleted")
	assertObjectString(t, root["alternate"], "!kind", "Created")
	assertObjectString(t, root["single"], "!kind", "Created")
	assertObjectString(t, root["hook"], "!kind", "Hooked")
	assertObjectString(t, root["value_hook"], "!kind", "PointerHookValue")
	assertObjectString(t, root["value_hook"], "name", "custom:value-hook")
	assertObjectString(t, root["ordinary"], "value", "custom:ordinary")
	assertStringValue(t, root["state"], "StateClosed")
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(root["nested"], &nested); err != nil {
		t.Fatal(err)
	}
	assertObjectString(t, nested["event"], "!kind", "Deleted")
	var events []json.RawMessage
	if err := json.Unmarshal(root["events"], &events); err != nil {
		t.Fatal(err)
	}
	assertObjectString(t, events[0], "!kind", "Created")
	assertObjectString(t, events[1], "!kind", "Deleted")
}

func assertObjectString(t *testing.T, data []byte, key, want string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := json.Unmarshal(object[key], &got); err != nil {
		t.Fatalf("decode %q from %s: %v", key, data, err)
	}
	if got != want {
		t.Fatalf("%q = %q, want %q in %s", key, got, want, data)
	}
}

func assertEnvelopeMeaning(t *testing.T, got Envelope) {
	t.Helper()
	if value, ok := got.Primary.(Created); !ok || value.Name != "first" {
		t.Fatalf("primary = %#v", got.Primary)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events = %#v", got.Events)
	}
	if value, ok := got.Events[0].(Created); !ok || value.Name != "second" {
		t.Fatalf("events[0] = %#v", got.Events[0])
	}
	if value, ok := got.Events[1].(*Deleted); !ok || value.ID != "gone" {
		t.Fatalf("events[1] = %#v", got.Events[1])
	}
	if value, ok := got.Optional.Value.(*Deleted); !got.Optional.Present || !ok || value.ID != "optional" {
		t.Fatalf("optional = %#v", got.Optional)
	}
	if value, ok := got.Alternate.Value.(Created); !got.Alternate.Present || !ok || value.Name != "alternate" {
		t.Fatalf("alternate = %#v", got.Alternate)
	}
	if value, ok := got.Single.Value.(Created); !got.Single.Present || !ok || value.Name != "single" {
		t.Fatalf("single = %#v", got.Single)
	}
	if value, ok := got.Hook.Value.(Hooked); !got.Hook.Present || !ok || value.Name != "hook" || !value.SawDiscriminator {
		t.Fatalf("hook = %#v", got.Hook)
	}
	if value, ok := got.ValueHook.Value.(PointerHookValue); !got.ValueHook.Present || !ok || value.Name != "value-hook" || !value.SawDiscriminator {
		t.Fatalf("value hook = %#v", got.ValueHook)
	}
	if value, ok := got.Nested.Event.(*Deleted); !ok || value.ID != "nested" {
		t.Fatalf("nested = %#v", got.Nested)
	}
	if got.Ordinary.Value != "ordinary" || got.State != StateClosed || got.Label != "label" || got.Omitted.Present {
		t.Fatalf("ordinary fields = %#v", got)
	}
}

func assertStringValue(t *testing.T, data []byte, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("value = %q, want %q", got, want)
	}
}
