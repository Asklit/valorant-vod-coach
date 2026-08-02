# Web Product Architecture

The web client is a React 19 and TypeScript application built with Vite. It is a product UI, not an infrastructure dashboard: player workflows and operator workflows have separate routes and data ownership.

## Routes

| Route | Purpose |
| --- | --- |
| `/dashboard` | Current review, coaching focus, and next action |
| `/library` | Search, upload, edit, and delete tenant-owned VODs |
| `/review?vod=<label>` | Video, workflow controls, evidence, and guided assessment |
| `/reports` | Coaching plan, practice drills, report history, and corrections |
| `/admin` | Readiness, Prometheus metrics, Loki logs, accounts, and service consoles |

Routes use the History API and the Go server provides an SPA fallback. The selected VOD is encoded in the review URL so refreshes and shared local links preserve context. Unknown routes safely resolve to Dashboard.

## Source Boundaries

```text
src/
  app/                    navigation and application-level composition
  pages/
    auth/                 authentication and installation bootstrap
    admin/                operator-only operations console
  shared/                 HTTP client and framework-independent helpers
  App.tsx                 authenticated product workflow controller
```

Page features fetch their own private data when that data has no cross-page product use. For example, Operations owns telemetry refresh and filters; `App` does not issue admin requests while a player is reviewing a match. Cross-page match state remains in the application controller until its write workflows justify a dedicated feature store.

## Security Contract

- Browser authentication uses an `HttpOnly`, `Secure` for HTTPS, `SameSite=Lax` session cookie.
- State-changing requests carry the session CSRF token in `X-CSRF-Token`.
- The frontend never receives password hashes, storage credentials, internal Prometheus/Loki addresses, or unrestricted PromQL/LogQL access.
- `/admin` is hidden for normal users and the API independently enforces the administrator role.
- Telemetry accepts only fixed windows (`1h`, `6h`, `24h`, `7d`) and runs server-owned queries.
- VOD, report, correction, assessment, video, and artifact reads are owner-scoped by the API.

## Operations Data

`GET /api/admin/telemetry?window=6h` concurrently reads a bounded set of Prometheus series and up to 200 recent Loki entries. Backend failures are returned as partial `errors` instead of hiding healthy data. The page falls back to the API request ring buffer when centralized logs are unavailable.

External console links are public browser URLs from server configuration. Internal collector addresses remain server-side. Grafana is the main deep-diagnostic UI; the embedded Operations page covers daily readiness, metrics, logs, users, and trace identifiers.
