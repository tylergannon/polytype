package union_codec

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/tylergannon/polytype"
)

//go:generate go run ./gen

// Event is sealed by isEvent. Every same-package struct declaring it directly
// is a variant: Created (value), Deleted (pointer), Hooked (value, custom
// JSON hooks), and PointerHookValue (value variant whose hooks live on the
// pointer). The discriminator property is "!kind", declared once with SealedUnion.
type Event interface{ isEvent() }

type Created struct {
	Name string `json:"name"`
}

func (Created) isEvent() {}

type Deleted struct {
	ID string `json:"id"`
}

func (*Deleted) isEvent() {}

var hookMarshalCalls int
var ordinaryMarshalCalls int
var pointerValueHookMarshalCalls int

type Hooked struct {
	Name             string `json:"name"`
	Behavior         string `json:"-"`
	SawDiscriminator bool   `json:"-"`
}

func (Hooked) isEvent() {}

func (h Hooked) MarshalJSON() ([]byte, error) {
	hookMarshalCalls++
	switch h.Behavior {
	case "matching":
		return json.Marshal(map[string]any{"!kind": "Hooked", "name": h.Name})
	case "conflict":
		return json.Marshal(map[string]any{"!kind": "other", "name": h.Name})
	case "non-string":
		return json.Marshal(map[string]any{"!kind": 3, "name": h.Name})
	case "null":
		return []byte("null"), nil
	case "array":
		return []byte("[]"), nil
	case "string":
		return []byte(`"payload"`), nil
	case "malformed":
		return []byte("{"), nil
	case "error":
		return nil, errors.New("hook failed")
	default:
		return json.Marshal(struct {
			Name string `json:"name"`
		}{Name: h.Name})
	}
}

func (h *Hooked) UnmarshalJSON(data []byte) error {
	var wire struct {
		Kind string `json:"!kind"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Kind != "Hooked" {
		return errors.New("Hooked.UnmarshalJSON did not receive the registered discriminator")
	}
	*h = Hooked{Name: wire.Name, SawDiscriminator: true}
	return nil
}

type PointerHookValue struct {
	Name             string `json:"name"`
	SawDiscriminator bool   `json:"-"`
}

func (PointerHookValue) isEvent() {}

func (p *PointerHookValue) MarshalJSON() ([]byte, error) {
	pointerValueHookMarshalCalls++
	return json.Marshal(struct {
		Name string `json:"name"`
	}{Name: "custom:" + p.Name})
}

func (p *PointerHookValue) UnmarshalJSON(data []byte) error {
	var wire struct {
		Kind string `json:"!kind"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Kind != "PointerHookValue" {
		return errors.New("PointerHookValue.UnmarshalJSON did not receive the registered discriminator")
	}
	*p = PointerHookValue{Name: strings.TrimPrefix(wire.Name, "custom:"), SawDiscriminator: true}
	return nil
}

type Ordinary struct {
	Value string `json:"value"`
}

type State int

const (
	StateOpen   State = 1
	StateClosed State = 9
)

func (State) String() string { return "not-the-wire-name" }

func (o *Ordinary) MarshalJSON() ([]byte, error) {
	ordinaryMarshalCalls++
	return json.Marshal(struct {
		Value string `json:"value"`
	}{Value: "custom:" + o.Value})
}

func (o *Ordinary) UnmarshalJSON(data []byte) error {
	var wire struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	o.Value = strings.TrimPrefix(wire.Value, "custom:")
	return nil
}

type Nested struct {
	Event Event `json:"event"`
}

type Envelope struct {
	Primary   Event                     `json:"primary"`
	Events    []Event                   `json:"events"`
	Optional  polytype.Optional[Event]  `json:"optional,omitzero"`
	Alternate polytype.Optional[Event]  `json:"alternate,omitzero"`
	Single    polytype.Optional[Event]  `json:"single,omitzero"`
	Hook      polytype.Optional[Event]  `json:"hook,omitzero"`
	ValueHook polytype.Optional[Event]  `json:"value_hook,omitzero"`
	Nested    Nested                    `json:"nested"`
	Ordinary  Ordinary                  `json:"ordinary"`
	State     State                     `json:"state"`
	Label     string                    `json:"label"`
	Tags      []string                  `json:"tags"`
	Groups    [][]string                `json:"groups"`
	Omitted   polytype.Optional[string] `json:"omitted,omitzero"`
}
