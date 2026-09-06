# Next step

All four milestones are committed: #106 (ecae5c3 and predecessors), #104
(ec5245c, 8048237, f79774b), #105 (ac9e9cf, 6f04344, 5ecd5f2), #107 (b746649).
The gates were re-run against the working tree just now and all pass:
`go test ./...`, `go vet ./...`, `just build-tagged` (exit 0).

The only thing left uncommitted is the closeout paperwork: the acceptance
review's result in `ephemeral/devalue-projection/REVIEW.md` (findings reduced
to "None.") and the "Final review state" section appended to
`ephemeral/worklog/202609061434-issues-104-107.md`.

## The step

Commit those two tracked ephemeral files, and nothing else, as the branch
closeout. This belongs to milestone 4 (#107) — it is that milestone's worklog
requirement ("the worklog records the decisions and any friction"), not new
work and not a fifth milestone.

- Confirm `git status --porcelain` shows only those two files modified.
- Commit them together with a message naming the closeout and the branch's
  four issues. Do not amend b746649; do not push; do not open a PR.

## How it is demonstrated

Run, in order, and show the output:

    go test ./... && go vet ./... && just build-tagged
    git status --porcelain     # empty afterwards
    git log --oneline -1       # the closeout commit on this branch

## Done when

The working tree is clean, the closeout commit is on
`claude/devalue-projection` and unpushed, and the three gates pass on that
commit.

## Not this step

Do not touch code, tests, the brief, or `docs/`. Do not push, tag, or open a
PR. Do not re-run or re-open the acceptance review. After this commit the
branch is finished — stop.
