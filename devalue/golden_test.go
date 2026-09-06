package devalue_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/tylergannon/polytype/devalue"
)

// goldenCase is one recorded value: the bytes the pinned JavaScript devalue
// produced for a generated value expression. See testdata/record/README.md.
type goldenCase struct {
	Name    string `json:"name"`
	Devalue string `json:"devalue"`
}

func goldenCases(t testing.TB) []goldenCase {
	t.Helper()
	contents, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(contents, &cases); err != nil {
		t.Fatalf("testdata/golden.json: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("testdata/golden.json holds no cases")
	}
	return cases
}

// TestGoldenRoundTrip is the conformance proof against the real JavaScript
// implementation: every document it emits parses here, and re-serializing what
// we parsed reproduces its bytes exactly.
func TestGoldenRoundTrip(t *testing.T) {
	t.Parallel()

	for _, c := range goldenCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			value, err := devalue.Parse(c.Devalue, nil)
			if err != nil {
				t.Fatalf("parse %s: %v", c.Devalue, err)
			}
			out, err := devalue.Stringify(value)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if out != c.Devalue {
				t.Fatalf("round trip differs\n want: %s\n  got: %s", c.Devalue, out)
			}
		})
	}
}

// FuzzRoundTrip seeds the mutator with the recorded documents. A mutated
// document is usually not valid, and rejecting it is a pass; what must never
// happen is a panic, or a value that serializes into something we can no longer
// read back. Byte equality is only required of the recorded documents (the
// mutator readily produces non-canonical spellings such as 1.0 for 1), so the
// property here is that serialization reaches a fixed point in one step.
func FuzzRoundTrip(f *testing.F) {
	for _, c := range goldenCases(f) {
		f.Add(c.Devalue)
	}

	f.Fuzz(func(t *testing.T, document string) {
		value, err := devalue.Parse(document, nil)
		if err != nil {
			return
		}
		out, err := devalue.Stringify(value)
		if err != nil {
			t.Fatalf("parsed document did not serialize: %v", err)
		}
		reparsed, err := devalue.Parse(out, nil)
		if err != nil {
			t.Fatalf("own output did not parse: %v\n%s", err, out)
		}
		again, err := devalue.Stringify(reparsed)
		if err != nil {
			t.Fatalf("stringify: %v", err)
		}
		if again != out {
			t.Fatalf("serialization is not a fixed point\n first: %s\nsecond: %s", out, again)
		}
	})
}
