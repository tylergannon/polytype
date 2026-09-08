package polytype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscriminatorInflections(t *testing.T) {
	for _, test := range []struct {
		name       string
		input      string
		wantSnake  string
		wantCamel  string
		wantPascal string
	}{
		{name: "words", input: "PaymentMethod", wantSnake: "payment_method", wantCamel: "paymentMethod", wantPascal: "PaymentMethod"},
		{name: "initialism", input: "HTTPServerEvent", wantSnake: "http_server_event", wantCamel: "httpServerEvent", wantPascal: "HTTPServerEvent"},
		{name: "unicode", input: "ÉxitoEvent", wantSnake: "éxito_event", wantCamel: "éxitoEvent", wantPascal: "ÉxitoEvent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.wantSnake, Snake(test.input))
			require.Equal(t, test.wantCamel, Camel(test.input))
			require.Equal(t, test.wantPascal, Pascal(test.input))
		})
	}
}
