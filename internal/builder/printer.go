package builder

import (
	"bytes"
	"fmt"
	"text/template"

	"golang.org/x/tools/imports"
)

// renderTemplate renders a template with the given data and returns the resulting string.
func RenderTemplate(tmplContent string, data any) (bytes.Buffer, error) {
	var buf bytes.Buffer
	tmpl, err := template.New("configTemplate").Parse(tmplContent)
	if err != nil {
		return buf, fmt.Errorf("failed to parse template: %w", err)
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return buf, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf, nil
}

func FormatCodeWithGoimports(source []byte) ([]byte, error) {
	options := &imports.Options{
		Comments:  true, // Preserve comments
		TabIndent: true, // Use tabs for indentation
		TabWidth:  8,    // As the goimports command; 0 leaves import groups unseparated
	}

	formatted, err := imports.Process("", source, options)
	if err != nil {
		return nil, fmt.Errorf("failed to format code: %w", err)
	}

	return formatted, nil
}
