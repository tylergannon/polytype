package crosspkg_union

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	shapes "github.com/tylergannon/polytype/internal/builder/testfixtures/crosspkg_shapes"
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

func TestForeignUnionRoundTrips(t *testing.T) {
	circle := shapes.Holder{S: shapes.Circle{Radius: 1}}
	roundTrip(t, Named{H: circle}, `{"h":{"s":{"type":"Circle","radius":1}}}`)
	roundTrip(t, Many{Items: []shapes.Holder{circle}}, `{"items":[{"s":{"type":"Circle","radius":1}}]}`)
	roundTrip(t, Embedded{Plain: shapes.Plain{S: shapes.Circle{Radius: 1}}, Local: Square{Side: 2}},
		`{"s":{"type":"Circle","radius":1},"local":{"type":"Square","side":2}}`)
}

// TestSchemasNameTheForeignVariants guards against resolving the promoted
// field against this package's Shape, which would list Square under "s".
func TestSchemasNameTheForeignVariants(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(Embedded{}.Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	if s := string(schema.Properties["s"]); !strings.Contains(s, `"Circle"`) || strings.Contains(s, `"Square"`) {
		t.Fatalf("property s = %s, want the shapes.Shape variants", s)
	}
	if local := string(schema.Properties["local"]); !strings.Contains(local, `"Square"`) || strings.Contains(local, `"Circle"`) {
		t.Fatalf("property local = %s, want the local Shape variants", local)
	}
}
