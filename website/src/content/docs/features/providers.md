---
title: Provider-rendered schemas
description: Supply field schemas at runtime when static generation is not enough.
---

Provider hooks replace selected field schemas with `json.Marshaler` values from
functions or methods:

```go
func BoolSchema(_ bool) json.Marshaler {
    return json.RawMessage(`{"type":"boolean"}`)
}

var _ = polytype.Declare(Config.Schema).
    Function(polytype.Field[Config, bool]("Enabled"), BoolSchema).
    RenderProviders()
```

Available provider chain methods are:

- `.Function(field, fn)` for a package function;
- `.Accessor(field, method)` for a receiver method taking only the receiver;
- `.Method(field, method)` for a receiver method that also accepts the field value;
- `.RenderProviders()` to generate `RenderedSchema()` and execute providers at runtime.

Migration: `NewJSONSchemaMethod(Config.Schema, WithFunction(Config{}.Enabled,
BoolSchema), WithRenderProviders())` is now `Declare(Config.Schema).Function(
polytype.Field[Config, bool]("Enabled"), BoolSchema).RenderProviders()`. The legacy
`NewJSONSchemaMethod`/`NewJSONSchemaFunc` with `WithFunction`,
`WithStructAccessorMethod`, `WithStructFunctionMethod`, and
`WithRenderProviders` remain supported and source-compatible; each carries a
`Deprecated:` godoc comment naming its fluent equivalent.

Provider implementations must be available in normal builds because
`RenderedSchema()` calls them at runtime. A rendered type does not receive
`ValidateJSON()` because its schema depends on runtime values.

See [`examples/providers_rendering`](https://github.com/tylergannon/polytype/tree/main/examples/providers_rendering)
for all three provider shapes and a runtime test.
