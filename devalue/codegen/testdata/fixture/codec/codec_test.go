package codec

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/devalue"

	"polytypedevaluefixture/model"
)

func sample() model.Envelope {
	envelope := model.Envelope{
		Label:   "hello",
		Numbers: model.Numbers{Flag: true, Float32: 1.5, Float64: -2.25, Int: -7, Int8: -8, Int16: 16, Int32: -32, Int64: 64, Uint: 7, Uint8: 8, Uint16: 16, Uint32: 32, Uint64: 64},
		When:    time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC),
		Coords:  [3]int{1, 2, 3},
		Tags:    []string{"a", "b"},
		Detail:  &model.Detail{Note: "note"},

		Priority: model.PriorityHigh,
		Ranked:   model.PriorityLow,
		Status:   model.StatusDone,
		Shade:    model.ShadeRed,
		Primary:  model.Created{Name: "created"},
		Events:   []model.Event{model.Created{Name: "one"}, &model.Deleted{ID: "two"}},
	}
	envelope.Inline.Text = "inline"
	envelope.Inline.Depth = 2
	return envelope
}

// TestInlineObjectRoundTrips covers the anonymous struct field, which the
// emitter can only reach through selectors on the Envelope value.
func TestInlineObjectRoundTrips(t *testing.T) {
	object := encode(t, sample())
	inline, ok := object.Get("inline")
	if !ok {
		t.Fatal("inline property is absent")
	}
	nested, ok := inline.(*devalue.Object)
	if !ok {
		t.Fatalf("inline = %#v, want *devalue.Object", inline)
	}
	if text, _ := nested.Get("text"); text != "inline" {
		t.Fatalf("inline.text = %#v, want \"inline\"", text)
	}
	nested.Set("depth", "two")
	_, err := DecodeEnvelope(object)
	assertErrorContains(t, err, "/inline/depth", "expected a number")
}

// TestInlineObjectUnderPresenceWrappers covers the other two positions an
// anonymous struct can occupy: the operand of an Optional and of a Nullable.
func TestInlineObjectUnderPresenceWrappers(t *testing.T) {
	want := sample()
	want.Extra.Present = true
	want.Extra.Value.Note = "extra"
	want.Marker.Present = true
	want.Marker.Value.Seen = true

	object := encode(t, want)
	if marker, _ := encode(t, sample()).Get("marker"); marker != nil {
		t.Fatalf("absent nullable marker = %#v, want null", marker)
	}
	got, err := DecodeEnvelope(object)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Extra != want.Extra || got.Marker != want.Marker {
		t.Fatalf("extra = %#v, marker = %#v", got.Extra, got.Marker)
	}
}

func encode(t *testing.T, v model.Envelope) *devalue.Object {
	t.Helper()
	encoded, err := EncodeEnvelope(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	object, ok := encoded.(*devalue.Object)
	if !ok {
		t.Fatalf("encoded value is %T, want *devalue.Object", encoded)
	}
	return object
}

// without copies o omitting key, since devalue.Object has no delete.
func without(o *devalue.Object, key string) *devalue.Object {
	out := devalue.NewObject()
	for _, k := range o.Keys() {
		if k == key {
			continue
		}
		v, _ := o.Get(k)
		out.Set(k, v)
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	want := sample()
	want.Alternate = polytype.Optional[model.Event]{Present: true, Value: &model.Deleted{ID: "alt"}}
	want.Nickname = polytype.Optional[string]{Present: true, Value: "nick"}
	want.Owner = polytype.Nullable[model.Detail]{Present: true, Value: model.Detail{Note: "owner"}}

	got, err := DecodeEnvelope(encode(t, want))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.When.Equal(want.When) {
		t.Fatalf("When = %v, want %v", got.When, want.When)
	}
	got.When = want.When
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the value:\n got %#v\nwant %#v", got, want)
	}
}

func TestStringifyParseRoundTrip(t *testing.T) {
	want := sample()
	serialized, err := StringifyEnvelope(want)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	got, err := ParseEnvelope(serialized)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got.When = want.When
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip through devalue changed the value:\n got %#v\nwant %#v", got, want)
	}
}

func TestNilRequiredSliceEncodesAsEmptyArray(t *testing.T) {
	value := sample()
	value.Tags = nil
	tags, ok := encode(t, value).Get("tags")
	if !ok {
		t.Fatal("tags property is absent")
	}
	items, ok := tags.([]any)
	if !ok || items == nil {
		t.Fatalf("tags = %#v, want a non-nil empty slice", tags)
	}
	if len(items) != 0 {
		t.Fatalf("tags = %#v, want an empty array", items)
	}
}

func TestAbsentOptionalIsNotAKey(t *testing.T) {
	object := encode(t, sample())
	for _, key := range object.Keys() {
		if key == "nickname" || key == "alternate" {
			t.Fatalf("absent optional %q was written", key)
		}
	}
}

func TestNullableZeroEncodesAsNull(t *testing.T) {
	owner, ok := encode(t, sample()).Get("owner")
	if !ok {
		t.Fatal("owner property is absent")
	}
	if owner != nil {
		t.Fatalf("owner = %#v, want null", owner)
	}
}

func TestDecodeRejectsMissingRequiredProperty(t *testing.T) {
	_, err := DecodeEnvelope(without(encode(t, sample()), "label"))
	assertErrorContains(t, err, "/label", "missing required property")
}

func TestDecodeRejectsWrongKind(t *testing.T) {
	object := encode(t, sample())
	object.Set("label", float64(3))
	_, err := DecodeEnvelope(object)
	assertErrorContains(t, err, "/label", "expected a string")
}

func TestDecodeRejectsEnumNonMember(t *testing.T) {
	object := encode(t, sample())
	object.Set("priority", float64(3))
	_, err := DecodeEnvelope(object)
	assertErrorContains(t, err, "/priority", "not a member of enum")
}

// TestAliasedEnumMembersShareOneWireValue covers the enum shape the grammar
// admits in value mode but the fixture otherwise misses: two members with one
// underlying value. The generated switches carry a single case for the pair,
// so encoding either member writes that value and decoding it yields the first
// member declaring it.
func TestAliasedEnumMembersShareOneWireValue(t *testing.T) {
	for _, member := range []model.Shade{model.ShadeRed, model.ShadeCrimson} {
		value := sample()
		value.Shade = member
		shade, ok := encode(t, value).Get("shade")
		if !ok {
			t.Fatal("shade property is absent")
		}
		if shade != float64(1) {
			t.Fatalf("shade = %#v, want 1", shade)
		}
	}

	object := encode(t, sample())
	object.Set("shade", float64(1))
	got, err := DecodeEnvelope(object)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Shade != model.ShadeRed {
		t.Fatalf("shade = %d, want ShadeRed (%d)", got.Shade, model.ShadeRed)
	}
}

// TestEnumMemberBeyondSafeIntegerIsALossyNumber pins the decided wire mapping:
// every numeric kind is a JavaScript number, so a member past 2^53 rounds
// rather than being refused. The Go constant is exact, so the value still
// round-trips.
func TestEnumMemberBeyondSafeIntegerIsALossyNumber(t *testing.T) {
	value := sample()
	value.Shade = model.ShadeVivid
	object := encode(t, value)
	shade, _ := object.Get("shade")
	number, ok := shade.(float64)
	if !ok {
		t.Fatalf("shade = %#v, want a number", shade)
	}
	// 9007199254740993 has no float64, so the wire carries 9007199254740992:
	// the precision loss the package documents rather than an encode error.
	if number != 9007199254740992 {
		t.Fatalf("shade = %v, want the rounded 9007199254740992", number)
	}
	if int64(number) == int64(model.ShadeVivid) {
		t.Fatal("test is vacuous: the member was exactly representable")
	}
	got, err := DecodeEnvelope(object)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Shade != model.ShadeVivid {
		t.Fatalf("shade = %d, want ShadeVivid (%d)", got.Shade, model.ShadeVivid)
	}
}

func TestDecodeRejectsUnknownProperty(t *testing.T) {
	object := encode(t, sample())
	object.Set("surprise", "x")
	_, err := DecodeEnvelope(object)
	assertErrorContains(t, err, "unknown property", "surprise")
}

// TestStringerEnumUsesConstantNames pins the two enum modes apart: the
// StringerEnum field carries the constant name, the plain field the value.
func TestStringerEnumUsesConstantNames(t *testing.T) {
	object := encode(t, sample())
	if ranked, _ := object.Get("ranked"); ranked != "PriorityLow" {
		t.Fatalf("ranked = %#v, want \"PriorityLow\"", ranked)
	}
	if priority, _ := object.Get("priority"); priority != float64(5) {
		t.Fatalf("priority = %#v, want 5", priority)
	}
}

// TestRootCodecsExist keeps the root entry points referenced, so a change that
// stopped emitting them would fail to compile here.
func TestRootCodecsExist(t *testing.T) {
	encoded, err := EncodeRoot0([]model.Envelope{sample()})
	if err != nil {
		t.Fatalf("encode root: %v", err)
	}
	decoded, err := DecodeRoot0(encoded)
	if err != nil {
		t.Fatalf("decode root: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded %d envelopes, want 1", len(decoded))
	}
	encodedDetail, err := EncodeRoot1(model.Detail{Note: "n"})
	if err != nil {
		t.Fatalf("encode root: %v", err)
	}
	detail, err := ParseRoot1(mustStringify(t, encodedDetail))
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	if detail.Note != "n" {
		t.Fatalf("detail = %#v", detail)
	}
}

func mustStringify(t *testing.T, v any) string {
	t.Helper()
	s, err := devalue.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return s
}

func assertErrorContains(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error %q does not contain %q", err, fragment)
		}
	}
}
