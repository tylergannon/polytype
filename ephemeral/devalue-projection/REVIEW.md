# Material findings

- **REJECTED — "The milestone commit does not close #106."** The finding is
  false. `git show -s 426e51c` shows `Closes #106` in the commit body; the
  reviewer read only the subject line. GitHub reporting #106 open is expected,
  not evidence: the brief forbids pushing this branch, so no closing marker has
  reached GitHub. No commit was amended. See the worklog.

## Found instead, during this pass

- **Seven committed examples were stale after the #106 generator change**
  (enums, iota_global, ref_types, self_contained, stringer_enums,
  template_rendering, test_options). CI runs `go generate ./...` and requires a
  clean tree, so this would have failed there. Fixed by cherry-picking a0cc4af;
  `go generate ./...` now leaves the tree clean.
