package v1_interfaces_options

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

//go:generate go run ./gen

type IFace interface{ isIface() }

type jsonString string

func (s *jsonString) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = jsonString("json:" + value)
	return nil
}

type Impl1 struct {
	X jsonString `json:"x"`
}

func (Impl1) isIface() {}

type Impl2 struct {
	Y int `json:"y"`
}

func (Impl2) isIface() {}

type PlainInner struct {
	A string `json:"a"`
	B string `json:"b"`
}

type Plain struct {
	Tags  []string                       `json:"tags"`
	Inner polytype.Nullable[*PlainInner] `json:"inner"`
	Count int                            `json:"count"`
}

type Owner struct {
	IF         IFace                     `json:"if"`
	IFaces     []IFace                   `json:"ifs"`
	OptionalIF polytype.Optional[IFace]  `json:"optional_if,omitzero"`
	Label      polytype.Optional[string] `json:"label,omitzero"`
	Timeout    polytype.Nullable[int]    `json:"timeout"`
}
