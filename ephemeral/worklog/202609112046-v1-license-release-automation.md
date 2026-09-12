# v1 license and release automation

decision: Use the OSI-approved Zero-Clause BSD license (`0BSD`) because the maintainer requested the most permissive choice. Use maintained, off-the-shelf GitHub Actions for Conventional Commit enforcement and direct semantic releases; do not create a repository-specific versioning program.

baseline: `go test ./...` passed on `origin/main` at `184d60a24ca8687a0518811522cf3fd1a473afa1` before changes.

research: Release Please was rejected for this workflow because it creates a release pull request that requires another merge. `semantic-release` publishes a tag and GitHub Release directly after successful main-branch CI. `amannn/action-semantic-pull-request` validates PR titles, which become the squash-merge commit titles.

implementation: Added the canonical 0BSD text and README link. Added a pinned `amannn/action-semantic-pull-request` PR-title workflow and a pinned `cycjimmy/semantic-release-action` job after `test-and-generate`, configured to run semantic-release 25.0.9 with only commit analysis, release-note generation, and GitHub publication. Removed the deleted `llms.txt` from website workflow path filters. Changed the repository squash-merge setting to always use the PR title and body so the validated title is the commit semantic-release reads.

correction: A real semantic-release dry run found that prerelease tags are not a stable baseline: before v1.0.0 exists, the tool selects v0.11.3 and proposes v0.12.0 from the historical conventional commits. Gate the release job on repository variable `SEMANTIC_RELEASE_ENABLED`; leave it disabled through the setup merge, publish v1.0.0, then set the variable to `true` so later main merges use v1.0.0 as their baseline.

proof:

- `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` passed for all workflows.
- An authenticated `semantic-release@25.0.9 --dry-run` loaded only commit analysis, release notes, and GitHub publication. Its v0.12.0 proposal from the old v0.11.3 stable tag demonstrated the bootstrap risk; `SEMANTIC_RELEASE_ENABLED=false` is verified in the repository.
- Ruby parsed every workflow as YAML and Python parsed `.releaserc.json`.
- `go generate ./...` passed without changing generated artifacts.
- `go vet ./...` passed.
- `just build-tagged` passed.
- `go test ./...` passed after all repository changes.
- `git diff --check` passed.
