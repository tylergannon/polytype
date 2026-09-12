package structs

//go:generate go run ../../polytype/ --validate

import (
	"time"

	"github.com/tylergannon/polytype"
)

// Address represents a physical location.
type Address struct {
	// Street is the street address.
	Street string `json:"street"`

	// City is the city name.
	City string `json:"city"`

	// State is the state or province.
	State string `json:"state"`

	// PostalCode is the postal or zip code.
	PostalCode string `json:"postalCode"`

	// Country is the country name.
	Country string `json:"country"`
}

// ContactInfo contains various ways to contact a person.
type ContactInfo struct {
	// Email is the primary email address.
	Email string `json:"email"`

	// Phone is the primary phone number.
	Phone polytype.Optional[string] `json:"phone,omitzero"`

	// AlternateEmails contains additional email addresses.
	AlternateEmails polytype.Optional[[]string] `json:"alternateEmails,omitzero"`

	// COMMENTED OUT: Map fields are not yet supported
	// // AlternatePhones contains additional phone numbers with labels.
	// AlternatePhones map[string]string `json:"alternatePhones,omitempty"`
}

// RetryPolicy demonstrates required fields with default and explicit JSON names.
type RetryPolicy struct {
	// MaxRetries uses its Go field name.
	MaxRetries int

	// TimeoutSeconds also uses its Go field name.
	TimeoutSeconds int

	// BackoffStrategy uses an explicit JSON name.
	BackoffStrategy int `json:"backoff_strategy"`

	// Untagged uses its Go field name.
	Untagged int

	// Ignored is excluded from JSON and generated schemas.
	Ignored int `json:"-"`
}

// Person demonstrates a complex struct with nested fields and embedded types.
type Person struct {
	// ID is a unique identifier.
	ID string `json:"id"`

	// Name is the person's full name.
	Name string `json:"name"`

	// BirthDate is the person's date of birth.
	// This demonstrates using the time.Time type which will be properly handled.
	BirthDate time.Time `json:"birthDate"`

	// COMMENTED OUT: Map fields are not yet supported
	// // Addresses is a map of labeled addresses (e.g., "home", "work").
	// Addresses map[string]Address `json:"addresses,omitempty"`

	// Embed the ContactInfo type.
	// All fields from ContactInfo will be flattened into Person.
	ContactInfo `json:",inline"`

	// Tags are arbitrary labels associated with the person.
	Tags polytype.Optional[[]string] `json:"tags,omitzero"`

	// COMMENTED OUT: Map fields are not yet supported
	// // Metadata contains any additional information.
	// // Using map[string]interface{} allows for arbitrary JSON.
	// Metadata map[string]any `json:"metadata,omitempty"`
}

// Organization demonstrates a complex struct with nested person references.
type Organization struct {
	// ID is a unique identifier.
	ID string `json:"id"`

	// Name is the organization's name.
	Name string `json:"name"`

	// Description is information about the organization.
	Description polytype.Optional[string] `json:"description,omitzero"`

	// Founded is when the organization was established.
	// COMMENTED OUT: External package types (time.Time) need special handling
	// Founded time.Time `json:"founded"`

	// HeadquartersAddress is the main address.
	HeadquartersAddress Address `json:"headquartersAddress"`

	// Employees is a list of people that work for the organization.
	Employees polytype.Optional[[]Person] `json:"employees,omitzero"`

	// Departments is a tree structure of departments within the organization.
	Departments polytype.Optional[[]Department] `json:"departments,omitzero"`
}

// Department represents a division within an organization.
type Department struct {
	// Name of the department.
	Name string `json:"name"`

	// Manager is the person in charge of the department.
	Manager polytype.Optional[*Person] `json:"manager,omitzero"`

	// ParentDepartment is the name of the parent department, if any.
	ParentDepartment polytype.Optional[string] `json:"parentDepartment,omitzero"`

	// COMMENTED OUT: Recursive/circular references are not yet supported
	// // SubDepartments demonstrates recursive structures.
	// // This creates a tree structure of departments.
	// SubDepartments []Department `json:"subDepartments,omitempty"`
}
