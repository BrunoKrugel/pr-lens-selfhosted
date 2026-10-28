# canvas-server

A private canvas store for [PR Lens](https://github.com/coldteadotai/pr-lens),
implementing version 1 of the
[canvas API contract](https://github.com/coldteadotai/pr-lens/blob/main/docs/canvas-api.md).

It answers the five routes the CLI needs (mint, fetch, push, rotate, delete),
the error envelope, and the rotate replay rules. Tokens are stored as SHA-256
hashes and compared in constant time. It draws nothing, so every push and fetch
answers `"tiles": []` — which the contract allows.

## Run

```bash
cp .env.example .env   # set PUBLIC_BASE_URL to the URL Cloudflare proxies here
docker compose up -d   # restart: unless-stopped, listens on 8080, published on host port 8585
```

The `-base-url` flag roots the `viewUrl`, `editUrl` and `embedUrl` the server hands out.

## Point the CLI at it

```bash
export PR_LENS_API_URL=https://lens.yourco.internal
# or pass --api on every canvas command
npx @coldtea/pr-lens-cli@latest canvas push .pr-lens/<drawing>/drawn.graph.json
```

Documents pushed this way never touch prlens.dev.

The canvas is theme-neutral: what you push is what it stores. For dark mode
everywhere, render the drawing dark before you push:

```bash
npx @coldtea/pr-lens-cli@latest render .pr-lens/graph.json --theme dark
```

## Notes

- The store is in memory: canvases are lost on restart. Swap `Server.canvases`
  for a durable store if you need persistence.
- The page at `/c/{id}` answers 404; a canvas web UI is out of scope here. The
  API contract says what it serves is the server's own business.
- The document validity check is shallow (`kind: "graph"`). A server that wants
  the full check the hosted app does can parse with `@coldtea/pr-lens-schema`.

## Web viewer

`/c/{id}` serves a dark-theme canvas page. The Go binary embeds a small JS
bundle (built from the official `@coldtea/pr-lens-renderer`, with a sha256
shim so it runs in the browser) that fetches the document from the API and
draws every view client-side. `/c/{id}.svg` serves the same page, so the
CLI's embed link works too.

Rebuild the bundle after touching `web/`:

```bash
cd web && npm install && npm run build   # outputs to internal/webui/dist
```

`go build` needs `internal/webui/dist/` to exist; the Dockerfile builds it
in a node stage.

## Layout

- `cmd/canvas-server` — the binary
- `internal/canvas` — the protocol: routes, error envelope, store, token minting
- `internal/canvas/server_test.go` — table of contract behaviors, via `httptest`

## Tests

```bash
go test -count=1 ./...
```
