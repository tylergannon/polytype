package devalue

import (
	"sort"
	"testing"
	"unicode/utf16"
)

// referenceCompare is the definition CompareUTF16 implements: encode both
// strings to UTF-16 and compare the code-unit sequences.
func referenceCompare(a, b string) int {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ua) < len(ub):
		return -1
	case len(ua) > len(ub):
		return 1
	}
	return 0
}

func TestCompareUTF16(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "a", -1},
		{"a", "", 1},
		{"a", "a", 0},
		{"a", "b", -1},
		{"ab", "a", 1},
		{"A", "a", -1},
		{"z", "é", -1},

		// The whole point: an astral character sorts before U+E000–U+FFFF in
		// UTF-16 (its lead surrogate is D83D) but after them in UTF-8 bytes
		// (its lead byte is F0). Go's `<` gets these backwards.
		{"\U0001F600", "\uE000", -1},
		{"\uE000", "\U0001F600", 1},
		{"\U0001F600", "\uFFFF", -1},
		{"a\U0001F600", "a\uE000", -1},

		// Below U+E000 the two orders agree.
		{"z", "\U0001F600", -1},

		// Astral against astral orders by code point either way, including
		// when the lead surrogate is shared.
		{"\U00010000", "\U0001F600", -1},
		{"\U0001F600", "\U0001F601", -1},
		{"\U0001F601", "\U0001F600", 1},
		{"\U0010FFFF", "\U0001F600", 1},
	}

	for _, c := range cases {
		if got := CompareUTF16(c.a, c.b); got != c.want {
			t.Errorf("CompareUTF16(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := referenceCompare(c.a, c.b); got != c.want {
			t.Errorf("reference disagrees for (%q, %q): %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCompareUTF16MatchesUTF16Encoding(t *testing.T) {
	// Runes chosen around every boundary where UTF-8 byte order and UTF-16
	// code-unit order can part company.
	runes := []rune{
		0, 'A', 'a', 'z', '~', 0x7F, 0x80, 0x7FF, 0x800,
		0xD7FF, 0xE000, 0xE001, 0xF8FF, 0xFFFD, 0xFFFF,
		0x10000, 0x10001, 0x1F600, 0x1F601, 0x2FFFF, 0x10FFFF,
	}

	var strs []string
	for _, r := range runes {
		strs = append(strs, string(r), "a"+string(r), string(r)+"a")
	}

	for _, a := range strs {
		for _, b := range strs {
			if got, want := CompareUTF16(a, b), referenceCompare(a, b); got != want {
				t.Fatalf("CompareUTF16(%q, %q) = %d, want %d", a, b, got, want)
			}
		}
	}
}

func TestSortStringsUTF16DiffersFromByteOrder(t *testing.T) {
	input := []string{"\uE000", "\U0001F600", "z"}

	utf16Sorted := append([]string(nil), input...)
	SortStringsUTF16(utf16Sorted)

	byteSorted := append([]string(nil), input...)
	sort.Strings(byteSorted)

	if want := []string{"z", "\U0001F600", "\uE000"}; !equalStrings(utf16Sorted, want) {
		t.Errorf("SortStringsUTF16 = %q, want %q", utf16Sorted, want)
	}
	if equalStrings(utf16Sorted, byteSorted) {
		t.Error("byte order and UTF-16 order agree; the test case no longer discriminates")
	}
}

func TestStringifyGoMapSortsByCodeUnit(t *testing.T) {
	// A Go map is serialized with its keys sorted; the order has to be the one
	// JavaScript would produce, so that a map and the equivalent *Object give
	// the same bytes.
	m := map[string]any{"\uE000": 1, "\U0001F600": 2, "z": 3}

	got, err := Stringify(m)
	if err != nil {
		t.Fatal(err)
	}

	want := "[{\"z\":1,\"\U0001F600\":2,\"\uE000\":3},3,2,1]"
	if got != want {
		t.Errorf("Stringify = %s, want %s", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
