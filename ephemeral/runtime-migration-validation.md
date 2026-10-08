# Standalone runtime migration validation

The migration removes github.com/tylergannon/polytype/devalue and generates codecs importing the published github.com/tylergannon/devalue/v5 v5.0.0. Generator APIs remain at github.com/tylergannon/polytype/devalue/codegen. The authoritative scope is /Users/tyler/src/devalue/ephemeral/polytype-migration.md; its runtime removal without aliases and minor-release policy are deliberate decisions.

Generated recursive codecs and nested fixture modules use v5.0.0 without a runtime replace. Fixture tests explicitly import the runtime so go mod tidy retains the dependency needed by generated codecs and subprocess fixture checks. All affected nested modules pass go mod tidy -diff. The root JS devalue pin is retained because TestRecursiveDevalueJSInterop consumes it; the old plan's recorder-only statement was incorrect.

Validation completed:
- go test ./... against the published v5 module.
- just lint and just build-tagged.
- go generate ./... followed by JSONSCHEMA_NO_CHANGES=1 go generate ./....
- Regenerated recursive fixture codecs and gomarkdoc API index; go.mod drops Goja and runtime-only dependencies.
- TestRecursiveDevalueJSInterop runs Go-to-JavaScript-to-Go recursive typed-codec exchange with the pinned devalue JS package.
- ephemeral/runtime-consumer uses the current generator plus published v5.0.0, emits codecs importing the standalone runtime, checks exact wire [{"name":1,"values":2},"Ada",[3,4],1,2], then decodes a tree parsed by that standalone runtime. It prints standalone-runtime-wire-and-roundtrip=ok and upstream=5.9.4.

The scratch consumer uses a local polytype replace before this branch is released; its runtime has no replace. Publication is followed by a fresh no-replace CLI install and rerunning this consumer with the released polytype version.

The Astro docs site builds successfully, and all internal links resolve across 20 HTML pages. The API index now omits the removed runtime package.

The public Go proxy also builds and installs the polytype CLI from this branch at e77d2430c57b with GOWORK=off. The executable help and module build metadata are recorded in ephemeral/runtime-cli-install.txt. Final publication proof will use the release tag.

Independent review round 1 identified tidy-removable fixture dependencies and recurring pre-existing grammar formatting/modernization drift. Both were corrected, and the full Go suite, lint and tagged build pass with those changes.
