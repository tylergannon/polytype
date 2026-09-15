package dep

// Item shares its bare name with model.Item but lives in a different package.
type Item struct {
	Name string `json:"name"`
}
