package optionality

import schema "github.com/tylergannon/polytype"

type Detail struct {
	Value string `json:"value"`
}

type Config struct {
	OrdinaryInt    int                      `json:"ordinary_int"`
	OptionalInt    schema.Optional[int]     `json:"optional_int,omitzero"`
	OptionalEmpty  schema.Optional[string]  `json:"optional_empty,omitzero"`
	OptionalObject schema.Optional[Detail]  `json:"optional_object,omitzero"`
	OptionalPtr    schema.Optional[*Detail] `json:"optional_ptr,omitzero"`
	OptionalSlice  schema.Optional[[]int]   `json:"optional_slice,omitzero"`
	NullableInt    schema.Nullable[int]     `json:"nullable_int"`
	NullableObject schema.Nullable[Detail]  `json:"nullable_object"`
	NullablePtr    schema.Nullable[*Detail] `json:"nullable_ptr"`
}
