// Package model declares one type graph covering every node kind the static
// type grammar admits, so the generated devalue codecs are exercised whole.
package model

import (
	"time"

	"github.com/tylergannon/polytype"
)

// Priority is an integer enum. It appears twice in Envelope: once in value
// mode and once, through StringerEnum, in constant-name mode.
type Priority int

const (
	PriorityLow  Priority = 1
	PriorityHigh Priority = 5
)

// String deliberately returns something other than the constant name, so a
// test can tell a name-mode wire value from a Stringer result.
func (Priority) String() string { return "not-the-wire-name" }

func (Priority) enum() {}

// Status is a string enum.
type Status string

const (
	StatusOpen Status = "open"
	StatusDone Status = "done"
)

func (Status) enum() {}

// Event is sealed by isEvent. Created is a value variant and Deleted a pointer
// variant; the discriminator is "kind".
type Event interface{ isEvent() }

type Created struct {
	Name string `json:"name"`
}

func (Created) isEvent() {}

type Deleted struct {
	ID string `json:"id"`
}

func (*Deleted) isEvent() {}

// Detail is referenced through a pointer, through a Nullable, and directly.
type Detail struct {
	Note string `json:"note"`
}

// Numbers covers every admitted scalar kind.
type Numbers struct {
	Flag    bool    `json:"flag"`
	Float32 float32 `json:"float32"`
	Float64 float64 `json:"float64"`
	Int     int     `json:"int"`
	Int8    int8    `json:"int8"`
	Int16   int16   `json:"int16"`
	Int32   int32   `json:"int32"`
	Int64   int64   `json:"int64"`
	Uint    uint    `json:"uint"`
	Uint8   uint8   `json:"uint8"`
	Uint16  uint16  `json:"uint16"`
	Uint32  uint32  `json:"uint32"`
	Uint64  uint64  `json:"uint64"`
}

// Envelope is the whole graph in one object.
type Envelope struct {
	Label     string                    `json:"label"`
	Numbers   Numbers                   `json:"numbers"`
	When      time.Time                 `json:"when"`
	Coords    [3]int                    `json:"coords"`
	Tags      []string                  `json:"tags"`
	Detail    *Detail                   `json:"detail"`
	Priority  Priority                  `json:"priority"`
	Ranked    Priority                  `json:"ranked"`
	Status    Status                    `json:"status"`
	Primary   Event                     `json:"primary"`
	Events    []Event                   `json:"events"`
	Alternate polytype.Optional[Event]  `json:"alternate,omitzero"`
	Nickname  polytype.Optional[string] `json:"nickname,omitzero"`
	Owner     polytype.Nullable[Detail] `json:"owner"`
}
