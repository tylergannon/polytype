# Proof: #106 against committed examples (manager, 2026-09-06 15:50 CST)

Detached worktree at HEAD 426e51c. `go generate ./...` with the new
generator changed seven committed examples:
enums, iota_global, ref_types, self_contained, stringer_enums,
template_rendering, test_options (jsonschema_gen.go each). CI's clean-tree
check would fail until these are regenerated on the branch.

After regeneration, `go test ./examples/...` passes, including
examples/enums/roundtrip_test.go and examples/stringer_enums/codec_test.go,
which exercise real generated codecs.

Probe test added temporarily to examples/enums (real generated code, the
exact #106 scenario):
- json.Marshal of a struct holding a zero Priority: rejected,
  `polytype: "" is not a declared member of enum Priority`
- json.Unmarshal of "bogus" into Priority: rejected.
Probe removed; not committed.
