package v1_interfaces_options

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
	yaml "go.yaml.in/yaml/v4"
)

//go:generate go run ./gen

type IFace interface{ isIface() }

type yamlString string

func (s *yamlString) UnmarshalYAML(node *yaml.Node) error {
	var value string
	if err := node.Decode(&value); err != nil {
		return err
	}
	*s = yamlString("yaml:" + value)
	return nil
}

func (s *yamlString) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = yamlString("json:" + value)
	return nil
}

type Impl1 struct {
	X yamlString `json:"x" yaml:"x"`
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
	IF         IFace                     `json:"if" yaml:"yaml_if"`
	IFaces     []IFace                   `json:"ifs" yaml:"yaml_ifs"`
	OptionalIF polytype.Optional[IFace]  `json:"optional_if,omitzero" yaml:"yaml_optional"`
	Label      polytype.Optional[string] `json:"label,omitzero" yaml:"label"`
	Timeout    polytype.Nullable[int]    `json:"timeout" yaml:"timeout"`
}
