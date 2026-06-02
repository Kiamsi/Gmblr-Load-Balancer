# below are described the requirements for the project, originally not written by me:

# 

# \# poker-lb

# 

# A Layer-7 reverse proxy for `gmblr-poker` backends. Routes by `room\\\_id`

# for session stickiness and drains cleanly on deploy.

# 

# \## What it does

# 

# \- \*\*Sticky routing\*\* — extracts `room\\\_id` from `/api/poker/rooms/<id>` paths and

# &#x20; consistently hashes it to a backend. The same room always goes to the same

# &#x20; server. If that server is removed, only its rooms are reshuffled.

# \- \*\*Round-robin fallback\*\* — requests without a `room\\\_id` (e.g. creating a new

# &#x20; room) are distributed across healthy backends.

# \- \*\*Active health checks\*\* — probes each backend's `/api/poker/health` every 5s.

# &#x20; 3 consecutive failures or a single 503 marks a backend unhealthy.

# \- \*\*Zero-downtime deploys\*\* — a two-step drain protocol lets you take a backend

# &#x20; offline without dropping live sessions.

# 

# \## What it is NOT

# 

# \- Not a general-purpose load balancer — poker paths only.

# \- Not a TLS terminator — Cloudflare/Caddy handles TLS upstream.

# \- Not a state store — stateless except for the in-memory consistent-hash ring.

# 

# \## Build

# 

# ```sh

# go build -o poker-lb ./cmd/lb

# ```

# 

# \## Run

# 

# ```sh

# LB\_ADMIN\_TOKEN=secret ./poker-lb -config config/lb.yaml

# ```

# 

# \## Configuration (`config/lb.yaml`)

# 

# | Key                        | Description                                        |

# | -------------------------- | -------------------------------------------------- |

# | `listen`                   | Proxy listen address (e.g. `:8080`)                |

# | `backends\\\[].addr`          | Backend `host:port`                                |

# | `health.path`              | Health check path                                  |

# | `health.interval\\\_s`        | Probe interval in seconds                          |

# | `health.fail\\\_threshold`    | Consecutive failures before marking unhealthy      |

# | `health.pass\\\_threshold`    | Consecutive successes before marking healthy again |

# | `stickiness.room\\\_id\\\_regex` | Regex with one capture group for the room ID       |

# | `admin.listen`             | Admin API listen address (loopback only)           |

# | `admin.auth\\\_token\\\_env`     | Name of env var holding the admin Bearer token     |

# 

# Config changes take effect on process restart.

# 

# \## Admin API

# 

# Runs on `127.0.0.1:9090` (loopback only). Requires:

# 

# ```

# Authorization: Bearer $LB\_ADMIN\_TOKEN

# ```

# 

# \### `GET /status`

# 

# Returns current pool state.

# 

# ```sh

# curl -H "Authorization: Bearer $LB\_ADMIN\_TOKEN" http://127.0.0.1:9090/status

# ```

# 

# ```json

# {

# &#x20; "ts": "2024-01-01T00:00:00Z",

# &#x20; "backends": \[

# &#x20;   { "addr": "10.0.0.10:3001", "status": "healthy", "updated\_at": "..." },

# &#x20;   {

# &#x20;     "addr": "10.0.0.11:3001",

# &#x20;     "status": "draining",

# &#x20;     "last\_error": "503 from health endpoint",

# &#x20;     "updated\_at": "..."

# &#x20;   }

# &#x20; ]

# }

# ```

# 

# \### `POST /drain?backend=<addr>`

# 

# Marks a backend draining. Existing sticky sessions still reach it; no new

# room assignments.

# 

# ```sh

# curl -X POST -H "Authorization: Bearer $LB\_ADMIN\_TOKEN" \\

# &#x20; "http://127.0.0.1:9090/drain?backend=10.0.0.10:3001"

# ```

# 

# \## Deploy flow (zero-downtime)

# 

# 1\. Mark the backend draining in the LB:

# &#x20;  ```sh

# &#x20;  curl -X POST -H "Authorization: Bearer $LB\_ADMIN\_TOKEN" \\

# &#x20;    "http://127.0.0.1:9090/drain?backend=10.0.0.10:3001"

# &#x20;  ```

# 2\. Tell the backend to stop accepting new work:

# &#x20;  ```sh

# &#x20;  curl -X POST http://10.0.0.10:3001/api/poker/drain

# &#x20;  ```

# 3\. The LB health prober sees the 503 within \~5s and removes the backend from

# &#x20;  the ring automatically.

# 4\. Wait \~30s for in-flight requests to finish, then restart the backend.

# 5\. The LB re-adds it after two consecutive healthy health checks (\~10s).

# 

# \## Observability

# 

# One JSON log line per request to stdout:

# 

# ```json

# {

# &#x20; "ts": "2024-01-01T00:00:00Z",

# &#x20; "src\_ip": "1.2.3.4",

# &#x20; "method": "POST",

# &#x20; "path": "/api/poker/rooms/abc-123/action",

# &#x20; "room\_id": "abc-123",

# &#x20; "backend": "10.0.0.10:3001",

# &#x20; "status": 200,

# &#x20; "duration\_ms": 12

# }

# ```

# 

# Health probe failures also log to stdout as JSON with `"event": "health\\\_probe\\\_failed"`.

# 

# \## Failure modes

# 

# | Failure                | Behaviour                                                                                                                        |

# | ---------------------- | -------------------------------------------------------------------------------------------------------------------------------- |

# | One backend dies       | Marked unhealthy in ≤15s, removed from ring. Its rooms are rehomed to a surviving backend, which rehydrates from the data store. |

# | All backends dead      | LB returns 503 to all clients.                                                                                                   |

# | Backend slow but alive | Health passes; request latency spikes. 60s max lifetime enforced, then 504.                                                      |

# | LB itself dies         | Single point of failure (MVP).                                                                                                   |

# 

# \## Retry policy

# 

# \- Connect refused / TCP reset before bytes sent → retry once on the \*\*same\*\* backend.

# \- If retry also fails → mark backend unhealthy, return 502.

# \- \*\*Never retry on a different backend\*\* — that would split a room's state.

# \- 5xx from backend → passed through to the client unchanged.

# \- Backend dies mid-response → client receives a truncated response.

