package model

type Todo struct {
	Note *string `json:"note"`
	Tags []string `json:"tags"`
	Title string `json:"title,omitempty"`
}
