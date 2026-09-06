package v1_enums_stringmode

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/internal/builder/testfixtures/v1_enums_stringmode/palette"
)

func TestStringModeEnumRoundTripAndFieldModes(t *testing.T) {
	colorStringCalls = 0
	want := Paint{
		C:        ColorGreen,
		Optional: polytype.Optional[Color]{Present: true, Value: ColorBlue},
		Nullable: polytype.Nullable[Color]{Present: true, Value: ColorRed},
		Numeric:  ColorGreen,
		Finish:   FinishReady,
		Remote:   palette.LevelHigh,
	}
	valueJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	pointerJSON, err := json.Marshal(&want)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(valueJSON, pointerJSON) {
		t.Fatalf("value JSON = %s, pointer JSON = %s", valueJSON, pointerJSON)
	}
	if err := (Paint{}).ValidateJSON(valueJSON); err != nil {
		t.Fatalf("schema rejected generated JSON: %v\n%s", err, valueJSON)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(valueJSON, &wire); err != nil {
		t.Fatal(err)
	}
	assertString(t, wire["c"], "ColorGreen")
	assertString(t, wire["optional"], "ColorBlue")
	assertString(t, wire["nullable"], "ColorRed")
	assertString(t, wire["finish"], `ready"now`)
	assertString(t, wire["remote"], "LevelHigh")
	if string(wire["numeric"]) != "7" {
		t.Fatalf("numeric = %s, want 7", wire["numeric"])
	}
	var got Paint
	if err := json.Unmarshal(valueJSON, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded = %#v, want %#v", got, want)
	}
	if colorStringCalls != 0 {
		t.Fatalf("Color.String calls = %d, want 0", colorStringCalls)
	}
}

func TestStringModeEnumWrappersAndErrors(t *testing.T) {
	value := Paint{C: ColorRed, Nullable: polytype.Nullable[Color]{}, Numeric: ColorBlue, Finish: FinishDone, Remote: palette.LevelLow}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Paint{}).ValidateJSON(data); err != nil {
		t.Fatalf("schema rejected absent optional/null nullable: %v\n%s", err, data)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["optional"]; ok {
		t.Fatalf("absent Optional was encoded: %s", data)
	}
	if string(wire["nullable"]) != "null" {
		t.Fatalf("nullable = %s, want null", wire["nullable"])
	}

	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{name: "unknown name", input: []byte(`{"c":"missing","nullable":null,"numeric":7,"finish":"done","remote":"LevelLow"}`), want: "unknown string-mode enum Color name"},
		{name: "numeric string-mode token", input: []byte(`{"c":7,"nullable":null,"numeric":7,"finish":"done","remote":"LevelLow"}`), want: "requires a JSON string"},
		{name: "optional null", input: []byte(`{"c":"ColorRed","optional":null,"nullable":null,"numeric":7,"finish":"done","remote":"LevelLow"}`), want: "Optional value cannot be JSON null"},
		{name: "nullable absent", input: []byte(`{"c":"ColorRed","numeric":7,"finish":"done","remote":"LevelLow"}`), want: "required nullable string-mode enum is missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Paint{C: ColorBlue, Numeric: ColorBlue, Finish: FinishReady}
			before := got
			err := json.Unmarshal(test.input, &got)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if !reflect.DeepEqual(got, before) {
				t.Fatalf("failed decode mutated receiver: got %#v, want %#v", got, before)
			}
		})
	}

	declaredZero := Paint{
		C:        ColorZero,
		Optional: polytype.Optional[Color]{Present: true, Value: ColorZero},
		Nullable: polytype.Nullable[Color]{Present: true, Value: ColorZero},
		Numeric:  ColorZero,
		Finish:   FinishDone,
		Remote:   palette.LevelLow,
	}
	data, err = json.Marshal(declaredZero)
	if err != nil {
		t.Fatalf("declared zero encode: %v", err)
	}
	if err := (Paint{}).ValidateJSON(data); err != nil {
		t.Fatalf("schema rejected declared zero wrappers: %v\n%s", err, data)
	}
	var decodedZero Paint
	if err := json.Unmarshal(data, &decodedZero); err != nil {
		t.Fatalf("declared zero decode: %v", err)
	}
	if !reflect.DeepEqual(decodedZero, declaredZero) {
		t.Fatalf("decoded declared zero = %#v, want %#v", decodedZero, declaredZero)
	}

	value.Remote = 0
	if _, err := json.Marshal(value); err == nil || !strings.Contains(err.Error(), "undeclared value") {
		t.Fatalf("zero-value encode error = %v", err)
	}
}

func assertString(t *testing.T, data json.RawMessage, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("value = %q, want %q", got, want)
	}
}

// TestOwnerAndTypeLevelEnumCodecsCoexist covers the one case where the
// two enum codecs could interfere: Color is used in string mode by the
// owner codec on Paint.C and left in numeric mode on Paint.Numeric, where
// the type-level Color codec is what encodes it.
func TestOwnerAndTypeLevelEnumCodecsCoexist(t *testing.T) {
	colorStringCalls = 0
	value := Paint{
		C:        ColorBlue,
		Optional: polytype.Optional[Color]{Present: true, Value: ColorGreen},
		Nullable: polytype.Nullable[Color]{Present: true, Value: ColorZero},
		Numeric:  ColorRed,
		Finish:   FinishDone,
		Remote:   palette.LevelHigh,
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	assertString(t, wire["c"], "ColorBlue")
	if string(wire["numeric"]) != "-2" {
		t.Fatalf("numeric = %s, want -2", wire["numeric"])
	}

	var decoded Paint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, value) {
		t.Fatalf("decoded = %#v, want %#v", decoded, value)
	}
	again, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, data) {
		t.Fatalf("round trip = %s, want %s", again, data)
	}

	const nonMember = Color(99)

	numericNonMember := value
	numericNonMember.Numeric = nonMember
	_, err = json.Marshal(numericNonMember)
	if err == nil || !strings.Contains(err.Error(), "99 is not a declared member of enum Color") {
		t.Fatalf("numeric non-member encode error = %v", err)
	}

	stringNonMember := value
	stringNonMember.C = nonMember
	_, err = json.Marshal(stringNonMember)
	if err == nil || !strings.Contains(err.Error(), "undeclared value for string-mode enum Color") {
		t.Fatalf("string-mode non-member encode error = %v", err)
	}

	if colorStringCalls != 0 {
		t.Fatalf("Color.String calls = %d, want 0", colorStringCalls)
	}
}
