package v1_enums_stringmode

import (
	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/internal/builder/testfixtures/v1_enums_stringmode/palette"
)

type Color int

const (
	ColorZero  Color = 0
	ColorRed   Color = -2
	ColorGreen Color = 7
	ColorBlue  Color = 42
)

var colorStringCalls int

func (Color) String() string {
	colorStringCalls++
	return "this-method-does-not-define-the-wire-name"
}

type Finish string

const (
	FinishReady Finish = `ready"now`
	FinishDone  Finish = "done"
)

type Paint struct {
	C        Color                    `json:"c"`
	Optional polytype.Optional[Color] `json:"optional,omitzero"`
	Nullable polytype.Nullable[Color] `json:"nullable"`
	Numeric  Color                    `json:"numeric"`
	Finish   Finish                   `json:"finish"`
	Remote   palette.Level            `json:"remote"`
}

// Color declares itself as an enum; the generator emits its typed constants.
func (Color) enum() {}
