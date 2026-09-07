package consumer

import (
	"time"

	"github.com/tylergannon/polytype"
)

type Event interface{ event() }
type Created struct {
	Name string `json:"name"`
}

func (Created) event() {}

type Deleted struct {
	ID int `json:"id"`
}

func (*Deleted) event() {}

// Detail comments safely contain a terminator: */ and Unicode: 雪.
type Detail struct {
	Message string `json:"message"`
}
type Status string

const (
	Ready     Status = "ready"
	Waiting   Status = "wait\"ing"
	Unrelated        = "not-a-status"
)

const Converted = Status("conv" + "erted")

type Priority int

const (
	Low Priority = iota
	High
)

const Urgent Priority = 1 << 3
const NotPriority = 123
const Medium = Priority(4)

func (p Priority) String() string { return "not-the-wire-value" }

type Empty struct{}
type Array struct {
	Value string `json:"value"`
}
type Omit struct {
	Value int `json:"value"`
}
type Envelope struct {
	Event        Event                      `json:"event"`
	Other        Event                      `json:"other"`
	Maybe        polytype.Optional[Event]   `json:"maybe,omitzero"`
	Events       []Event                    `json:"events"`
	Label        polytype.Optional[string]  `json:"label,omitzero"`
	Detail       polytype.Nullable[*Detail] `json:"detail"`
	Shared       Detail                     `json:"shared"`
	Status       Status                     `json:"status"`
	Priority     Priority                   `json:"priority"`
	PriorityName Priority                   `json:"priority_name"`
	When         time.Time                  `json:"when"`
	Odd          string                     `json:"strange-key"`
	Empty        Empty                      `json:"empty"`
}

type Composition struct {
	A [][]int                     `json:"a"`
	B []*Detail                   `json:"b"`
	C polytype.Optional[[]Status] `json:"c,omitzero"`
	D polytype.Nullable[Status]   `json:"d"`
	E [2]string                   `json:"e"`
	F *bool                       `json:"f"`
	G []Envelope                  `json:"g"`
	H Array                       `json:"h"`
	I Omit                        `json:"i"`
}

// Status declares itself as an enum; the generator emits its typed constants.
func (Status) enum() {}

// Priority declares itself as an enum; the generator emits its typed constants.
func (Priority) enum() {}

// The fixture is generated into a temporary consumer, so it references its
// marked enum types by hand; the generated file emits the same assertions.
var (
	_ interface{ enum() } = Ready
	_ interface{ enum() } = Low
)
