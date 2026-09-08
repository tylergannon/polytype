# Union discriminator inflection

decision: discriminator inflection is a per-union setting alongside the discriminator property; the default remains the concrete Go type name in PascalCase for wire compatibility.
correction: user requested named snake, camel, and pascal inflections plus arbitrary func(string) string support in Go configuration, and requested a new version after merge.
decision: build-tagged source accepts only polytype.Snake, polytype.Camel, and polytype.Pascal because source scanning cannot safely execute arbitrary callbacks; executable codegen configuration resolves arbitrary callbacks once before any backend writes output.
decision: resolved discriminator values live on the inferred union and feed JSON Schema, generated JSON and YAML codecs, typegrammar, TypeScript, and devalue from one shared mapping.
friction: adding resolved values to generated-helper identity churned unchanged default golden names; values are already globally fixed per interface, so helper identity remains based on interface, property, variants, and pointer shape.
proof: focused static and programmatic generation tests pass; just build-tagged, two consecutive go generate ./... runs with no new diff, go vet ./..., and just lint pass.
decision: the next release is v1.0.0-rc.12 because main is one unreleased additive commit beyond v1.0.0-rc.11 and this is another additive pre-1.0 API feature.
