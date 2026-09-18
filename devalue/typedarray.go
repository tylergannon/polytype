package devalue

import (
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"
)

// typedArrayElements renders the whole of buf, read as kind, as the
// comma-separated element list `new <kind>([...])` takes.
//
// It reads the buffer rather than the view because that is what devalue does:
// the emitted constructor always rebuilds the entire buffer, and a view that
// covers only part of it gets a trailing `.subarray(...)`.
func typedArrayElements(kind TypedArrayKind, buf ArrayBuffer) (string, error) {
	per := kind.BytesPerElement()
	if per == 0 {
		return "", errors.New("Cannot stringify arbitrary non-POJOs") //nolint:staticcheck // ST1005: devalue's message is part of the compatibility surface.
	}
	if len(buf)%per != 0 {
		return "", errors.New("byte length of " + string(kind) + " should be a multiple of " + strconv.Itoa(per))
	}

	n := len(buf) / per
	parts := make([]string, n)
	for i := range n {
		b := buf[i*per:]
		switch kind {
		case Int8Array:
			parts[i] = strconv.Itoa(int(int8(b[0])))
		case Uint8Array, Uint8ClampedArray:
			parts[i] = strconv.Itoa(int(b[0]))
		case Int16Array:
			parts[i] = strconv.Itoa(int(int16(binary.LittleEndian.Uint16(b))))
		case Uint16Array:
			parts[i] = strconv.Itoa(int(binary.LittleEndian.Uint16(b)))
		case Int32Array:
			parts[i] = strconv.Itoa(int(int32(binary.LittleEndian.Uint32(b))))
		case Uint32Array:
			parts[i] = strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b)), 10)
		case Float32Array:
			parts[i] = formatFloatElement(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))))
		case Float64Array:
			parts[i] = formatFloatElement(math.Float64frombits(binary.LittleEndian.Uint64(b)))
		case BigInt64Array:
			// bigint elements need the `n` suffix, or the emitted
			// `new BigInt64Array([...])` throws.
			parts[i] = strconv.FormatInt(int64(binary.LittleEndian.Uint64(b)), 10) + "n"
		case BigUint64Array:
			parts[i] = strconv.FormatUint(binary.LittleEndian.Uint64(b), 10) + "n"
		}
	}
	return strings.Join(parts, ","), nil
}

// formatFloatElement renders one float element. `toString()` collapses -0 to
// "0", silently losing the sign on round-trip, so devalue writes it out.
func formatFloatElement(f float64) string {
	if f == 0 && math.Signbit(f) {
		return "-0"
	}
	return formatNumber(f)
}
