# v1.0.0-rc.13 consumer proof

release: Tagged and published v1.0.0-rc.13 from merged main c8cca3951509f649d8699af42998a04fa6081b60 after PR #120 passed GitHub Go and website checks.
proof: A fresh module at /tmp/polytype-rc13-consumer.rAlhjQ installed github.com/tylergannon/polytype v1.0.0-rc.13 with a Go tool directive and no local replace. Generation emitted schema, validation, JSON union/enum codecs, and structural TypeScript.
proof: Consumer Go tests encoded, validated, decoded, and compared an explicit Optional/Nullable model with an empty slice, snake-inflected sealed union, and string-mode enum; invalid enum and discriminator values were rejected.
proof: TypeScript 6.0.3 compiled a consumer-authored Envelope using the generated type-only barrel. JSONSCHEMA_NO_CHANGES=1 regeneration preserved identical artifact hashes; Go tests and TypeScript compilation passed again.
discovery: Generated code contains no YAML methods or imports, but `go list -m all` still includes gopkg.in/yaml.v3 through polytype's testify/assert test dependency. Correct the broader docs phrase "takes no YAML dependency" before stable and record the distinction in #62.
change: Corrected the README, v1 spec, and validation guide to distinguish generated consumer imports from the repository's transitive test dependency. Updated #62 acceptance to require the same precise claim.
proof: After the documentation correction, `go test ./...`, website `npm run check`, internal link checking, the stale-claim search, and `git diff --check` passed.
