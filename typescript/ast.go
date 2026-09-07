package typescript

type typeExpr interface {
	typeExpression()
}

type keywordType string
type literalType string
type referenceType string

type objectType struct {
	properties []property
}

type property struct {
	name        string
	description string
	optional    bool
	typeExpr    typeExpr
}

type genericType struct {
	name      string
	arguments []typeExpr
}

type unionType struct {
	members []typeExpr
}

type intersectionType struct {
	members []typeExpr
}

func (keywordType) typeExpression()      {}
func (literalType) typeExpression()      {}
func (referenceType) typeExpression()    {}
func (objectType) typeExpression()       {}
func (genericType) typeExpression()      {}
func (unionType) typeExpression()        {}
func (intersectionType) typeExpression() {}

func union(members ...typeExpr) typeExpr {
	flat := make([]typeExpr, 0, len(members))
	for _, member := range members {
		if nested, ok := member.(unionType); ok {
			flat = append(flat, nested.members...)
		} else {
			flat = append(flat, member)
		}
	}
	if len(flat) == 1 {
		return flat[0]
	}
	return unionType{members: flat}
}

func intersection(members ...typeExpr) typeExpr {
	flat := make([]typeExpr, 0, len(members))
	for _, member := range members {
		if nested, ok := member.(intersectionType); ok {
			flat = append(flat, nested.members...)
		} else {
			flat = append(flat, member)
		}
	}
	if len(flat) == 1 {
		return flat[0]
	}
	return intersectionType{members: flat}
}

type typeAlias struct {
	name        string
	description string
	typeExpr    typeExpr
}
