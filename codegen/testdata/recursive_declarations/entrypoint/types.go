package entrypoint

type Node interface{ node() }

type Leaf struct {
	Name string `json:"name"`
}

func (Leaf) node() {}

type Branch struct {
	Name string `json:"name"`
	Body []Node `json:"body"`
}

func (Branch) node() {}

type Watcher struct {
	Name     string    `json:"name"`
	Watchers []Watcher `json:"watchers"`
}

type Tree struct {
	Body     []Node    `json:"body"`
	Watchers []Watcher `json:"watchers"`
}
