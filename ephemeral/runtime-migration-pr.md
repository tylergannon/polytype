Generated devalue codecs now import `github.com/tylergannon/devalue/v5` at published v5.0.0, giving consumers the standalone runtime's value model. The duplicate Go runtime, its recorder and migrated notes are removed; `github.com/tylergannon/polytype/devalue/codegen` stays in polytype.

Migration: replace runtime imports of `github.com/tylergannon/polytype/devalue` with `github.com/tylergannon/devalue/v5`, then regenerate codecs. Removing the old runtime without aliases is the explicitly agreed minor-release migration. Runtime and codegen documentation, the website API index, nested fixture dependencies and fixture source tracking now follow the separate module. The pinned JavaScript devalue dependency remains because the generated-codec interoperability test uses it.

Validation:
- `go test ./...`, `just lint`, and `just build-tagged` pass.
- Generation followed by `JSONSCHEMA_NO_CHANGES=1 go generate ./...` passes.
- The recursive Go→JavaScript→Go typed-codec exchange passes against the pinned upstream JS package.
- A compiling consumer generates codecs importing published v5.0.0, checks exact expected wire bytes, and decodes a tree parsed by the standalone runtime. [Consumer source and result](https://github.com/tylergannon/polytype/tree/codex/devalue-runtime-migration/ephemeral/runtime-consumer) are included in this branch.
- The Astro website builds, and all internal links resolve across its 20 HTML pages.
