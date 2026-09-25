package devalue_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/tylergannon/polytype/devalue"
)

// golden is testdata/golden.json: the devalue release that recorded it and
// what that release produced for each generated value expression. See
// testdata/record/README.md.
type golden struct {
	Devalue string       `json:"devalue"`
	Cases   []goldenCase `json:"cases"`
}

// goldenCase is one recorded value: the bytes the pinned JavaScript devalue
// produced for a generated value expression.
type goldenCase struct {
	Name    string `json:"name"`
	Devalue string `json:"devalue"`
	Uneval  string `json:"uneval"`
}

// TestUpstreamVersion holds the parity promise together: the goldens were
// recorded by the devalue release UpstreamVersion names, and that release is
// the exact version package.json pins for recording them.
func TestUpstreamVersion(t *testing.T) {
	t.Parallel()

	if recorded := readGolden(t).Devalue; recorded != devalue.UpstreamVersion {
		t.Errorf("testdata/golden.json was recorded from devalue %q, but UpstreamVersion is %q; re-record it (testdata/record/README.md)", recorded, devalue.UpstreamVersion)
	}

	contents, err := os.ReadFile("../package.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(contents, &pkg); err != nil {
		t.Fatalf("package.json: %v", err)
	}
	if pinned := pkg.DevDependencies["devalue"]; pinned != devalue.UpstreamVersion {
		t.Errorf("package.json pins devalue %q, but UpstreamVersion is %q; the pin must be that exact version", pinned, devalue.UpstreamVersion)
	}
}

// TestUnevalGolden compares Polytype's expression serializer with the pinned
// devalue implementation for every value shared with the flat format.
func TestUnevalGolden(t *testing.T) {
	t.Parallel()

	for _, c := range goldenCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			value, err := devalue.Parse(c.Devalue, nil)
			if err != nil {
				t.Fatalf("parse %s: %v", c.Devalue, err)
			}
			out, err := devalue.Uneval(value)
			if err != nil {
				t.Fatalf("uneval: %v", err)
			}
			if out != c.Uneval {
				t.Fatalf("uneval differs\n want: %s\n  got: %s", c.Uneval, out)
			}
		})
	}
}

func readGolden(t testing.TB) golden {
	t.Helper()
	contents, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(contents, &g); err != nil {
		t.Fatalf("testdata/golden.json: %v", err)
	}
	return g
}

func goldenCases(t testing.TB) []goldenCase {
	t.Helper()
	cases := readGolden(t).Cases
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
