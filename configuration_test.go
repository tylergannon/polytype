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
		StringerEnum(Field[configuredPerson, int]("Status")).
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

func TestFieldRejectsWrongValueType(t *testing.T) {
	_, err := ResolveConfiguration(Declare[configuredPerson]().StringerEnum(Field[configuredPerson, string]("Status")))
	require.ErrorContains(t, err, "field Status has type int")
}

type configuredEvent interface{ configuredEvent() }

func TestSealedUnionAcceptsCustomInflection(t *testing.T) {
	config := Compose(
		Declare[configuredPerson](),
		SealedUnion[configuredEvent]("kind", func(name string) string { return "event:" + name }),
	)
	spec, err := ResolveConfiguration(config)
	require.NoError(t, err)
	require.Equal(t, "event:Created", spec.SealedUnions[0].Inflect("Created"))
}

func TestSealedUnionRejectsInvalidInflectionArguments(t *testing.T) {
	_, err := ResolveConfiguration(Compose(
		Declare[configuredPerson](),
		SealedUnion[configuredEvent]("kind", nil),
	))
	require.ErrorContains(t, err, "inflection function must not be nil")

	_, err = ResolveConfiguration(Compose(
		Declare[configuredPerson](),
		SealedUnion[configuredEvent]("kind", Snake, Camel),
	))
	require.ErrorContains(t, err, "accepts at most one")
}
