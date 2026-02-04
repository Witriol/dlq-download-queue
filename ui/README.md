# DLQ UI

SvelteKit UI for the DLQ HTTP API.

## Setup

```bash
cd ui
npm install
npm run dev
```

## Configuration

The UI proxies requests to the DLQ API via server routes.

- `DLQ_API` or `DLQ_API_BASE` (fallback `http://127.0.0.1:8099`)

Example:

```bash
DLQ_API=http://127.0.0.1:8099 npm run dev
```

## Authelia (later)

- Put Authelia in front of the SvelteKit server and protect `/` + `/api/*`.
- In `src/hooks.server.ts`, trust Authelia headers like `Remote-User` and `Remote-Groups`.
- Gate UI routes + server proxy routes with simple role checks before forwarding to DLQ.
