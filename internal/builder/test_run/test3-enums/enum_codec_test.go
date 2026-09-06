package basictypes

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEnumCodecRejectsJSONNull proves the generated type-level decoder rejects
// JSON null even when the underlying zero value is a declared member, which a
// plain decode into the underlying type would silently accept.
func TestEnumCodecRejectsJSONNull(t *testing.T) {
	stringValue := ZeroValueSet
	if err := stringValue.UnmarshalJSON([]byte("null")); err == nil {
		t.Fatalf("expected null to be rejected, got %q", stringValue)
	}
	if stringValue != ZeroValueSet {
		t.Fatalf("rejected decode modified the receiver: %q", stringValue)
	}

	intValue := ZeroCodeOne
	if err := intValue.UnmarshalJSON([]byte("null")); err == nil {
		t.Fatalf("expected null to be rejected, got %v", intValue)
	}
	if intValue != ZeroCodeOne {
		t.Fatalf("rejected decode modified the receiver: %v", intValue)
	}
}

// TestEnumCodecRoundTripsZeroMembers proves the zero value of each underlying
// type still round-trips when it is a declared member.
func TestEnumCodecRoundTripsZeroMembers(t *testing.T) {
	for _, test := range []struct {
		wire  string
		value json.Unmarshaler
		want  any
	}{
		{wire: `""`, value: new(ZeroValueEnum), want: ZeroValueEmpty},
		{wire: `0`, value: new(ZeroCodeEnum), want: ZeroCodeNone},
	} {
		if err := test.value.UnmarshalJSON([]byte(test.wire)); err != nil {
			t.Fatalf("decoding %s: %v", test.wire, err)
		}
		encoded, err := json.Marshal(test.value)
		if err != nil {
			t.Fatalf("encoding %s: %v", test.wire, err)
		}
		if string(encoded) != test.wire {
			t.Fatalf("round trip of %s produced %s", test.wire, encoded)
		}
	}
}

// TestEnumCodecRejectsNonMembers proves a value outside the declared set is
// rejected on both sides.
func TestEnumCodecRejectsNonMembers(t *testing.T) {
	var value ZeroValueEnum
	if err := value.UnmarshalJSON([]byte(`"nope"`)); err == nil {
		t.Fatal("expected a non-member to be rejected on decode")
	}
	if _, err := ZeroValueEnum("nope").MarshalJSON(); err == nil {
		t.Fatal("expected a non-member to be rejected on encode")
	}
}

// TestEnumCodecThroughStruct proves the generated codec is what a consumer of
// the generated package actually reaches: encoding/json on the enclosing
// struct, not the enum methods called directly.
func TestEnumCodecThroughStruct(t *testing.T) {
	_, err := json.Marshal(EnumHolder{Name: "widget"})
	if err == nil {
		t.Fatal("expected the zero-valued enum field to fail encoding")
	}
	if !strings.Contains(err.Error(), "EnumType") || !strings.Contains(err.Error(), `""`) {
		t.Fatalf("error names neither the enum type nor the offending value: %v", err)
	}

	const document = `{"kind":"val2","name":"widget"}`

	encoded, err := json.Marshal(EnumHolder{Kind: EnumVal2, Name: "widget"})
	if err != nil {
		t.Fatalf("encoding a member: %v", err)
	}
	if string(encoded) != document {
		t.Fatalf("encoded %s, want %s", encoded, document)
	}

	var decoded EnumHolder
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("decoding %s: %v", document, err)
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-encoding %s: %v", document, err)
	}
	if string(reencoded) != document {
		t.Fatalf("round trip of %s produced %s", document, reencoded)
	}

	if err := json.Unmarshal([]byte(`{"kind":"nope","name":"widget"}`), &decoded); err == nil {
		t.Fatal("expected a non-member field value to be rejected on decode")
	}
}
