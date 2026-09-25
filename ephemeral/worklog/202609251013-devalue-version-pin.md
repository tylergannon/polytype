# Worklog: devalue upstream version pin and 5.9.2 → 6.0.2 delta map

Baseline: `go test ./...` passed on `3d70c98` before any change.

decision: the parity promise stays at devalue 5.9.2 in this change. Upgrading is
separate work; the delta map is `ephemeral/devalue-parity/5.9.2-to-6.0.2.md`.

decision: the pin is enforced in three places that one Go test ties together:
root `package.json` (exact version, the install source), `devalue.UpstreamVersion`
(the promise a polytype release makes, visible in godoc), and the `devalue`
field `record.mjs` now writes into `testdata/golden.json`. Without the golden
field, bumping `package.json` and the constant without re-recording would still
pass. `record.mjs` refuses to record from an installed devalue other than the pin.

decision: the recommended next parity target is 5.9.4, not 6.0.x. SvelteKit
2.70.3 depends on `devalue ^5.8.1` and 3.0.0-next.29 on `^5.9.4` (npm registry,
2026-09-25). 5.9.3 is where the upstream uneval DoS fixes landed (quadratic
repeated-string output, eager sparse allocation). 6.0.0 changes the `uneval`
replacer API, so matching it would break `UnevalWith` for skgo.

discovery: none of the newer releases changes flat `stringify` output on the 337
recorded cases; only `uneval` output moves (sparse_array in 5.9.3;
shared_reference and self_cycle in 6.0.0). Lone-surrogate escaping (6.0.0)
changes flat output too, but the corpus has no lone surrogates.

discovery: the Go port already rejects a non-string null-prototype key (upstream
started rejecting it in 5.9.3), and it boxes the `-2`/`-7` sentinels, which 6.0.1
rejects.

doc_bug: README and AGENTS said the devalue test "consults Node tooling when
present and skips otherwise"; the golden tests read `golden.json` and never run
Node. -> reworded while stating the exact pin.

friction: the recorder needs devalue in the root `node_modules`. The repo is wired
to `npm ci` (`package-lock.json`, CI), but pnpm/VitePlus is the house tool. For
verification I linked `node_modules/devalue` to the registry tarball (shasum
checked against the registry) instead of running npm. -> the npm → pnpm
migration of the root pins and CI remains open.

correction: the user asked, mid-task, how breaking this is for skgo before any
release. Downstream break analysis is now part of every parity bump; see the skgo
section of `ephemeral/devalue-parity/5.9.2-to-6.0.2.md`.

discovery: skgo's contract is byte parity with kit, and kit renders with the
devalue it resolves (`@sveltejs/kit@3.0.0-next.28` → devalue 5.9.4 in
`skgo/example/web/pnpm-lock.yaml`). Polytype at 5.9.2 is therefore already behind
kit for sparse arrays and repeated long strings/BigInts. The parity target is
kit's resolved devalue, not upstream's latest.

decision: 6.0.x waits until kit depends on `devalue ^6`, and then ships with an
additive replacer API. Changing `Replacer`/`UnevalWith` in place breaks skgo at
10 sites and polytype's v1 API.

doc_bug: skgo `devalue_wire_test.go` says kit's range "resolves to" 5.9.2; its
lockfile now resolves 5.9.4. -> skgo-side fix, not made here.

proof: skgo against this branch through a scratch go.work: the root-module
packages pass. `cmd/skgo` passes when its test binary is built with the scratch
GOWORK and run without it; build info shows polytype `(devel)`. The `example`
module compiles against the branch (`go test -run '^$'`). The only runtime Go
change in `devalue/` is the added constant; value.go changes only a comment.

friction: `GOWORK=<scratch> go test ./...` in skgo gave 3 false `cmd/skgo`
failures, because those tests run `go build` in temp projects and inherit
GOWORK. -> build the test binary with GOWORK set (`go test -c`) and run it
without GOWORK. Also, `go test ./...` from the skgo root does not reach the
`example` module; run it separately.

discovery: skgo's `example` module fails on its own baseline (polytype v1.1.0):
its frontend build is stale (adapter 16cb67886deb vs program 17f12e0497d6).
Its document tests need `pnpm add -D @skgo/sveltekit-adapter`, `go generate` and
a frontend rebuild in the skgo checkout before they can serve as downstream proof.
