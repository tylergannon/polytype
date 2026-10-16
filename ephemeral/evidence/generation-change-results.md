# Actual generator consumer observations

2026-10-16, isolated worktree based on Polytype v1.9.0 (0fc3aad).

Copied codegen/testdata/programmatic under ephemeral/, changed its local module
replacement to this checkout, and changed the transport command from codegen.Gen
to codegen.GenResult, printing its bool and error. Configuration retained Envelope,
the kind discriminator with polytype.Snake, TypeScript/barrel and devalue outputs.

Observed from separate go run ./cmd/transport processes:

- First publication: generated=true error=<nil>.
- Second and third runs: generated=false error=<nil>.
- TypeScript mtime before and after equal run: 1792179412388197023 ns.
- Generated TypeScript contained literal "kind": "http_event_created".
- After deleting generated/typescript/types.ts: generated=true error=<nil>.
- go test ./generated/codec compiled the emitted Go package successfully.
- A consumer called generated StringifyEnvelope then ParseEnvelope for authored
  Envelope{Message:"hello", Count:2, State:StateReady,
  Event:HTTPEventCreated{ID:"evt-1"}}, comparing against that value:
  roundtrip equal=true message=hello count=2 event=model.HTTPEventCreated{ID:"evt-1"}.

The disposable consumer copy was removed after observation. Regular
codegen/result_test.go retains regression coverage for ordinary and retained
requests, partial publication followed by error, failed analysis, actual byte
repair, missing output, schema pruning and equal mtimes.

These are local generator/tooling observations. SKGO/Gimbal integration,
application replacement, native hosting, timings and release remain unproved.
