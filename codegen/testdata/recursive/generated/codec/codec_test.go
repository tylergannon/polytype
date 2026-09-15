package codec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"recursivefixture/generated/codec"
	"recursivefixture/model"

	"github.com/tylergannon/polytype"
	devalue "github.com/tylergannon/polytype/devalue"
)

// --- devalue round-trips ---

func TestTreeRoundTrip(t *testing.T) {
	tree := model.Tree{
		Name: "root",
		Children: []model.Tree{
			{Name: "child1", Children: []model.Tree{{Name: "grandchild"}}},
			{Name: "child2"},
		},
	}

	s, err := codec.StringifyTree(tree)
	if err != nil {
		t.Fatalf("StringifyTree: %v", err)
	}
	got, err := codec.ParseTree(s)
	if err != nil {
		t.Fatalf("ParseTree: %v", err)
	}
	if got.Name != tree.Name {
		t.Fatalf("Name: got %q, want %q", got.Name, tree.Name)
	}
	if len(got.Children) != 2 {
		t.Fatalf("Children: got %d, want 2", len(got.Children))
	}
	if got.Children[0].Children[0].Name != "grandchild" {
		t.Fatalf("grandchild: got %q", got.Children[0].Children[0].Name)
	}
}

func TestTreeNullableParentRoundTrip(t *testing.T) {
	parent := model.Tree{Name: "parent"}
	tree := model.Tree{
		Name:   "child",
		Parent: polytype.Nullable[*model.Tree]{Present: true, Value: &parent},
	}

	s, err := codec.StringifyTree(tree)
	if err != nil {
		t.Fatalf("StringifyTree: %v", err)
	}
	got, err := codec.ParseTree(s)
	if err != nil {
		t.Fatalf("ParseTree: %v", err)
	}
	if !got.Parent.Present || got.Parent.Value == nil || got.Parent.Value.Name != "parent" {
		t.Fatalf("Parent: got %+v", got.Parent)
	}
}

func TestTreeNullParent(t *testing.T) {
	tree := model.Tree{Name: "orphan"}

	s, err := codec.StringifyTree(tree)
	if err != nil {
		t.Fatalf("StringifyTree: %v", err)
	}
	got, err := codec.ParseTree(s)
	if err != nil {
		t.Fatalf("ParseTree: %v", err)
	}
	if got.Parent.Present {
		t.Fatalf("expected null parent, got %+v", got.Parent)
	}
}

func TestTreeOptionalMetadataRoundTrip(t *testing.T) {
	meta := model.Tree{Name: "meta-node"}
	tree := model.Tree{
		Name:     "root",
		Children: []model.Tree{},
		Metadata: polytype.Optional[*model.Tree]{Present: true, Value: &meta},
	}

	s, err := codec.StringifyTree(tree)
	if err != nil {
		t.Fatalf("StringifyTree: %v", err)
	}
	got, err := codec.ParseTree(s)
	if err != nil {
		t.Fatalf("ParseTree: %v", err)
	}
	if !got.Metadata.Present || got.Metadata.Value == nil {
		t.Fatalf("Metadata not present")
	}
	if got.Metadata.Value.Name != "meta-node" {
		t.Fatalf("Metadata.Name: got %q, want %q", got.Metadata.Value.Name, "meta-node")
	}
}

func TestTreeAbsentOptionalMetadata(t *testing.T) {
	tree := model.Tree{Name: "bare"}

	s, err := codec.StringifyTree(tree)
	if err != nil {
		t.Fatalf("StringifyTree: %v", err)
	}
	got, err := codec.ParseTree(s)
	if err != nil {
		t.Fatalf("ParseTree: %v", err)
	}
	if got.Metadata.Present {
		t.Fatalf("expected absent metadata, got present")
	}
}

func TestDocumentRecursiveUnionRoundTrip(t *testing.T) {
	doc := model.Document{
		Title: "test",
		Content: []model.Block{
			model.Paragraph{Text: "intro"},
			model.Section{
				Heading: "chapter",
				Children: []model.Block{
					model.Paragraph{Text: "body"},
					model.Section{
						Heading:  "subsection",
						Children: []model.Block{model.Paragraph{Text: "detail"}},
					},
				},
			},
		},
	}

	s, err := codec.StringifyDocument(doc)
	if err != nil {
		t.Fatalf("StringifyDocument: %v", err)
	}
	got, err := codec.ParseDocument(s)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if got.Title != doc.Title {
		t.Fatalf("Title: got %q, want %q", got.Title, doc.Title)
	}
	if len(got.Content) != 2 {
		t.Fatalf("Content: got %d, want 2", len(got.Content))
	}
	section, ok := got.Content[1].(model.Section)
	if !ok {
		t.Fatalf("Content[1]: got %T, want Section", got.Content[1])
	}
	if section.Heading != "chapter" || len(section.Children) != 2 {
		t.Fatalf("Section: heading=%q children=%d", section.Heading, len(section.Children))
	}
	sub, ok := section.Children[1].(model.Section)
	if !ok {
		t.Fatalf("nested: got %T, want Section", section.Children[1])
	}
	if sub.Heading != "subsection" || len(sub.Children) != 1 {
		t.Fatalf("subsection: heading=%q children=%d", sub.Heading, len(sub.Children))
	}
}

func TestMutualRecursionRoundTrip(t *testing.T) {
	innerA := model.MutualA{Value: 99}
	b := model.MutualB{
		Label: "hello",
		Peer:  polytype.Nullable[*model.MutualA]{Present: true, Value: &innerA},
	}
	a := model.MutualA{
		Value: 42,
		Peer:  polytype.Nullable[*model.MutualB]{Present: true, Value: &b},
	}

	s, err := codec.StringifyMutualA(a)
	if err != nil {
		t.Fatalf("StringifyMutualA: %v", err)
	}
	got, err := codec.ParseMutualA(s)
	if err != nil {
		t.Fatalf("ParseMutualA: %v", err)
	}
	if got.Value != 42 || !got.Peer.Present {
		t.Fatalf("MutualA: got %+v", got)
	}
	if got.Peer.Value.Label != "hello" || !got.Peer.Value.Peer.Present {
		t.Fatalf("MutualB: got %+v", got.Peer.Value)
	}
	if got.Peer.Value.Peer.Value.Value != 99 {
		t.Fatalf("nested MutualA: got %d", got.Peer.Value.Peer.Value.Value)
	}
}

// --- JSON round-trips ---

func TestTreeJSONRoundTrip(t *testing.T) {
	tree := model.Tree{
		Name: "root",
		Children: []model.Tree{
			{Name: "child", Children: []model.Tree{{Name: "grandchild"}}},
		},
	}

	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got model.Tree
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Name != tree.Name || len(got.Children) != 1 || got.Children[0].Children[0].Name != "grandchild" {
		t.Fatalf("JSON round-trip mismatch: %+v", got)
	}
}

func TestTreeOptionalMetadataJSONRoundTrip(t *testing.T) {
	meta := model.Tree{Name: "meta-node", Children: []model.Tree{}}
	tree := model.Tree{
		Name:     "root",
		Children: []model.Tree{},
		Metadata: polytype.Optional[*model.Tree]{Present: true, Value: &meta},
	}

	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"metadata"`) {
		t.Fatalf("expected metadata in JSON, got %s", data)
	}
	var got model.Tree
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.Metadata.Present || got.Metadata.Value == nil || got.Metadata.Value.Name != "meta-node" {
		t.Fatalf("Metadata JSON round-trip: got %+v", got.Metadata)
	}
}

func TestTreeAbsentOptionalMetadataJSON(t *testing.T) {
	tree := model.Tree{Name: "bare", Children: []model.Tree{}}

	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), `"metadata"`) {
		t.Fatalf("absent optional should omit key, got %s", data)
	}
	var got model.Tree
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Metadata.Present {
		t.Fatalf("expected absent metadata after JSON round-trip")
	}
}

func TestDocumentJSONRoundTrip(t *testing.T) {
	doc := model.Document{
		Title: "test",
		Content: []model.Block{
			model.Paragraph{Text: "intro"},
			model.Section{
				Heading: "chapter",
				Children: []model.Block{
					model.Paragraph{Text: "body"},
					model.Section{
						Heading:  "subsection",
						Children: []model.Block{model.Paragraph{Text: "detail"}},
					},
				},
			},
		},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got model.Document
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Title != "test" || len(got.Content) != 2 {
		t.Fatalf("Title/Content length: %q %d", got.Title, len(got.Content))
	}
	section, ok := got.Content[1].(model.Section)
	if !ok {
		t.Fatalf("Content[1]: got %T, want Section", got.Content[1])
	}
	if section.Heading != "chapter" || len(section.Children) != 2 {
		t.Fatalf("Section: heading=%q children=%d", section.Heading, len(section.Children))
	}
	sub, ok := section.Children[1].(model.Section)
	if !ok {
		t.Fatalf("nested: got %T, want Section", section.Children[1])
	}
	if sub.Heading != "subsection" || len(sub.Children) != 1 {
		t.Fatalf("subsection: heading=%q children=%d", sub.Heading, len(sub.Children))
	}
	para, ok := sub.Children[0].(model.Paragraph)
	if !ok {
		t.Fatalf("leaf: got %T, want Paragraph", sub.Children[0])
	}
	if para.Text != "detail" {
		t.Fatalf("leaf text: got %q", para.Text)
	}
}

func TestMutualJSONRoundTrip(t *testing.T) {
	innerA := model.MutualA{Value: 99}
	b := model.MutualB{
		Label: "hello",
		Peer:  polytype.Nullable[*model.MutualA]{Present: true, Value: &innerA},
	}
	a := model.MutualA{
		Value: 42,
		Peer:  polytype.Nullable[*model.MutualB]{Present: true, Value: &b},
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got model.MutualA
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Value != 42 || !got.Peer.Present || got.Peer.Value == nil {
		t.Fatalf("MutualA: got %+v", got)
	}
	if got.Peer.Value.Label != "hello" || !got.Peer.Value.Peer.Present {
		t.Fatalf("MutualB: got %+v", got.Peer.Value)
	}
	if got.Peer.Value.Peer.Value.Value != 99 {
		t.Fatalf("nested MutualA: got %d", got.Peer.Value.Peer.Value.Value)
	}
}

// --- nested malformed-value path diagnostics ---

func TestDevalueNestedMalformedUnionPath(t *testing.T) {
	badBlock := devalue.NewObject()
	badBlock.Set("type", float64(42))
	badBlock.Set("text", "oops")
	doc := devalue.NewObject()
	doc.Set("title", "test")
	doc.Set("content", []any{badBlock})
	s, err := devalue.Stringify(doc)
	if err != nil {
		t.Fatalf("Stringify: %v", err)
	}
	_, err = codec.ParseDocument(s)
	if err == nil {
		t.Fatal("expected error for malformed nested discriminator")
	}
	msg := err.Error()
	if !strings.Contains(msg, "/content/0") {
		t.Fatalf("expected path /content/0 in error, got: %s", msg)
	}
}

func TestJSONNestedMalformedUnionPath(t *testing.T) {
	bad := `{"title":"test","content":[{"type":99,"text":"oops"}]}`
	var doc model.Document
	err := json.Unmarshal([]byte(bad), &doc)
	if err == nil {
		t.Fatal("expected error for non-string discriminator")
	}
	msg := err.Error()
	if !strings.Contains(msg, "content") {
		t.Fatalf("expected field context in error, got: %s", msg)
	}
}

// --- unchanged receiver on decode failure ---

func TestJSONUnchangedReceiverOnDecodeFailure(t *testing.T) {
	original := model.Document{
		Title:   "original",
		Content: []model.Block{model.Paragraph{Text: "keep"}},
	}
	doc := original
	err := json.Unmarshal([]byte(`not-json`), &doc)
	if err == nil {
		t.Fatal("expected error")
	}
	if doc.Title != "original" || len(doc.Content) != 1 {
		t.Fatalf("receiver was modified on failed decode: %+v", doc)
	}
}

// --- transport-specific nil/empty and absence/null ---

func TestJSONNilUnionSliceRejects(t *testing.T) {
	doc := model.Document{Title: "t", Content: nil}
	_, err := json.Marshal(doc)
	if err == nil {
		t.Fatal("expected error marshaling nil union slice")
	}
	if !strings.Contains(err.Error(), "nil registered interface slice") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJSONEmptyUnionSlice(t *testing.T) {
	doc := model.Document{Title: "t", Content: []model.Block{}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"content":[]`) {
		t.Fatalf("empty slice should serialize as [], got %s", data)
	}
}

func TestDevalueNilUnionSliceNormalized(t *testing.T) {
	doc := model.Document{Title: "t", Content: []model.Block{}}
	s, err := codec.StringifyDocument(doc)
	if err != nil {
		t.Fatalf("StringifyDocument: %v", err)
	}
	got, err := codec.ParseDocument(s)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if got.Content == nil {
		t.Fatal("devalue should decode empty union slice as non-nil")
	}
	if len(got.Content) != 0 {
		t.Fatalf("expected empty slice, got %d elements", len(got.Content))
	}
}

func TestDevalueNullableAbsentRequired(t *testing.T) {
	bad := `[{"name":1,"children":2},"leaf",[]]`
	_, err := codec.ParseTree(bad)
	if err == nil {
		t.Fatal("expected error for absent required nullable parent")
	}
	if !strings.Contains(err.Error(), "parent") {
		t.Fatalf("expected parent in error, got: %v", err)
	}
}

func TestDevalueNullableExplicitNull(t *testing.T) {
	tree := model.Tree{
		Name:     "leaf",
		Children: []model.Tree{},
	}
	s, err := codec.StringifyTree(tree)
	if err != nil {
		t.Fatalf("StringifyTree: %v", err)
	}
	got, err := codec.ParseTree(s)
	if err != nil {
		t.Fatalf("ParseTree: %v", err)
	}
	if got.Parent.Present {
		t.Fatal("explicit null parent should not be present")
	}
}
