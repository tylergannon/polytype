package builder_test

import (
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

func TestCanonicalJSONEncoderNormalizesNilSlicesRecursively(t *testing.T) {
	type document struct {
		Tags   []string   `json:"tags"`
		Groups [][]string `json:"groups"`
	}

	for _, test := range []struct {
		name    string
		options []jsonv2.Options
	}{
		{name: "v2 defaults"},
		{name: "v1 compatibility except slices", options: []jsonv2.Options{jsonv1.DefaultOptionsV1(), jsonv2.FormatNilSliceAsNull(false)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := jsonv2.Marshal(document{Groups: [][]string{nil}}, test.options...)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(data), `{"tags":[],"groups":[[]]}`; got != want {
				t.Fatalf("canonical JSON = %s, want %s", got, want)
			}
		})
	}
}
