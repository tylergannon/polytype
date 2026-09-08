package polytype

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type configuredPerson struct {
	Name   string
	Status int
}

func (configuredPerson) Schema() json.RawMessage { return nil }

func TestDeclareReturnsExecutableConfiguration(t *testing.T) {
	config := Declare(configuredPerson.Schema).
		StringerEnum(Field[configuredPerson]("Status")).
		Ref()

	spec, err := ResolveConfiguration(config)
	require.NoError(t, err)
	require.Len(t, spec.Declarations, 1)
	require.Equal(t, "configuredPerson", spec.Declarations[0].Type.Name)
	require.Equal(t, "Schema", spec.Declarations[0].EntrypointName)
	require.Equal(t, []RuleKind{RuleStringerEnum, RuleRef}, []RuleKind{
		spec.Declarations[0].Rules[0].Kind,
		spec.Declarations[0].Rules[1].Kind,
	})
	require.Equal(t, "Status", spec.Declarations[0].Rules[0].Field.Name)
}

func TestDeclareWithoutEntrypointDoesNotImplySchema(t *testing.T) {
	spec, err := ResolveConfiguration(Declare[configuredPerson]())
	require.NoError(t, err)
	require.Empty(t, spec.Declarations[0].EntrypointName)
}

func TestEvaluatedFieldExplainsHowToRetainIdentity(t *testing.T) {
	_, err := ResolveConfiguration(Declare[configuredPerson]().StringerEnum(configuredPerson{}.Status))
	require.ErrorContains(t, err, `polytype.Field[configuredPerson]("FieldName")`)
}
