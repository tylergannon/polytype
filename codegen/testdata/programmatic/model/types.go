package model

// Envelope is a transport value generated without schema registration.
type Envelope struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
	State   State  `json:"state"`
	Event   Event  `json:"event"`
}

type State string

const StateReady State = "ready"

func (State) enum() {}

type Event interface{ event() }

type HTTPEventCreated struct {
	ID string `json:"id"`
}

func (HTTPEventCreated) event() {}
