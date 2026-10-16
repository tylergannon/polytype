# Generation publication results

decision: additive bool/error APIs report successful output mutations, including those completed before a later error; worker statistics remain analysis/backend counters.
decision: publication compares actual output bytes rather than schema checksum alone so missing or modified output repairs are observable and equal output preserves mtimes.
proof: baseline and final go test ./... passed; focused result and publication tests passed. just build-tagged, go build ./... and go vet ./... passed. Separate real generator processes showed cold publication true, equal output false with identical mtime, repair true, and an emitted devalue codec roundtrip over an authored Envelope value.
decision: retain programmatic TypeScript's existing selected-files-only ownership semantics; no new sibling pruning or retention promises. Only existing schema/Go removal paths contribute removals.
review: independent reviewer found no material issues; artifact ephemeral/reviews/20261016-generation-results-round-01.md. Parent authorized PR handoff after review.
proof: go generate ./... and JSONSCHEMA_NO_CHANGES=1 go generate ./... passed without changing tracked generated outputs. Final full suite includes the publication unit test; no required check remains locally.
handoff: native repository auto-merge and squash merge are enabled. Preparing feature PR after the reviewed checkpoint; preserve this worktree while merge is queued or downstream local adoption needs it.
