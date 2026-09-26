# Projection lint checks

decision: Issue #157 keeps independent TypeScript and JSDoc projection paths; generated output gets language-specific formatter and linter checks without a shared backend framework.
correction: The user prefers oxfmt and oxlint over the existing tsc invocation for generated TypeScript checks; use tsgo only if the later JSDoc consumer proof needs semantic type diagnostics.
decision: Oxfmt will parse generated declarations in the test without requiring a printer-style rewrite; oxlint runs on the generated files with warnings treated as failures.
proof: Fresh `npm ci --ignore-scripts --no-audit --no-fund` installed the pinned tools; `go test ./typescript -run TestGenerateEdgeCasesLint -count=1 -v` ran them without a skip; `go test ./...` passed. Oxfmt rejected deliberately invalid TypeScript with exit status 2.
decision: Issue #157 now requires the same JSDoc lint check and reserves `tsgo` for checked-JavaScript semantic diagnostics. The issue also records separate language projection paths.
branch: `codex/projection-lint` contains the repository changes; issue #157 was updated directly. No PR has been opened at this checkpoint.
