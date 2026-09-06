package basictypes

import (
	"encoding/json"
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
