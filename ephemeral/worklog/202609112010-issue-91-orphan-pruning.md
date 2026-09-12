# Issue 91: prune orphaned generated schemas

decision: Continue the existing task worktree on codex/issue-91-orphan-pruning from origin/main 0f3c696 after PR #119 merged.
proof: Baseline `go test ./...` passed before implementation.
scope: Remove stale generator-owned schema artifacts after successful generation, report them under `--no-changes`, and preserve application files without a generated checksum sidecar.
design: Treat a matching `.sum` sidecar as the ownership marker. Preflight every orphaned pair before deletion and refuse to delete an orphan whose contents no longer match its checksum.
friction: The first integration fixture used `polytype.Compose` in a build-tagged registration file, but the source scanner intentionally accepts individual declaration markers there. Replaced it with two `polytype.Declare` markers; executable configuration still supports Compose.
change: RenderSchemas now derives the complete expected artifact set, validates stale checksum-owned pairs before any writes, prunes unchanged orphans after successful generation, and returns pending removals to the CLI for `--no-changes` diagnostics. Programmatic codegen uses the same behavior.
proof: TestGenerationPrunesOrphanedOwnedSchemas proves no-change reporting without deletion, refusal and fail-before-mutation for an edited orphan, successful pruning after checksum restoration, preservation of current schemas, and preservation of an application JSON file without a checksum ownership marker.
proof: `go generate ./...`, `go test ./...`, `go vet ./...`, `just build-tagged`, website `npm run check`, internal link checking, and `git diff --check` passed after implementation.
