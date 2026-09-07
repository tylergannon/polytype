# devalue goldens

`golden.json` records what the pinned JavaScript `devalue` (see the repository
root `package.json`) produces for each generated value; `go test` reads it and
never runs Node.

Regenerate with `npm ci` installed at the repository root:

```sh
go run ./devalue/testdata/record && node devalue/testdata/record/record.mjs
```
