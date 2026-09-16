package interfaces

// Overall description for MyEnumType.
type MyEnumType string

const (
	// The first possible item
	Val1 MyEnumType = "val1"
	// Use this one second
	Val2 MyEnumType = "val2"
	// Use this one third
	Val3 MyEnumType = "val3"
	// Fourth option.
	Val4 MyEnumType = "val4"
)

type TestInterface interface {
	marker()
}

// Make this look pretty interesting.
type FancyStruct struct {
	// A list of enumVals that can be really meaningful when used correctly.
	EnumVal []MyEnumType `json:"enumVal"`

	// Something tells me this isn't going to make it into the document.
	IFace TestInterface `json:"iface"`
	// Every element is dispatched through the same registered interface union.
	IFaces []TestInterface `json:"ifaces"`
	// Here are the details.  Make sure you fill them out.
	Details [](*struct {
		Name      string `json:"-"`
		OtherName string `json:"-"`
		funk      int
		// Highly interesting stuff regarding Foo and Bar.
		Foo, Bar string

		EnumVal MyEnumType `json:"enumVal"`
	})
}

// Put this down when you feel really great about life.
type TestInterface1 struct {
	Field1 string `json:"field1"` // obvious
	Field2 string `json:"field2"` // oblivious
	Field3 int    `json:"field3"` // obsequious
}

func (t TestInterface1) marker() {}

// This is seriously silly, don't you imagine so?
type TestInterface2 struct {
	Fork3 int `json:"fork3"`
	Fork4 int `json:"fork4"`
	Fork5 int `json:"fork5"`
}

func (t TestInterface2) marker() {}

type PointerToTestInterface struct {
	Fork99 int `json:"fork99"`
	Fork10 int `json:"fork10"`
	Fork11 int `json:"fork11"`
}

func (t *PointerToTestInterface) marker() {}

var _ TestInterface = &PointerToTestInterface{}

// MyEnumType declares itself as an enum; the generator emits its typed constants.
func (MyEnumType) enum() {}
