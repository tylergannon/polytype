package noarg

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestTreeRoundTrip proves the generated codecs round-trip a Tree three
// levels deep through both of its recursive paths: the sealed union Node
// through Branch.Body, and the self-recursive Watcher.
func TestTreeRoundTrip(t *testing.T) {
	want := Tree{
		Body: []Node{
			Leaf{Name: "first"},
			Branch{Name: "outer", Body: []Node{
				Branch{Name: "inner", Body: []Node{Leaf{Name: "deepest"}}},
			}},
		},
		Watchers: []Watcher{{Name: "outer", Watchers: []Watcher{
			{Name: "inner", Watchers: []Watcher{{Name: "deepest", Watchers: []Watcher{}}}},
		}}},
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{`"kind":"leaf"`, `"kind":"branch"`} {
		if !strings.Contains(string(data), variant) {
			t.Fatalf("wire form lacks %s: %s", variant, data)
		}
	}
	var got Tree
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the value:\n got %#v\nwant %#v", got, want)
	}
}

// TestMissingDiscriminatorNamesConfiguredProperty proves the decoder reports
// the discriminator property the declaration configured.
func TestMissingDiscriminatorNamesConfiguredProperty(t *testing.T) {
	var got Tree
	err := json.Unmarshal([]byte(`{"body":[{"name":"orphan"}],"watchers":[]}`), &got)
	if err == nil || !strings.Contains(err.Error(), "no discriminator property 'kind' found") {
		t.Fatalf("got %v, want the missing 'kind' discriminator", err)
	}
}
