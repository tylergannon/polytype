package builder

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"

	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// RootType is one shape a caller asked to have lowered. Type may be anonymous,
// so Position carries the source location to report in diagnostics.
type RootType struct {
	Type     types.Type
	Position token.Position
}

// LowerRoots lowers caller-supplied roots and their reachable named
// dependencies into the validated static type grammar, returning the
// definitions and one grammar node per root in source order.
//
// Roots are matched by name, never by types.Type identity, so a caller's own
// packages.Load result works. A named root needs no Declare marker; the
// package holding it is loaded on demand if marker-seeded traversal never
// reached it.
func (s *SchemaBuilder) LowerRoots(roots []RootType) (typegrammar.Definitions, []typegrammar.Type, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("lower roots: nil SchemaBuilder")
	}
	if err := typeGrammarPackageError(s.Scan); err != nil {
		return nil, nil, err
	}
	l := typeGrammarLowerer{
		builder: s,
		index:   make(map[typegrammar.Name]int),
		resolve: func(name typegrammar.Name) error {
			return s.Scan.EnsureRemoteType(name.PackagePath, name.Name)
		},
	}
	nodes := make([]typegrammar.Type, 0, len(roots))
	for i, root := range roots {
		node, err := l.rootType(root.Type, root.Position)
		if err != nil {
			return nil, nil, fmt.Errorf("lower root %d (%s): %w", i, root.Type, err)
		}
		nodes = append(nodes, node)
	}
	// The roots go through the same admission boundary as the definitions.
	// A root node is not reachable from any definition, so validating only
	// l.defs would return shapes the grammar excludes -- a []byte root, for
	// one -- with no error at all.
	if err := l.defs.ValidateWithRoots(nodes); err != nil {
		return nil, nil, fmt.Errorf("validate type definitions: %w", err)
	}
	return l.defs, nodes, nil
}

// rootType lowers a go/types type structurally. A named type hands off to the
// dst-based named() lowering, which owns every source-level fact (tags,
// embedding, registrations, enum membership) that go/types alone cannot see.
func (l *typeGrammarLowerer) rootType(t types.Type, pos token.Position) (typegrammar.Type, error) {
	switch node := types.Unalias(t).(type) {
	case *types.Basic:
		if kind, ok := scalarKind(node.Name()); ok {
			return &typegrammar.Scalar{Kind: kind}, nil
		}
		return nil, fmt.Errorf(msgUnsupportedTypeExpr, t, pos)

	case *types.Pointer:
		element, err := l.rootType(node.Elem(), pos)
		if err != nil {
			return nil, err
		}
		return &typegrammar.Pointer{Element: element}, nil

	case *types.Slice:
		element, err := l.rootType(node.Elem(), pos)
		if err != nil {
			return nil, err
		}
		return &typegrammar.Slice{Element: element}, nil

	case *types.Array:
		element, err := l.rootType(node.Elem(), pos)
		if err != nil {
			return nil, err
		}
		return &typegrammar.Array{Length: node.Len(), Element: element}, nil

	case *types.Named:
		return l.rootNamed(node, pos)

	case *types.Map:
		return nil, fmt.Errorf(msgMapType, pos)
	case *types.Chan:
		return nil, fmt.Errorf(msgChanType, pos)
	case *types.Signature:
		return nil, fmt.Errorf(msgFuncType, pos)
	case *types.Interface:
		return nil, fmt.Errorf(msgInterfaceType, pos)
	default:
		return nil, fmt.Errorf(msgUnsupportedTypeExpr, t, pos)
	}
}

func (l *typeGrammarLowerer) rootNamed(node *types.Named, pos token.Position) (typegrammar.Type, error) {
	obj := node.Obj()
	if obj.Pkg() == nil {
		// A universe-scope named type (error) has no wire shape here.
		return nil, fmt.Errorf(msgUnsupportedTypeExpr, node, pos)
	}
	// Optional[T]/Nullable[T] and every other instantiated generic reach the
	// dst lowering as an IndexExpr; refuse them here in the same words.
	if node.TypeArgs().Len() > 0 {
		return nil, fmt.Errorf(msgPresenceWrapper, pos)
	}
	name := typegrammar.Name{PackagePath: obj.Pkg().Path(), Name: obj.Name()}
	if syntax.IsTimeType(name.PackagePath, name.Name) {
		return &typegrammar.Time{}, nil
	}
	if err := l.ensurePackage(name); err != nil {
		if !errors.Is(err, errPackageNotLoaded) {
			return nil, err
		}
		return nil, fmt.Errorf(msgUnresolvedExternal, name)
	}
	if err := l.named(name); err != nil {
		return nil, err
	}
	return &typegrammar.Ref{Target: name}, nil
}
