package devalue

import (
	"sort"
	"unicode/utf8"
)

// CompareUTF16 compares two strings the way JavaScript's relational operators
// and `Array#sort` do: lexicographically by UTF-16 code unit. It returns -1, 0
// or +1.
//
// This is not the same as Go's `<`, which compares UTF-8 bytes. The two orders
// agree everywhere except when an astral character (U+10000 and above, encoded
// in UTF-16 as a surrogate pair D800–DFFF) is compared against a character in
// U+E000–U+FFFF: JavaScript puts the astral character first, Go puts it last.
// Any sort whose result reaches the wire — a remote function's payload, which
// the client also computes — has to use this one.
func CompareUTF16(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)

		if ra != rb {
			// Equal lead units can only mean two astral runes sharing a high
			// surrogate, and then the low surrogates order like the runes.
			if la, lb := leadUnit(ra), leadUnit(rb); la != lb {
				return sign(int32(la) - int32(lb))
			}
			return sign(int32(ra) - int32(rb))
		}

		a, b = a[na:], b[nb:]
	}

	switch {
	case len(a) > 0:
		return 1
	case len(b) > 0:
		return -1
	}
	return 0
}

// leadUnit returns the first UTF-16 code unit of r: r itself in the BMP, and
// the high surrogate above it.
func leadUnit(r rune) uint16 {
	if r < 0x10000 {
		return uint16(r)
	}
	return uint16(0xD800 + ((r - 0x10000) >> 10))
}

func sign(d int32) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}

// SortStringsUTF16 sorts a slice the way JavaScript's `Array#sort` sorts an
// array of strings.
func SortStringsUTF16(s []string) {
	sort.Slice(s, func(i, j int) bool { return CompareUTF16(s[i], s[j]) < 0 })
}
