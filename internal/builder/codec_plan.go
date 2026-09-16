package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tylergannon/polytype/typegrammar"
)

// OwnerCodec is the single composition point for all generated field codecs
// on a containing struct: one MarshalJSON/UnmarshalJSON pair per owner, which
// adapts every union field and every name-mode enum field it declares.
type OwnerCodec struct {
	Name        string
	UnionFields []InterfaceProp
	EnumFields  []EnumFieldPlan
	Initial     string
}

// InterfaceProp is one registered interface field of an owner codec, as the
// template renders it.
type InterfaceProp struct {
	goName   string
	jsonName string
	tag      string
	// path is the embedded field chain the field was promoted through.
	path   []string
	source fieldSource

	Union typegrammar.Union
	// DiscPropName is the discriminator as the interface declared it: empty
	// when the package default applies. The generated helper is keyed on it.
	DiscPropName                string
	interfacePkgName            string
	InterfaceTypeNameWithPrefix string
	Optional                    bool
	Repeated                    bool
}

func (i InterfaceProp) FieldNames() string { return i.goName }
func (i InterfaceProp) StructTag() string  { return i.tag }
func (i InterfaceProp) JSONName() string   { return i.jsonName }

// Accessor is the field's selector from receiver, through any embedded
// structs it was promoted from.
func (i InterfaceProp) Accessor(receiver string) string {
	parts := append([]string{receiver}, i.path...)
	return strings.Join(append(parts, i.goName), ".")
}

func (i InterfaceProp) UnmarshalerFunc() string {
	identityHash := sha256.Sum256([]byte(i.helperIdentity()))
	return fmt.Sprintf(
		"__jsonUnmarshal__%s__%s__%s",
		i.interfacePkgName,
		i.Union.Interface.Name,
		hex.EncodeToString(identityHash[:]),
	)
}

func (i InterfaceProp) MarshalerFunc() string {
	return strings.Replace(i.UnmarshalerFunc(), "__jsonUnmarshal__", "__jsonMarshal__", 1)
}

// helperIdentity names the exact resolved interface registration consumed by a
// generated helper. Length-prefixed parts keep distinct package paths and
// configurations unambiguous even when package and type names match.
func (i InterfaceProp) helperIdentity() string {
	var identity strings.Builder
	writePart := func(value string) {
		fmt.Fprintf(&identity, "%d:%s;", len(value), value)
	}
	writePart(i.Union.Interface.PackagePath)
	writePart(i.Union.Interface.Name)
	writePart(i.DiscPropName)
	for _, variant := range i.Union.Variants {
		indirection := 0
		if variant.Pointer {
			indirection = 1
		}
		writePart(variant.Implementation.PackagePath)
		writePart(variant.Implementation.Name)
		writePart(strconv.Itoa(indirection))
	}
	return identity.String()
}

// EnumFieldPlan is one name-mode enum field of an owner codec: an integer
// enum whose constants' names are the wire values.
type EnumFieldPlan struct {
	goName   string
	jsonName string
	tag      string
	optional bool
	nullable bool

	EnumType               typegrammar.Name
	EnumTypeName           string
	EnumTypeNameWithPrefix string
	Entries                []EnumEntry
	MarshalerFunc          string
	UnmarshalerFunc        string
}

type EnumEntry struct {
	ConstName   string
	GoValueExpr string
	WireName    string
}

func (e EnumFieldPlan) FieldNames() string { return e.goName }
func (e EnumFieldPlan) StructTag() string  { return e.tag }
func (e EnumFieldPlan) JSONName() string   { return e.jsonName }
func (e EnumFieldPlan) Optional() bool     { return e.optional }
func (e EnumFieldPlan) Nullable() bool     { return e.nullable }

// planOwnerCodecs derives the owner codecs from the lowered definitions. An
// owner is a named object of the generated package that a root reaches over
// reference or union-variant edges and that declares a union field or a
// name-mode enum field. Embedded structs are flattened into their owners,
// so they need no codec of their own.
func planOwnerCodecs(s *SchemaBuilder, lowered *lowering) (map[string]OwnerCodec, error) {
	index := make(map[typegrammar.Name]typegrammar.Definition, len(lowered.defs))
	for _, def := range lowered.defs {
		index[def.Name] = def
	}
	reachable := make(map[typegrammar.Name]bool)
	var visitType func(t typegrammar.Type)
	var visitUnion func(u typegrammar.Union)
	visitDef := func(name typegrammar.Name) {
		if reachable[name] {
			return
		}
		reachable[name] = true
		visitType(index[name].Type)
	}
	visitUnion = func(u typegrammar.Union) {
		for _, variant := range u.Variants {
			visitDef(variant.Implementation)
		}
	}
	visitType = func(t typegrammar.Type) {
		switch n := t.(type) {
		case *typegrammar.Object:
			for _, field := range n.Fields {
				switch v := field.Value.(type) {
				case *typegrammar.Required:
					visitType(v.Type)
				case *typegrammar.Optional:
					visitType(v.Type)
				case *typegrammar.Nullable:
					visitType(v.Type)
				case *typegrammar.Union:
					visitUnion(*v)
				case *typegrammar.OptionalUnion:
					visitUnion(v.Union)
				case *typegrammar.UnionSlice:
					visitUnion(v.Union)
				}
			}
		case *typegrammar.Pointer:
			visitType(n.Element)
		case *typegrammar.Slice:
			visitType(n.Element)
		case *typegrammar.Array:
			visitType(n.Element)
		case *typegrammar.Ref:
			visitDef(n.Target)
		}
	}
	for _, root := range lowered.roots {
		if root.schema.Union != nil {
			visitUnion(*root.schema.Union)
		} else {
			visitDef(root.schema.Name)
		}
	}

	owners := make(map[string]OwnerCodec)
	for _, def := range lowered.defs {
		if !reachable[def.Name] || def.Name.PackagePath != s.Scan.Pkg.PkgPath {
			continue
		}
		object, ok := def.Type.(*typegrammar.Object)
		if !ok {
			continue
		}
		sources := lowered.fields[def.Name]
		var (
			unions []InterfaceProp
			enums  []EnumFieldPlan
		)
		for _, field := range object.Fields {
			source := sources[field.GoName]
			switch v := field.Value.(type) {
			case *typegrammar.Union:
				unions = append(unions, s.interfaceProp(field, source, *v, false, false))
			case *typegrammar.OptionalUnion:
				unions = append(unions, s.interfaceProp(field, source, v.Union, true, false))
			case *typegrammar.UnionSlice:
				unions = append(unions, s.interfaceProp(field, source, v.Union, false, true))
			case *typegrammar.Required:
				enums = appendEnumFieldPlan(enums, def.Name.Name, field, source, v.Type, false, false)
			case *typegrammar.Optional:
				enums = appendEnumFieldPlan(enums, def.Name.Name, field, source, v.Type, true, false)
			case *typegrammar.Nullable:
				enums = appendEnumFieldPlan(enums, def.Name.Name, field, source, v.Type, false, true)
			}
		}
		if len(unions) == 0 && len(enums) == 0 {
			continue
		}
		// The owner's own fields come first, then each embedded struct's in
		// declaration order; enum adapters are emitted by field name.
		slices.SortStableFunc(unions, func(a, b InterfaceProp) int { return compareFieldSource(a.source, b.source) })
		slices.SortStableFunc(enums, func(a, b EnumFieldPlan) int { return strings.Compare(a.goName, b.goName) })
		if err := validateOwnerCodecInterfaceFields(def.Name.Name, unions); err != nil {
			return nil, err
		}
		owners[def.Name.Name] = OwnerCodec{
			Name:        def.Name.Name,
			UnionFields: unions,
			EnumFields:  enums,
			Initial:     strings.ToLower(def.Name.Name[0:1]),
		}
	}
	return owners, nil
}

func (s *SchemaBuilder) interfaceProp(field typegrammar.Field, source fieldSource, union typegrammar.Union, optional, repeated bool) InterfaceProp {
	prop := InterfaceProp{
		goName:   field.GoName,
		jsonName: field.JSONName,
		tag:      source.tag,
		path:     source.path,
		source:   source,
		Union:    union,
		Optional: optional,
		Repeated: repeated,
	}
	if scan, ok := s.Scan.GetPackage(union.Interface.PackagePath); ok {
		prop.interfacePkgName = scan.Pkg.Name
		prop.DiscPropName = scan.Interfaces[union.Interface.Name].Discriminator
	}
	return prop
}

func appendEnumFieldPlan(plans []EnumFieldPlan, owner string, field typegrammar.Field, source fieldSource, t typegrammar.Type, optional, nullable bool) []EnumFieldPlan {
	enum, ok := t.(*typegrammar.Enum)
	if !ok || enum.Mode != typegrammar.EnumNames {
		return plans
	}
	entries := make([]EnumEntry, 0, len(enum.Members))
	for _, member := range enum.Members {
		entries = append(entries, EnumEntry{ConstName: member.Name, WireName: member.Name})
	}
	return append(plans, EnumFieldPlan{
		goName:          field.GoName,
		jsonName:        field.JSONName,
		tag:             source.tag,
		optional:        optional,
		nullable:        nullable,
		EnumType:        enum.GoType,
		EnumTypeName:    enum.GoType.Name,
		Entries:         entries,
		MarshalerFunc:   "__jsonMarshalEnum__" + owner + "__" + field.GoName,
		UnmarshalerFunc: "__jsonUnmarshalEnum__" + owner + "__" + field.GoName,
	})
}

// compareFieldSource orders fields the way the generator always has: a
// struct's own fields before those it promotes, embedded structs in
// declaration order, and declaration order within each struct.
func compareFieldSource(a, b fieldSource) int {
	for i := 0; i < len(a.steps) && i < len(b.steps); i++ {
		if a.steps[i] != b.steps[i] {
			return a.steps[i] - b.steps[i]
		}
	}
	if len(a.steps) != len(b.steps) {
		return len(a.steps) - len(b.steps)
	}
	return a.order - b.order
}

// validateOwnerCodecInterfaceFields refuses an owner whose promoted union
// fields would collide in the generated wrapper struct.
func validateOwnerCodecInterfaceFields(owner string, props []InterfaceProp) error {
	goFields := make(map[string]string, len(props))
	jsonFields := make(map[string]string, len(props))
	for _, prop := range props {
		path := prop.Accessor(owner)
		if previous, exists := goFields[prop.goName]; exists {
			return fmt.Errorf(
				"cannot generate owner codec for %s: promoted registered interface fields %s and %s are ambiguous because they share Go field name %q",
				owner, previous, path, prop.goName,
			)
		}
		goFields[prop.goName] = path
		if previous, exists := jsonFields[prop.jsonName]; exists {
			return fmt.Errorf(
				"cannot generate owner codec for %s: promoted registered interface fields %s and %s are ambiguous because they share JSON property %q",
				owner, previous, path, prop.jsonName,
			)
		}
		jsonFields[prop.jsonName] = path
	}
	return nil
}
