# Next step

Milestone 1 (#106) is done, committed and reviewed (a3afd28). Milestone 2
(#104) has not started. The brief splits #104 into two independent halves —
moving the grammar package out of `internal/`, and the new exported `grammar`
lowering package. This step is the first half only. It is mechanical, it
changes no behavior, and the full gate suite demonstrates it.

## Step: export `typegrammar` (first half of #104)

1. `git mv internal/typegrammar typegrammar`. Keep the package name
   `typegrammar`; only the import path changes to
   `github.com/tylergannon/polytype/typegrammar`.

2. Rewrite the import path in the four Go files that reference it —
   `internal/typescript/generate.go`, `internal/typescript/generate_test.go`,
   `internal/builder/typegrammar.go`,
   `internal/builder/typegrammar_adapter_test.go` — plus
   `typegrammar/grammar_test.go` and `tests/typescript/projection/generate/main.go`.
   (`tests/typescript/` is deleted in milestone 4, but it must compile now.)
   Do not touch the `ephemeral/` files that mention the old path; they are
   historical records.

3. Add one paragraph to the package doc in `typegrammar/grammar.go`: new node
   kinds may be added in minor versions, so consumers must not treat type
   switches over the node types as exhaustive.

4. Commit on this branch, message naming `#104` and stating that this is the
   package move only (`Closes #104` waits for the `grammar` package). Do not
   push, do not open a PR. Update
   `ephemeral/worklog/202609061434-issues-104-107.md` with decisions,
   corrections and friction only.

Nothing else from #104 is in scope: no `grammar` package, no `Load`, no
`Lower`, no root bridge, no scanner change, no new tests. Nothing from #105
or #107.

## Demonstrated by

`grep -rn "internal/typegrammar" --exclude-dir=.git --exclude-dir=ephemeral .`
returning nothing, then the gates: `go test ./...`, `go vet ./...`,
`just build-tagged`, and `git log -1` showing the commit.
