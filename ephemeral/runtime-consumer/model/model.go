package model

type Packet struct {
	Name   string `json:"name"`
	Values []int  `json:"values"`
}
