package recursivedeclarations_test

import (
	"reflect"
	"testing"

	"recursivedeclarations/noarg"
)

// TestTreeDevalueRoundTrip proves the devalue codec that gen writes into noarg
// (OUT=devalue) round-trips a Tree three levels deep through both of its
// recursive paths. It lives outside noarg because that package is also built
// from CLI output, which has no devalue codec.
func TestTreeDevalueRoundTrip(t *testing.T) {
	want := noarg.Tree{
		Body: []noarg.Node{
			noarg.Leaf{Name: "first"},
			noarg.Branch{Name: "outer", Body: []noarg.Node{
				noarg.Branch{Name: "inner", Body: []noarg.Node{noarg.Leaf{Name: "deepest"}}},
			}},
		},
		Watchers: []noarg.Watcher{{Name: "outer", Watchers: []noarg.Watcher{
			{Name: "inner", Watchers: []noarg.Watcher{{Name: "deepest", Watchers: []noarg.Watcher{}}}},
		}}},
	}
	data, err := noarg.StringifyTree(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := noarg.ParseTree(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the value:\n got %#v\nwant %#v", got, want)
	}
}
