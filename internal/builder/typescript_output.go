package builder

import (
	"github.com/tylergannon/polytype/typescript"
)

const (
	typeScriptTypesFile  = "types.ts"
	typeScriptBarrelFile = "index.ts"
)

// prepareTypeScriptOutput preflights the requested TypeScript files without
// writing them. Disabling the barrel makes index.ts an obsolete candidate:
// when it carries the generated header it is removed, and when it does not it
// is an application file that is preserved. types.js is likewise an obsolete
// candidate so switching to TypeScript removes a generated JavaScript module.
func prepareTypeScriptOutput(dir string, generated []typescript.File, barrel bool) (*outputPlan, error) {
	expected := []string{typeScriptTypesFile}
	if barrel {
		expected = append(expected, typeScriptBarrelFile)
	}
	desired := make([]plannedOutputFile, 0, len(generated))
	for _, file := range generated {
		desired = append(desired, plannedOutputFile{name: file.Name, content: file.Content})
	}
	var obsolete []string
	if !barrel {
		obsolete = append(obsolete, typeScriptBarrelFile)
	}
	obsolete = append(obsolete, javaScriptTypesFile)
	return prepareOutputPlan(dir, "TypeScript", typescript.GeneratedHeader, desired, expected, obsolete)
}
