# Research: #105 (facts for the implementer)

## skgo internal/devalue API (copy source: ~/src/skgo/internal/devalue)
Exported: Stringify(v any) (string, error); StringifyWith(v, []Reducer);
Parse(s string, revivers map[string]func(any)(any,error)) (any, error);
Reducer{Key; Fn func(any)(any,bool,error)}; Undefined/UndefinedValue;
Hole/HoleValue; Object (NewObject, NewNullProtoObject, Set, Get, Keys, Len,
MarshalJSON); Map/MapEntry; Set; Date (Time()); BigInt; RegExp; ArrayBuffer;
Boxed; CompareUTF16; SortStringsUTF16. Stdlib imports only. helper_test.go
is 4 lines.

Parse output kinds: null→nil, undefined→Undefined, bool→bool, every
number→float64, string→string, array→[]any (holes = Hole), object→*Object,
plus *Map, *Set, Date, BigInt, RegExp, ArrayBuffer, *Boxed.

Stringify numerics: asFloat (internal.go:93-121) accepts float32/64, int*,
uint, uint16/32/64 but NOT uint8/byte (falls to "Cannot stringify arbitrary
non-POJOs"). Number formatting is ECMAScript Number::toString
(formatNumber internal.go:170-222).

Dedupe: Stringify dedupes by identityKey (internal.go:46-91): primitives by
value, *Object/*Map/*Set/*Boxed by pointer, []any by data pointer+len.

## Codegen patterns to copy
- internal/typescript/generate.go: Validate first (:33), allocate
  collision-free names (allocateNames :204), then two type switches,
  project(t Type, at string) :67 over the 8 node kinds and
  projectField(FieldValue, at) :140 over the 6 field kinds, threading `at`
  as the diagnostic path. Copy this shape.
- internal/builder: text/template (schemas.go.tmpl) via RenderTemplate
  (printer.go:11), formatted by FormatCodeWithGoimports (printer.go:26-32,
  imports.Process). Use the same: goimports resolves the emitted imports.

## Consumer-compile harness
No single helper. internal/builder/basic_test.go:31-96 copies a fixture with
testutils.CopyDir into test_run/<name>, then RunCommand for go mod tidy,
go generate, go build, go test. The replace directive is committed in each
fixture go.mod (e.g. testfixtures/basictypes/go.mod:15
`replace github.com/tylergannon/polytype => ../../../../`).
internal/compiletest/negative_test.go:29 shows `exec.Command("go","build",
"./testdata/<name>/")` for in-module fixtures.

## Closest fixture
internal/builder/testfixtures/union_codec (Envelope, types.go:148-161) has
Union, UnionSlice, OptionalUnion, Object, Ref, Pointer variant, string
scalar, int enum, codec_test.go, go.mod replace. Lacks: time.Time, Array
(no [N]T fixture exists anywhere), Nullable, non-union Optional, plain
Slice of scalars, plain Pointer field, scalar breadth (bool, floats, sized
ints, uints). Complements: traversal (time.Time, cross-package ref),
optionality (Optional/Nullable incl Optional[*Detail], Optional[[]int]),
indirecttypes, enums.
