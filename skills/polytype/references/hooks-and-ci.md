# Keeping Generated Output in Sync: Git Hooks and CI

Read this when wiring `go generate` into pre-commit hooks or CI so generated
schemas, Go code, and requested TypeScript declarations cannot drift from the Go
types they were generated from.

Two viable strategies — pick one, don't mix them in the same hook:

- **Auto-stage** (convenient): the hook regenerates and stages the results.
  Commits always carry fresh schemas; contributors never think about it.
- **Fail-on-drift** (strict): the hook fails if regeneration would change
  anything, and the author reruns `go generate` themselves. Nothing mutates
  the commit behind the author's back; pairs exactly with the CI check.

The tool supports the strict mode natively: `polytype gen -no-changes`
(or env `JSONSCHEMA_NO_CHANGES=1`) fails — writing nothing — when regeneration
would change any schema or requested TypeScript artifact. The env var form is
the one to use in hooks, because
it flows through `go generate` to every `//go:generate go tool polytype`
directive without editing them. Generated Go output can still update when those
artifacts are unchanged, so pair the command with a clean status check.

## lefthook

Install once: `go get -tool github.com/evilmartians/lefthook@latest && go tool lefthook install`
(or `brew install lefthook && lefthook install`).

### Option A — auto-stage regenerated files

```yaml
# lefthook.yml
pre-commit:
  commands:
    polytype:
      glob: "*.go"          # skip the hook entirely when no Go files are staged
      run: |
        go generate ./...
        git add '*jsonschema_gen.go' '*polytype_gen.go' '*jsonschema/*' 2>/dev/null || true
        git add 'web/src/generated/*' 2>/dev/null || true # use your configured TypeScript directory
```

Notes:
- Git pathspec `*` crosses directory separators, so `'*jsonschema/*'` stages
  the generated `.json` **and** `.json.sum` files at any depth, root included.
- The explicit `git add` matters. lefthook's `stage_fixed: true` only re-stages
  files that were already staged and matched the glob — freshly generated
  `.json` files wouldn't qualify, so stage them explicitly.
- Replace `web/src/generated` with the `--typescript` directory from the
  project's generation directive, or omit that line when TypeScript output is
  disabled.
- Keep this command out of any `parallel: true` group that also runs linters
  over the same files, or the linter may see the pre-regeneration state.

### Option B — fail the commit on drift

```yaml
# lefthook.yml
pre-commit:
  commands:
    polytype-check:
      glob: "*.go"
      run: JSONSCHEMA_NO_CHANGES=1 go generate ./... && test -z "$(git status --porcelain)"
```

On failure the tool names changed schema types and/or requested TypeScript paths.
The fix is always: `go generate ./... && git add -A`, then commit again.

Schema and requested TypeScript drift are detected without writing those
destinations. Keep the command sequential with tools that read generated Go
output because that file can still be refreshed.

Lefthook also has a hook-level `fail_on_changes` option (`never`/`always`/`ci`/
`non-ci`) that fails when *any* git-tracked file was modified by the hook.
`fail_on_changes: ci` combined with Option A gives auto-staging locally and a
strict failure in CI from one config.

## Plain git hook (no lefthook)

```bash
#!/bin/sh
# .git/hooks/pre-commit
JSONSCHEMA_NO_CHANGES=1 go generate ./... && test -z "$(git status --porcelain)" || {
  echo "Generated output is out of date. Run: go generate ./... then re-stage." >&2
  exit 1
}
```

## CI check

Run the same drift check in CI regardless of which local strategy you chose —
hooks can be skipped (`--no-verify`), CI can't:

```yaml
# .github/workflows/ci.yml (excerpt)
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
- name: Check generated output is current
  run: JSONSCHEMA_NO_CHANGES=1 go generate ./... && test -z "$(git status --porcelain)"
```

A generic fallback that also catches non-schema generators and untracked files:
`go generate ./... && test -z "$(git status --porcelain)"`.
