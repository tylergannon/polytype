package enums

import "github.com/tylergannon/polytype"

//go:generate go run ../../polytype/ --validate

// Status represents the state of an item in the system.
// This enum type will be represented as a string with a fixed set of possible values.
type Status string

// These constants define the possible values for the Status enum.
// Each constant will be included in the enum schema with its documentation.
const (
	// StatusPending indicates the item is waiting to be processed.
	StatusPending Status = "pending"

	// StatusInProgress indicates the item is currently being processed.
	StatusInProgress Status = "in_progress"

	// StatusCompleted indicates the item has been successfully processed.
	StatusCompleted Status = "completed"

	// StatusFailed indicates the processing of the item has failed.
	StatusFailed Status = "failed"
)

// Priority represents the importance level of an item.
type Priority string

const (
	// PriorityLow indicates minimal urgency.
	PriorityLow Priority = "low"

	// PriorityMedium indicates standard urgency.
	PriorityMedium Priority = "medium"

	// PriorityHigh indicates immediate attention required.
	PriorityHigh Priority = "high"
)

// Task demonstrates a struct that uses enum fields.
// This shows how enum types can be used within other structures.
type Task struct {
	// ID is a unique identifier for the task.
	ID string `json:"id"`

	// Name is the title of the task.
	Name string `json:"name"`

	// Description provides details about the task.
	Description polytype.Optional[string] `json:"description,omitzero"`

	// Status indicates the current state of the task.
	// This will use the Status enum type defined above.
	Status Status `json:"status"`

	// Priority indicates how important this task is.
	Priority Priority `json:"priority"`

	// Tags are additional categorization for the task.
	Tags polytype.Optional[[]string] `json:"tags,omitzero"`
}

// SliceOfStatus demonstrates how to use a slice of enum values.
// This type will be represented as an array of enum values in the schema.
type SliceOfStatus []Status

// Status declares itself as an enum; the generator emits its typed constants.
func (Status) enum() {}

// Priority declares itself as an enum; the generator emits its typed constants.
func (Priority) enum() {}
