package codegen

import (
	"sync"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/internal/builder"
)

// Worker retains analysis and emitted TypeScript between serialized development
// edits. Its zero value is ready to use. Each Worker retains one request; use
// separate workers for independent targets. Close releases private source files.
//
// Callback-free TypeScript requests reuse captured local source and type analysis.
// The first analysis checks the selected local source closure together so later
// edits retain a coherent graph. The owned session retains at most four graph
// versions. If Options.Session is supplied, its owner must call SetMaxUniverses
// with a positive limit for sustained edits; Worker does not change a shared
// session's retention policy. This bounds graph versions, not bytes.
// Other output modes, workspaces, mutable native/embed dependencies and custom
// loader configurations keep Generate's behavior.
// Versioned module-cache files and the Go toolchain must remain immutable during
// its lifetime. Changing environment, module controls, imports or file membership
// refreshes discovery. A failed edit never replaces the last successful output.
type Worker struct {
	mu         sync.Mutex
	typescript builder.TypeScriptWorker
}

// Generate updates the selected outputs from current authored source. Missing or
// modified output is repaired even when no input changed.
func (w *Worker) Generate(config polytype.Configuration, opts Options) error {
	_, err := w.GenerateResult(config, opts)
	return err
}

// GenerateResult is Generate with the output-change result of GenerateResult.
func (w *Worker) GenerateResult(config polytype.Configuration, opts Options) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	opts.worker = &w.typescript
	return GenerateResult(config, opts)
}

// Close releases retained analysis and private files. The Worker may be reused.
func (w *Worker) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.typescript.Close()
}

// WorkerStats counts retained TypeScript work. Other output modes use Generate
// directly and do not contribute. Emissions count backend executions, including
// executions whose bytes match the existing output; Replays run no backend.
type WorkerStats struct{ Analyses, Emissions, Replays uint64 }

// Stats returns cumulative work for this Worker, including across Close calls.
func (w *Worker) Stats() WorkerStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.typescript.Stats()
	return WorkerStats{Analyses: s.Analyses, Emissions: s.Emissions, Replays: s.Replays}
}
