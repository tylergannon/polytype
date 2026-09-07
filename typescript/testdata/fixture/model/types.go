// Package model declares a type graph a driving generator projects to
// TypeScript through the library path. It carries no build-tagged
// registration file and no Declare marker; the test that needs the CLI's
// tagged file writes it into a separate copy.
package model

import (
	"time"

	"github.com/tylergannon/polytype"
)

// Priority ranks an envelope.
type Priority int

const (
	PriorityLow  Priority = 1
	PriorityHigh Priority = 5
)

func (Priority) enum() {}

// Status is the string enum.
type Status string

const (
	StatusOpen Status = "open"
	StatusDone Status = "done"
)

func (Status) enum() {}

// Event is sealed by isEvent; the discriminator is the default "type".
type Event interface{ isEvent() }

// Created is a value variant.
type Created struct {
	Name string `json:"name"`
}

func (Created) isEvent() {}

// Deleted is a pointer variant.
type Deleted struct {
	ID string `json:"id"`
}

func (*Deleted) isEvent() {}

// Detail is reached through a pointer, a Nullable and directly.
type Detail struct {
	Note string `json:"note"`
}

// Envelope is the first root.
type Envelope struct {
	Event    Event                      `json:"event"`
	Events   []Event                    `json:"events"`
	Maybe    polytype.Optional[Event]   `json:"maybe,omitzero"`
	Label    polytype.Optional[string]  `json:"label,omitzero"`
	Detail   polytype.Nullable[*Detail] `json:"detail"`
	Shared   Detail                     `json:"shared"`
	Status   Status                     `json:"status"`
	Priority Priority                   `json:"priority"`
	When     time.Time                  `json:"when"`
	Odd      string                     `json:"strange-key"`
}

// Composition is the second root.
type Composition struct {
	A [][]int                     `json:"a"`
	B []*Detail                   `json:"b"`
	C polytype.Optional[[]Status] `json:"c,omitzero"`
	D polytype.Nullable[Status]   `json:"d"`
	E [2]string                   `json:"e"`
	F *bool                       `json:"f"`
	G []Envelope                  `json:"g"`
}
