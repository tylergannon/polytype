# Documentation website

The public Starlight site for `polytype`.

```bash
npm ci
npm run dev
npm run check
```

`npm run check` performs the production build, regenerates the Go API reference,
and fails on broken internal links or fragments.

The GitHub Pages workflow deploys website changes and reruns whenever a root Go
file changes, keeping the generated API reference aligned with the public API.
