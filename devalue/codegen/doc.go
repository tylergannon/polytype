// Package codegen emits Go encoders and strict decoders that move values
// between Go types and the devalue value model of
// github.com/tylergannon/polytype/devalue.
//
// The input is a validated [github.com/tylergannon/polytype/typegrammar]
// definition graph plus the caller's root nodes, as produced by
// github.com/tylergannon/polytype/grammar. The output is one Go source file
// holding, for every definition and every root, an encoder, a strict decoder
// and a Stringify/Parse convenience wrapper. The file is written for a package
// other than the one declaring the Go types, so the emitted functions use only
// exported fields.
//
// # Wire mapping
//
// Every Go numeric kind is a JavaScript number: the encoder converts to
// float64 itself, so integers beyond 2^53 lose precision on the wire. There is
// no BigInt. time.Time is the same string encoding/json produces, never a
// JavaScript Date. A required nil slice encodes as an empty array; a nil
// pointer where a value is required is an encode error. An absent Optional is
// no property at all; an absent Nullable is null. Unions encode as one object
// whose discriminator property holds the concrete type name.
//
// Decoders are strict. They reject a missing required property, an unknown
// property, a value of the wrong kind, undefined, null where the field is not
// Nullable, an array whose length does not match a Go array's, an enum
// non-member, an unknown union tag, and every devalue tagged form (Date, Map,
// Set, BigInt, RegExp, ArrayBuffer, boxed primitives), since the grammar
// admits none of them. Every diagnostic names a JSON-pointer-style path.
//
// Encoders never dedupe: a fresh object and slice is built per value. Reducers
// and revivers are a caller concern; pass them to devalue.StringifyWith and
// devalue.Parse together with the generated encoder or decoder.
//
// # Limitations
//
// An anonymous struct type is only supported as a definition's own type. An
// inline struct used as a field or as a root is refused, because the emitted
// package cannot spell that Go type with confidence.
package codegen
