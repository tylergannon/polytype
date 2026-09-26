package builder

import (
	"github.com/tylergannon/polytype/javascript"
)

const javaScriptTypesFile = "types.js"

// prepareJavaScriptOutput preflights the requested JavaScript module without
// writing it. types.ts and index.ts are obsolete candidates: a generated
// sibling left by TypeScript mode is removed, while a headerless
// application-owned file is preserved. A generated types.js is replaced, and
// a headerless one is refused rather than overwritten.
func prepareJavaScriptOutput(dir string, generated []javascript.File) (*outputPlan, error) {
	desired := make([]plannedOutputFile, 0, len(generated))
	for _, file := range generated {
		desired = append(desired, plannedOutputFile{name: file.Name, content: file.Content})
	}
	return prepareOutputPlan(dir, "JavaScript", javascript.GeneratedHeader, desired,
		[]string{javaScriptTypesFile}, []string{typeScriptTypesFile, typeScriptBarrelFile})
}
