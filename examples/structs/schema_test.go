package structs

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tylergannon/polytype"
)

func TestStructSchemaPropertyNamesMatchJSON(t *testing.T) {
	value := RetryPolicy{
		MaxRetries:      3,
		TimeoutSeconds:  30,
		BackoffStrategy: 2,
		Untagged:        1,
		Ignored:         99,
	}

	jsonBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Failed to marshal RetryPolicy: %v", err)
	}
	var jsonObject map[string]json.RawMessage
	if err := json.Unmarshal(jsonBytes, &jsonObject); err != nil {
		t.Fatalf("Failed to parse RetryPolicy JSON: %v", err)
	}

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(value.Schema(), &schema); err != nil {
		t.Fatalf("Failed to parse RetryPolicy schema: %v", err)
	}

	keys := func(object map[string]json.RawMessage) []string {
		result := make([]string, 0, len(object))
		for key := range object {
			result = append(result, key)
		}
		slices.Sort(result)
		return result
	}

	jsonKeys := keys(jsonObject)
	schemaKeys := keys(schema.Properties)
	if !slices.Equal(schemaKeys, jsonKeys) {
		t.Fatalf("Schema property names %v do not match encoding/json keys %v", schemaKeys, jsonKeys)
	}
}

func TestPersonSchemaWithTime(t *testing.T) {
	// Get the generated schema
	person := Person{}
	schemaBytes := person.Schema()

	// Parse the schema
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	// Check that birthDate is properly defined
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Schema missing properties field")
	}

	birthDate, ok := props["birthDate"].(map[string]any)
	if !ok {
		t.Fatal("Missing birthDate property")
	}

	// Verify it's a string type
	if birthDate["type"] != "string" {
		t.Errorf("birthDate type = %v, want string", birthDate["type"])
	}

	// Verify the description includes RFC3339 guidance
	desc, ok := birthDate["description"].(string)
	if !ok {
		t.Fatal("birthDate missing description")
	}

	if !strings.Contains(desc, "RFC3339") {
		t.Errorf("birthDate description doesn't mention RFC3339: %s", desc)
	}

	if !strings.Contains(desc, "2006-01-02T15:04:05Z07:00") {
		t.Errorf("birthDate description doesn't include example format: %s", desc)
	}
}

func TestPersonJSONMarshalUnmarshal(t *testing.T) {
	// Create a person with a specific time
	original := Person{
		ID:              "123",
		Name:            "John Doe",
		BirthDate:       time.Date(1990, 5, 15, 14, 30, 0, 0, time.UTC),
		Email:           "john@example.com",
		Phone:           polytype.Optional[string]{Present: true, Value: "555-1234"},
		AlternateEmails: polytype.Optional[[]string]{Present: true, Value: []string{"john.doe@example.com"}},
		Tags:            polytype.Optional[[]string]{Present: true, Value: []string{"developer", "golang"}},
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal Person: %v", err)
	}

	// Verify the time is in RFC3339 format
	var jsonMap map[string]any
	if err := json.Unmarshal(jsonBytes, &jsonMap); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	birthDateStr, ok := jsonMap["birthDate"].(string)
	if !ok {
		t.Fatal("birthDate is not a string in JSON")
	}

	// Parse the time string to verify it's valid RFC3339
	parsedTime, err := time.Parse(time.RFC3339, birthDateStr)
	if err != nil {
		t.Errorf("birthDate is not valid RFC3339: %s, error: %v", birthDateStr, err)
	}

	// Unmarshal back to Person
	var unmarshaled Person
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal Person: %v", err)
	}

	// Verify the time matches (compare Unix timestamps to avoid timezone issues)
	if !original.BirthDate.Equal(unmarshaled.BirthDate) {
		t.Errorf("BirthDate mismatch: original=%v, unmarshaled=%v",
			original.BirthDate, unmarshaled.BirthDate)
	}

	// The unmarshaled time should match what we parsed
	if !parsedTime.Equal(unmarshaled.BirthDate) {
		t.Errorf("Parsed time doesn't match unmarshaled: parsed=%v, unmarshaled=%v",
			parsedTime, unmarshaled.BirthDate)
	}
}

func TestValidate_ValidJSON(t *testing.T) {
	validPerson := `{
		"id": "123",
		"name": "Jane Doe",
		"birthDate": "1990-05-15T14:30:00Z",
		"email": "jane@example.com",
		"phone": "555-1234",
		"alternateEmails": ["jane.doe@example.com"],
		"tags": ["developer"]
	}`

	if err := (Person{}).ValidateJSON([]byte(validPerson)); err != nil {
		t.Fatalf("Validate rejected valid Person JSON: %v", err)
	}

	validAddress := `{
		"street": "123 Main St",
		"city": "Springfield",
		"state": "IL",
		"postalCode": "62701",
		"country": "US"
	}`

	if err := (Address{}).ValidateJSON([]byte(validAddress)); err != nil {
		t.Fatalf("Validate rejected valid Address JSON: %v", err)
	}
}

func TestValidate_InvalidJSON(t *testing.T) {
	// Missing required field "name"
	missingRequired := `{
		"id": "123",
		"birthDate": "1990-05-15T14:30:00Z",
		"email": "jane@example.com"
	}`

	if err := (Person{}).ValidateJSON([]byte(missingRequired)); err == nil {
		t.Fatal("Validate accepted Person JSON with missing required field 'name'")
	}

	// Wrong type for field
	wrongType := `{
		"street": 123,
		"city": "Springfield",
		"state": "IL",
		"postalCode": "62701",
		"country": "US"
	}`

	if err := (Address{}).ValidateJSON([]byte(wrongType)); err == nil {
		t.Fatal("Validate accepted Address JSON with wrong type for 'street'")
	}

	// Additional property not allowed
	extraField := `{
		"street": "123 Main St",
		"city": "Springfield",
		"state": "IL",
		"postalCode": "62701",
		"country": "US",
		"planet": "Earth"
	}`

	if err := (Address{}).ValidateJSON([]byte(extraField)); err == nil {
		t.Fatal("Validate accepted Address JSON with additional property 'planet'")
	}
}

func TestValidate_MalformedJSON(t *testing.T) {
	if err := (Person{}).ValidateJSON([]byte(`not json`)); err == nil {
		t.Fatal("Validate accepted malformed JSON")
	}
}
