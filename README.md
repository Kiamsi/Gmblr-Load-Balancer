below are described the requirements for the project, originally not written by me:

# poker-lb

A Layer-7 reverse proxy for `gmblr-poker` backends. Routes by `room_id`
for session stickiness and drains cleanly on deploy.

## What it does

- **Sticky routing** — extracts `room_id` from `/api/poker/rooms/<id>` paths and
  consistently hashes it to a backend. The same room always goes to the same
  server. If that server is removed, only its rooms are reshuffled.
- **Round-robin fallback** — requests without a `room_id` (e.g. creating a new
  room) are distributed across healthy backends.
- **Active health checks** — probes each backend's `/api/poker/health` every 5s.
  3 consecutive failures or a single 503 marks a backend unhealthy.
- **Zero-downtime deploys** — a two-step drain protocol lets you take a backend
  offline without dropping live sessions.

## What it is NOT

- Not a general-purpose load balancer — poker paths only.
- Not a TLS terminator — Cloudflare/Caddy handles TLS upstream.
- Not a state store — stateless except for the in-memory consistent-hash ring.

## Build

```sh
go build -o poker-lb ./cmd/lb
```

## Run

```sh
LB_ADMIN_TOKEN=secret ./poker-lb -config config/lb.yaml
```

## Configuration (`config/lb.yaml`)

| Key                        | Description                                        |
| -------------------------- | -------------------------------------------------- |
| `listen`                   | Proxy listen address (e.g. `:8080`)                |
| `backends[].addr`          | Backend `host:port`                                |
| `health.path`              | Health check path                                  |
| `health.interval_s`        | Probe interval in seconds                          |
| `health.fail_threshold`    | Consecutive failures before marking unhealthy      |
| `health.pass_threshold`    | Consecutive successes before marking healthy again |
| `stickiness.room_id_regex` | Regex with one capture group for the room ID       |
| `admin.listen`             | Admin API listen address (loopback only)           |
| `admin.auth_token_env`     | Name of env var holding the admin Bearer token     |

Config changes take effect on process restart.

## Admin API

Runs on `127.0.0.1:9090` (loopback only). Requires:

```
Authorization: Bearer $LB_ADMIN_TOKEN
```

### `GET /status`

Returns current pool state.

```sh
curl -H "Authorization: Bearer $LB_ADMIN_TOKEN" http://127.0.0.1:9090/status
```

```json
{
  "ts": "2024-01-01T00:00:00Z",
  "backends": [
    { "addr": "10.0.0.10:3001", "status": "healthy", "updated_at": "..." },
    {
      "addr": "10.0.0.11:3001",
      "status": "draining",
      "last_error": "503 from health endpoint",
      "updated_at": "..."
    }
  ]
}
```

### `POST /drain?backend=<addr>`

Marks a backend draining. Existing sticky sessions still reach it; no new
room assignments.

```sh
curl -X POST -H "Authorization: Bearer $LB_ADMIN_TOKEN" \
  "http://127.0.0.1:9090/drain?backend=10.0.0.10:3001"
```

## Deploy flow (zero-downtime)

1. Mark the backend draining in the LB:
   ```sh
   curl -X POST -H "Authorization: Bearer $LB_ADMIN_TOKEN" \
     "http://127.0.0.1:9090/drain?backend=10.0.0.10:3001"
   ```
2. Tell the backend to stop accepting new work:
   ```sh
   curl -X POST http://10.0.0.10:3001/api/poker/drain
   ```
3. The LB health prober sees the 503 within ~5s and removes the backend from
   the ring automatically.
4. Wait ~30s for in-flight requests to finish, then restart the backend.
5. The LB re-adds it after two consecutive healthy health checks (~10s).

## Observability

One JSON log line per request to stdout:

```json
{
  "ts": "2024-01-01T00:00:00Z",
  "src_ip": "1.2.3.4",
  "method": "POST",
  "path": "/api/poker/rooms/abc-123/action",
  "room_id": "abc-123",
  "backend": "10.0.0.10:3001",
  "status": 200,
  "duration_ms": 12
}
```

Health probe failures also log to stdout as JSON with `"event": "health_probe_failed"`.

## Failure modes

| Failure                | Behaviour                                                                                                                        |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| One backend dies       | Marked unhealthy in ≤15s, removed from ring. Its rooms are rehomed to a surviving backend, which rehydrates from the data store. |
| All backends dead      | LB returns 503 to all clients.                                                                                                   |
| Backend slow but alive | Health passes; request latency spikes. 60s max lifetime enforced, then 504.                                                      |
| LB itself dies         | Single point of failure (MVP).                                                                                                   |

## Retry policy

- Connect refused / TCP reset before bytes sent → retry once on the **same** backend.
- If retry also fails → mark backend unhealthy, return 502.
- **Never retry on a different backend** — that would split a room's state.
- 5xx from backend → passed through to the client unchanged.
- Backend dies mid-response → client receives a truncated response.
