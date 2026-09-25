# devalue goldens

`golden.json` records what the pinned JavaScript `devalue` produces from both
`stringify` and `uneval` for each generated value, together with the devalue
version that produced it. `go test` reads it and never runs Node.

## The pin

Each polytype release is feature-equivalent to exactly one devalue release,
named in three places that `TestUpstreamVersion` requires to agree:

- `devalue.UpstreamVersion` in `devalue/version.go`, the promise the Go API
  publishes;
- `devDependencies.devalue` in the repository root `package.json`, an exact
  version and the one the recorder installs;
- the `devalue` field of `golden.json`, written by the recorder from the
  installed package.

`record.mjs` refuses to run against any other installed version.

## Regenerate

With the root packages installed (`npm ci` at the repository root):

```sh
go run ./devalue/testdata/record && node devalue/testdata/record/record.mjs
```

## Moving to a newer devalue

1. Pick the target from what consumers run, not from upstream's latest:
   SvelteKit renders with the devalue its own dependency range resolves, and a
   Go server mirroring SvelteKit needs output byte-equal to that.
   Read every upstream change between the current pin and the target release:
   the GitHub release notes, the published source diff, and the
   `test/index.test.js` expectations.
2. Bump the root `package.json` pin and its lockfile, and `UpstreamVersion`.
3. Re-record `golden.json`. Recorded cases that change show which outputs
   moved. The corpus does not cover every changed behavior, so extend
   `main.go` with any value shapes the release changed that it does not
   yet generate.
4. Port the changes, re-port the `uneval_test.go` expectations from the new
   release's tests, and update the version named in the docs.
5. Before releasing, run each downstream consumer's tests (skgo at minimum)
   against the branch through a `go.work` that uses both checkouts. Keep the
   exported API compatible: when upstream changes an API such as the
   `uneval` replacer, add the new form alongside `Replacer`/`UnevalWith`
   rather than changing them.
