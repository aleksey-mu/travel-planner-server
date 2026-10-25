# Phase 1 — Basic cloud-storage sync (self-hosted TravelPlan server)

> **Superseded for current product.** Tenancy + `/sync/quota` shape and multi-user work live in [plan-multi-user.md](./plan-multi-user.md) / [plan-multi-user-impl.md](./plan-multi-user-impl.md) and `travel-plans/docs/self-hosted-server.md`. This file is historical Phase 1 build notes only — do not treat `{usage,limit}` or single-owner forever as current contracts.

Greenfield Go server that behaves as a **cloud storage equivalent** of the Google Drive / folder-layout sync path. Sharing, live-edit, and analytics are out of scope.

**API contract (source of truth):**  
[`travel-plans/docs/self-hosted-server.md`](https://github.com/aleksey-mu/travel-plans/blob/main/docs/self-hosted-server.md)  
Local mirror: `D:\Projects\!0TEST_ANYTHING_HERE\travel-plans\docs\self-hosted-server.md`

**Client already wired:** `SelfHostedAuthService` + `SelfHostedFolderClient` + `SelfHostedSyncAdapter` (see §10).

**Primary operator story:** `docker compose up -d` → set server URL in the TravelPlan app → login.

---

## 1. Goals / non-goals

### Goals

- **Single-owner vault** REST JSON API under `/api/v1` matching the client’s SelfHosted adapter (one operator account; not family multi-tenant).
- Per-user filesystem layout kept for future-proofing (`users/{userId}/`), but Phase 1 product = **one owner**.
- JWT Bearer auth (HS256); bcrypt password hashes; SQLite for accounts only.
- CORS for Angular web origins; Docker + Compose from day one.
- Single binary, pure-Go build (`CGO_ENABLED=0`), easy cross-compile.
- Acceptance tests via `curl` / `httptest` so another agent can implement without guessing.

### Non-goals (later phases)

- Self-serve multi-user / household accounts / email invite or password-reset mail.
- Sharing / collaborative edit / presence.
- Live sync / WebSockets / SSE.
- Usage analytics / admin dashboard beyond `create-user` / `set-password` CLI.
- Postgres / Redis / object storage (S3).
- Refresh tokens / OAuth2 / OIDC federation.
- Rate limiting / CAPTCHA (note in security; implement later).
- TLS termination in-process (recommend reverse proxy: Caddy/nginx).
- Backward compatibility with any prior server (greenfield).
- PWA / service worker / Capacitor.

---

## 2. Architecture

### Module

```
module github.com/aleksey-mu/travel-planner-server
go 1.26   # pin go.mod to current stable (1.26.x as of 2026-07); bump patch freely
```

Remote: `https://github.com/aleksey-mu/travel-planner-server.git`

### Package layout

```
travel-planner-server/
├── cmd/
│   └── server/
│       └── main.go              # parse flags/subcommands, load config, run HTTP or create-user
├── internal/
│   ├── config/
│   │   └── config.go            # env (+ optional .env), validation
│   ├── auth/
│   │   ├── password.go          # bcrypt cost 12
│   │   ├── jwt.go               # issue/parse HS256; reject alg≠HS256
│   │   ├── users.go             # User store (SQLite)
│   │   └── middleware.go        # Bearer → context userID
│   ├── syncstore/
│   │   ├── store.go             # per-user FS root ops
│   │   ├── paths.go             # safe join, allowlists, traversal checks
│   │   └── quota.go             # usage walk + limit
│   ├── httpapi/
│   │   ├── router.go            # net/http ServeMux registration
│   │   ├── middleware.go        # request ID, recover, CORS, logging
│   │   ├── errors.go            # JSON { "error": { "code", "message" } }
│   │   ├── auth_handlers.go     # register, login, me, health
│   │   └── sync_handlers.go     # bootstrap … files
│   └── version/
│       └── version.go           # injected at build (-ldflags) or const "1.0.0"
├── docs/
│   └── plan-basic-sync.md       # this file
├── Dockerfile
├── docker-compose.yml
├── .env.example
├── .dockerignore
├── go.mod
├── go.sum
└── README.md
```

### Why stdlib `net/http` (not Gin/Echo/Chi)

Go 1.22+ ServeMux supports method+path patterns and wildcards (`GET /sync/files/{fileId...}`, `r.PathValue("fileId")`). For a small REST surface (~15 routes), a router framework adds dependency surface without clear benefit. Introduce Chi/etc. only if middleware composition becomes painful.

### Request flow

```
Client (Angular)
  → CORS preflight (OPTIONS) / Bearer JWT
  → httpapi middleware (slog, recover, CORS, auth)
  → handler
  → auth.Users (SQLite) | syncstore (filesystem under DATA_DIR/users/{userId}/)
```

### Dependencies (minimal)

| Package | Role |
|---------|------|
| `modernc.org/sqlite` | Pure-Go SQLite (accounts) — prefer over `mattn/go-sqlite3` for `CGO_ENABLED=0` |
| `github.com/golang-jwt/jwt/v5` | HS256 access tokens |
| `golang.org/x/crypto/bcrypt` | Password hashing |
| Optional: `github.com/joho/godotenv` | Load `.env` in Compose/dev only (fail soft if missing) |

Do **not** add ORMs (GORM) for a single `users` table.

---

## 3. Data layout on disk + SQLite schema

### Environment / roots

| Var | Meaning |
|-----|---------|
| `DATA_DIR` | Absolute path; all durable state lives here |

```
{DATA_DIR}/
├── server.db                 # SQLite (users only)
└── users/
    └── {userId}/             # opaque UUID string (also used as rootId)
        ├── manifest.json
        ├── travels/
        │   └── {travelId}.json
        ├── files/
        │   └── {travelId}/
        │       └── {itemId}/
        │           └── {fileName}
        ├── user-data/
        │   ├── user-prefs.json
        │   ├── item-types.json
        │   └── checklist-templates.json
        └── meta/               # optional server-managed; unused in Phase 1 handlers
```

`POST /sync/bootstrap` creates this tree (empty dirs + no files required except dirs). Idempotent.

### Contract tightenings vs API sketch (breaking OK; document + align client if needed)

| Topic | API sketch (`self-hosted-server.md`) | **Implement this** | Rationale |
|-------|--------------------------------------|--------------------|-----------|
| User-data presets file | `custom-item-presets.json` | **`item-types.json`** | Client constant `USER_DATA_ITEM_TYPES_FILE_NAME` |
| Allowed user-data names | three names including custom-item-presets | `user-prefs.json`, `item-types.json`, `checklist-templates.json` | Match client |
| Attachment `fileId` | path under `files/` e.g. `files/{travel}/{item}/{name}` | Same | Client stores Drive-like path ids |
| `GET/DELETE /sync/files/{fileId}` | `{fileId}` is path under `files/` | Accept **both** `files/...` and bare `{travel}/{item}/{name}` after `/sync/files/` | Client currently calls `/sync/files/files/...` (see §4.3) |

Update `self-hosted-server.md` in the Angular repo when implementing (same PR / follow-up) so sketch matches reality.

### Empty defaults

| Resource | Missing behavior |
|----------|------------------|
| `GET /sync/manifest` | `200` with `{ "schemaVersion": 1, "updatedAt": "<RFC3339>", "travels": [] }` — do **not** 404 |
| `GET /sync/travels/{id}` | `404` `NOT_FOUND` |
| `GET /sync/user-data/{file}` | `404` if file absent |
| `GET /sync/files/...` | `404` if blob absent |

Server does **not** validate full `ITravel` / prefs schema beyond “valid JSON object” for JSON PUTs (optional: reject non-object top-level). Treat body as opaque JSON bytes for storage.

### SQLite schema (`server.db`)

```sql
CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY NOT NULL,  -- UUID v4
  email         TEXT NOT NULL COLLATE NOCASE UNIQUE,
  password_hash TEXT NOT NULL,              -- bcrypt
  display_name  TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,              -- RFC3339 UTC
  updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
```

Open with WAL + foreign keys on (even if unused):

```
_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)
```

(Exact DSN form per `modernc.org/sqlite` docs.)

No sync metadata tables in Phase 1 — filesystem is source of truth for blobs/docs.

### Quota

- Default limit: `10 GiB` per user (`10737418240`), overridable via `DEFAULT_QUOTA_BYTES`.
- Default limit: `10240` MiB per user (`VAULT_QUOTA_MB`).
- Max single upload: `50` MiB (`MAX_UPLOAD_MB`).
- `usage`: recursive byte sum of `{DATA_DIR}/users/{userId}/` (follow regular files only; ignore symlinks or reject symlink creation).
- Enforce on `PUT` travel / user-data / files: configured quota → `413` `QUOTA_EXCEEDED`; host volume shortfall → `507` `DISK_FULL`.
- `GET /sync/quota` → `{ "usage", "limit" }` with effective `limit = min(configured, usage+diskFree)`.

---

## 4. Endpoint checklist (1:1 with API + client)

Base URL: `{origin}/api/v1`  
All JSON unless noted. Protected routes require `Authorization: Bearer <accessToken>` except health (+ OPTIONS).

### 4.1 Auth & health

| Method | Path | Auth | Status | Request | Response | Notes |
|--------|------|------|--------|---------|----------|-------|
| `GET` | `/health` | no | 200 | — | `{ "status": "ok", "version": "1.0.0" }` | Client checks `status === "ok"` |
| `POST` | `/auth/register` | no* | 201 | `{ email, password, displayName? }` | `{ "userId", "email" }` | Disable when `ALLOW_REGISTER=false` → `403` `REGISTER_DISABLED` |
| `POST` | `/auth/login` | no | 200 | `{ email, password }` | `{ accessToken, expiresIn, user: { id, email, displayName } }` | Client field names exact |
| `GET` | `/me` | yes | 200 | — | `{ "id", "email", "displayName" }` | Client only requires `id` for test |

\*Register public only if enabled.

**Login field mapping (client):**

```ts
// SelfHostedAuthService.signInWithCredentials
POST {base}/auth/login
body: { email: config.username, password }
reads: accessToken, expiresIn, user.id, user.email, user.displayName?
```

**JWT claims (recommended):**

```json
{
  "sub": "<userId>",
  "email": "<email>",
  "iat": ...,
  "exp": ...
}
```

- Alg: **HS256 only**; parse with explicit `jwt.WithValidMethods([]string{"HS256"})`.
- `expiresIn`: seconds (default `3600`; env `JWT_TTL_SECONDS`).
- Secret: `JWT_SECRET` (≥32 chars **and** not a documented placeholder; see `.env.example`).

**Password rules (v1):** min 8 chars on register / create-user; email trimmed + lowercased for uniqueness.

### 4.2 Sync

| Method | Path | Auth | Status | Body | Response |
|--------|------|------|--------|------|----------|
| `POST` | `/sync/bootstrap` | yes | 200 | empty / null | `{ "rootId": "<userId>" }` |
| `GET` | `/sync/quota` | yes | 200 | — | `{ "usage", "limit" }` |
| `GET` | `/sync/manifest` | yes | 200 | — | `ITravelManifest` JSON (empty default if missing) |
| `PUT` | `/sync/manifest` | yes | 204 | `ITravelManifest` | empty (client “deletes” manifest by PUT empty travels — **no** `DELETE /sync/manifest`) |
| `GET` | `/sync/travels/{travelId}` | yes | 200 / 404 | — | travel JSON |
| `PUT` | `/sync/travels/{travelId}` | yes | 200 | travel JSON | `{ "fileId": "{travelId}" }` |
| `DELETE` | `/sync/travels/{travelId}` | yes | 204 | — | empty (idempotent OK: 204 if already gone) |
| `GET` | `/sync/user-data/{fileName}` | yes | 200 / 404 | — | JSON |
| `PUT` | `/sync/user-data/{fileName}` | yes | 204 | JSON | empty |
| `PUT` | `/sync/files/{travelId}/{itemId}/{fileName}` | yes | 200 | **raw bytes** | `{ "fileId": "files/{travelId}/{itemId}/{fileName}" }` |
| `GET` | `/sync/files/{fileId...}` | yes | 200 / 404 | — | blob + `Content-Type` |
| `DELETE` | `/sync/files/{fileId...}` | yes | 204 | — | empty |
| `POST` | `/sync/files/{travelId}/{itemId}/ensure-folder` | yes | 200 | empty | `{ "folderId": "files/{travelId}/{itemId}" }` |

### 4.3 Attachment path / ServeMux patterns

Register **specific** routes before wildcards:

```go
mux.HandleFunc("PUT /sync/files/{travelId}/{itemId}/{fileName}", ...)
mux.HandleFunc("POST /sync/files/{travelId}/{itemId}/ensure-folder", ...)
mux.HandleFunc("GET /sync/files/{fileId...}", ...)
mux.HandleFunc("DELETE /sync/files/{fileId...}", ...)
```

**Normalize `fileId` for GET/DELETE:**

1. URL-decode path value.
2. If prefix `files/`, strip it once → relative under user `files/` dir.
3. Reject empty, `.`, `..`, backslashes, absolute paths, NUL.
4. Clean with `path.Clean` / `filepath.Clean` then verify result is still under `{userRoot}/files/` via `filepath.Rel` + no `..` prefix.

Client behaviors to satisfy:

| Client method | HTTP | Effective path pattern |
|---------------|------|------------------------|
| `uploadBinaryFile` | `PUT .../sync/files/{travelId}/{itemId}/{fileName}` | segment form; `Content-Type` often `application/octet-stream` or file MIME |
| `downloadFileBlob` | `GET .../sync/files/files/{travel}/{item}/{name}` | **double `files/`** — wildcard must accept `files/...` |
| `deleteFile` (attachments) | `DELETE .../sync/files/files/...` | same |

Optional client fix (same Phase 1 window): strip leading `files/` before appending to `/sync/files/` — server should still accept both shapes.

**PUT attachment response `fileId`:** always `files/{travelId}/{itemId}/{fileName}` (no leading slash).

**Max upload size:** env `MAX_UPLOAD_MB` default `50` (MiB); exceed → `413` `PAYLOAD_TOO_LARGE`.

### 4.4 ID / name validation

| Param | Rules |
|-------|--------|
| `travelId`, `itemId` | Non-empty; `[A-Za-z0-9._-]+` (UUID-safe); reject `..` |
| `fileName` (attachment) | Non-empty; no `/` `\`; no `..`; reasonable max length (255) |
| `fileName` (user-data) | Exact allowlist only |

### 4.5 Error JSON

```json
{ "error": { "code": "NOT_FOUND", "message": "travel not found" } }
```

| HTTP | `code` (examples) |
|------|-------------------|
| 400 | `BAD_REQUEST`, `VALIDATION_ERROR` |
| 401 | `UNAUTHORIZED` |
| 403 | `FORBIDDEN`, `REGISTER_DISABLED` |
| 404 | `NOT_FOUND` |
| 409 | `CONFLICT` (e.g. email taken on register) |
| 413 | `QUOTA_EXCEEDED`, `PAYLOAD_TOO_LARGE` |
| 507 | `DISK_FULL` |
| 500 | `INTERNAL` |

Never leak stack traces or SQL errors in `message`.

### 4.6 CORS

Env: `CORS_ORIGINS` comma-separated exact origins (e.g. `http://localhost:4200,https://app.example.com`).

For matching Origin:

- `Access-Control-Allow-Origin: <origin>` (echo exact; never `*` with credentials if unused)
- `Access-Control-Allow-Headers: Authorization, Content-Type`
- `Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS`
- `Access-Control-Max-Age: 86400` (optional)
- Answer `OPTIONS` for `/*` with `204`

If `CORS_ORIGINS` empty: no CORS headers (native / same-origin / reverse-proxy CORS OK).

Client does **not** send cookies for SelfHosted; Bearer only — `Allow-Credentials` not required. Omit or set `false`.

---

## 5. Auth flow

### Modes

| Mode | How users appear |
|------|------------------|
| **A. Seed / CLI (recommended for private family server)** | `ALLOW_REGISTER=false`; create users via env seed or CLI |
| **B. Open register** | `ALLOW_REGISTER=true`; `POST /auth/register` |

### CLI: `travelplan-server create-user`

Same binary, subcommand:

```bash
travelplan-server create-user \
  --email alex@example.com \
  --password '…' \
  --display-name Alex
```

- Loads `DATA_DIR` / DB same as server.
- Prints `userId` on success; non-zero exit on conflict.
- Alternatively: flags as env for one-shot in Compose init — optional `SEED_ADMIN_EMAIL` + `SEED_ADMIN_PASSWORD` (+ optional `SEED_ADMIN_DISPLAY_NAME`) on server start: if set and email missing, create once (log userId). Do **not** recreate/overwrite password on every restart.

### Login → sync

1. Client `GET /health` (no auth).
2. Client `POST /auth/login`.
3. Client stores JWT; `GET /me`.
4. Client `POST /sync/bootstrap` → `rootId` (== userId).
5. Subsequent sync via FolderLayoutSyncAdapter → SelfHostedFolderClient.

**No refresh token:** client re-logins with stored password when JWT near expiry (`expiresAt - 60s`). Server only issues short-lived access JWTs.

### Register response

`201` `{ "userId": "<uuid>", "email": "<normalized>" }` — create user FS root lazily on first bootstrap (or eagerly on register; either OK if bootstrap remains idempotent).

---

## 6. Docker & Compose

### Files to create

| File | Purpose |
|------|---------|
| `Dockerfile` | Multi-stage build |
| `docker-compose.yml` | Service + volume + port |
| `.env.example` | Documented env template |
| `.dockerignore` | Exclude `.git`, docs noise, local data |

### Dockerfile (sketch)

```dockerfile
# build
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=1.0.0
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X github.com/aleksey-mu/travel-planner-server/internal/version.Version=${VERSION}" -o /out/travelplan-server ./cmd/server

# run — alpine locked for Phase 1 (HEALTHCHECK + create-user exec ergonomics)
FROM alpine:3.22
RUN adduser -D -u 65532 -H appuser
COPY --from=build /out/travelplan-server /travelplan-server
USER appuser
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/travelplan-server", "healthcheck"]
ENTRYPOINT ["/travelplan-server"]
CMD ["serve"]
```

**Locked (Phase 1):** alpine non-root + `healthcheck` subcommand (not distroless). Distroless can be a later hardening pass.

Implement subcommand `healthcheck` that GETs `http://127.0.0.1:$PORT/health` or dials the listen addr — keeps HEALTHCHECK self-contained.

### `docker-compose.yml` (sketch)

```yaml
services:
  travelplan-server:
    build: .
    image: travelplan-server:local
    ports:
      - "${HTTP_PUBLISH_PORT:-8080}:8080"
    env_file: .env
    environment:
      DATA_DIR: /data
      HTTP_ADDR: ":8080"
    volumes:
      - travelplan-data:/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "/travelplan-server", "healthcheck"]
      interval: 30s
      timeout: 3s
      retries: 3
      start_period: 5s

volumes:
  travelplan-data:
```

### `.env.example`

```bash
# Required — generate secret; do not commit real .env
#   openssl rand -base64 48
DATA_DIR=/data
JWT_SECRET=
HTTP_ADDR=:8080
# Web app origin(s) only (not the API URL). Canonical name: CORS_ORIGINS
CORS_ORIGINS=http://localhost:4200

# Optional
ALLOW_REGISTER=false
JWT_TTL_SECONDS=3600
VAULT_QUOTA_MB=10240
MAX_UPLOAD_MB=50
LOG_FORMAT=json
LOG_LEVEL=info
# Prefer create-user CLI; if using seed, remove password from .env after first boot
SEED_ADMIN_EMAIL=
SEED_ADMIN_PASSWORD=
SEED_ADMIN_DISPLAY_NAME=Admin
HTTP_PUBLISH_PORT=8080
```

**Config fail-fast:** reject empty `JWT_SECRET`, length &lt; 32, **and** known placeholders (e.g. `change-me`, `changeme`, strings containing `at-least-32-chars`). Length alone is not enough.

Compose `env_file: .env` requires the file to exist — operator must `cp .env.example .env` first.

### Operator steps (document in README)

```bash
cp .env.example .env
# set JWT_SECRET=$(openssl rand -base64 48), CORS_ORIGINS, optional SEED_ADMIN_*
docker compose up -d --build
docker compose exec travelplan-server /travelplan-server create-user --email you@example.com --password '…'
# In TravelPlan app: Self-hosted server URL http://<host>:8080 (LAN IP if phone/other device)
# Web: CORS_ORIGINS must include the web app origin (e.g. http://localhost:4200)
```

### Reverse proxy (note only — not in Compose v1)

Terminate TLS with Caddy/nginx; proxy to `127.0.0.1:8080`; set `CORS_ORIGINS` to the **web app** origin (not the API origin). See CORS section in `self-hosted-server.md`.

---

## 7. Implementation phases + acceptance tests

### Phase 0 — Skeleton

- [x] `go mod init`, `cmd/server` with `serve` / `create-user` / `healthcheck`
- [x] `internal/config` loads env; fails fast if `DATA_DIR` or `JWT_SECRET` missing / weak / placeholder
- [x] Align `travel-plans/docs/self-hosted-server.md`: `item-types.json`, dual fileId, no manifest DELETE
- [x] `log/slog` JSON or text via `LOG_FORMAT`
- [x] `GET /health` returns ok
- [x] Dockerfile (alpine) + compose + `.env.example` build and listen; healthcheck green

**Accept:**

```bash
curl -s http://127.0.0.1:8080/health
# {"status":"ok","version":"1.0.0"}
```

### Phase 1 — Auth + users DB

- [x] SQLite users table; bcrypt cost 12
- [x] register (gated), login, me, JWT middleware
- [x] create-user CLI + optional seed

**Accept (httptest sketch):**

```
POST /auth/login  email+password → 200 accessToken
GET  /me          Authorization: Bearer … → 200 id
GET  /me          no header → 401
POST /auth/register when ALLOW_REGISTER=false → 403
```

```bash
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"secret123"}' | jq -r .accessToken)
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/me
```

### Phase 2 — Sync JSON (manifest, travels, user-data)

- [x] bootstrap creates dirs; returns rootId
- [x] manifest GET default / PUT 204
- [x] travels CRUD
- [x] user-data allowlist

**Accept:**

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/sync/bootstrap
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/sync/manifest
curl -s -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"schemaVersion":1,"updatedAt":"2026-07-19T12:00:00Z","travels":[]}' \
  http://127.0.0.1:8080/sync/manifest -w "%{http_code}\n"   # 204
curl -s -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"id":"t1","name":"Trip","items":[]}' \
  http://127.0.0.1:8080/sync/travels/t1
# {"fileId":"t1"}
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/sync/quota
```

### Phase 3 — Attachments + quota enforce

- [x] ensure-folder, PUT/GET/DELETE files
- [x] path traversal tests (negative)
- [x] quota / payload 413
- [x] DELETE travel + DELETE file (both `files/...` and bare path)

**Accept:**

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8080/sync/files/t1/i1/ensure-folder
# {"folderId":"files/t1/i1"}
curl -s -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/octet-stream' \
  --data-binary @./photo.jpg \
  http://127.0.0.1:8080/sync/files/t1/i1/photo.jpg
# {"fileId":"files/t1/i1/photo.jpg"}
curl -s -H "Authorization: Bearer $TOKEN" \
  -o /tmp/out.jpg \
  http://127.0.0.1:8080/sync/files/files/t1/i1/photo.jpg
# also: .../sync/files/t1/i1/photo.jpg
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8080/sync/files/files/t1/i1/photo.jpg -w "%{http_code}\n"   # 204
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8080/sync/travels/t1 -w "%{http_code}\n"   # 204
# negatives: GET .../files/../etc/passwd or .../files/%2e%2e/... → 400
# PUT over MAX_UPLOAD_MB or quota → 413; free disk shortfall → 507 DISK_FULL
# GET /sync/manifest without Authorization → 401
```

### Phase 4 — CORS + Docker polish + table tests

- [x] CORS preflight from configured origin
- [x] Compose healthcheck green
- [x] `internal/...` unit tests: path sanitize, JWT alg confusion, bcrypt roundtrip, empty manifest
- [x] **Required:** `httptest` suite under `internal/httpapi` (auth + files happy/negative)

**CORS accept:**

```bash
curl -si -X OPTIONS http://127.0.0.1:8080/me \
  -H "Origin: http://localhost:4200" \
  -H "Access-Control-Request-Method: GET" \
  -H "Access-Control-Request-Headers: Authorization"
# 204 + Allow-Origin echo
```

### Phase 5 — Client smoke (manual)

- [x] API smoke against Compose (`health` → login → bootstrap → travel → dual-path file GET → quota) — 2026-07-19
- [ ] Point TravelPlan Self-hosted URL at compose port (browser dogfood)
- [ ] Test connection → login → bootstrap → create travel → sync → attach file → sign out / in → data present

### Suggested Go test files

```
internal/auth/jwt_test.go
internal/auth/password_test.go
internal/syncstore/paths_test.go
internal/syncstore/store_test.go      # t.TempDir()
internal/httpapi/api_test.go         # full mux integration
```

---

## 8. Security notes

| Topic | Requirement |
|-------|-------------|
| Path traversal | Never join user input unchecked; after Clean, `filepath.Rel` must not start with `..`; reject absolute paths |
| Symlinks | Do not create; if walking quota, do not follow symlinks out of user root (`filepath.WalkDir` + skip symlink dirs/files or verify resolved path under root) |
| JWT secret | Fail start if empty, length &lt; 32, or known placeholder; document rotate = all sessions invalidate |
| JWT alg | Explicit HS256 only (no `none`, no RS256 confuse) |
| bcrypt | Cost **12**; constant-time compare via `bcrypt.CompareHashAndPassword` |
| Timing on login | Generic `401` message for bad email/password (`INVALID_CREDENTIALS`) |
| Register | Default **off** in `.env.example` |
| Authorization scope | Every sync path must resolve under **authenticated** `userId` only — no cross-user rootId |
| Upload size | Cap body with `http.MaxBytesReader` |
| Logs | No passwords, no full JWTs; optional request-id |
| Rate limit | **Later** — login + register abuse; note for Phase 2+ |
| TLS | Terminate at reverse proxy; do not ship plaintext over internet without it |
| FS permissions | Container non-root; volume writable by that uid |

---

## 9. Open questions (minimal)

1. ~~**Runtime image**~~ — **locked:** alpine + `healthcheck` subcommand (Phase 1).
2. ~~**Eager user root**~~ — **locked:** bootstrap-only (lazy FS root).
3. **Whether to patch Angular client** to avoid double `files/` in GET/DELETE URLs — recommended small fix when touching client; server still accepts both.

No other blockers for Phase 1.

**Review:** [`plan-basic-sync-review.md`](./plan-basic-sync-review.md) (2026-07-19).

---

## 10. References — client adapter files

All under `D:\Projects\!0TEST_ANYTHING_HERE\travel-plans\` (Angular TravelPlan repo):

| File | Role |
|------|------|
| `docs/self-hosted-server.md` | API sketch (update when tightening) |
| `src/app/core/services/self-hosted-auth.service.ts` | `POST /auth/login`, `GET /health`, `GET /me`, Bearer session |
| `src/app/core/services/self-hosted-folder.client.ts` | All `/sync/*` calls |
| `src/app/core/sync/self-hosted-sync.adapter.ts` | Thin wrapper → `FolderLayoutSyncAdapter` |
| `src/app/core/sync/folder-layout-sync.adapter.ts` | Shared Drive-layout sync logic |
| `src/app/core/sync/cloud-folder-client.interface.ts` | Folder client contract |
| `src/app/constants/constants.ts` | `manifest.json`, `travels`, `files`, `user-data`, `user-prefs.json`, `item-types.json`, `checklist-templates.json` |
| `src/app/interfaces/interfaces.ts` | `ITravelManifest`, `IDriveQuota`, `ISelfHostedConnectionConfig`, … |
| `src/app/features/settings/self-hosted-config-dialog.component.ts` | UI for server URL + credentials |

### Client URL cheat-sheet (implement exactly)

```
GET  {base}/health
POST {base}/auth/login
GET  {base}/me
POST {base}/sync/bootstrap
GET  {base}/sync/quota
GET  {base}/sync/manifest
PUT  {base}/sync/manifest          # also used to “clear” (empty travels); no DELETE
GET  {base}/sync/travels/{travelId}
PUT  {base}/sync/travels/{travelId}
DELETE {base}/sync/travels/{travelId}
GET  {base}/sync/user-data/{fileName}
PUT  {base}/sync/user-data/{fileName}
PUT  {base}/sync/files/{travelId}/{itemId}/{fileName}     # raw body
GET  {base}/sync/files/{fileId...}                        # fileId often "files/..."
DELETE {base}/sync/files/{fileId...}
POST {base}/sync/files/{travelId}/{itemId}/ensure-folder
```

---

## Config reference (complete)

| Env | Required | Default | Description |
|-----|----------|---------|-------------|
| `DATA_DIR` | yes | — | Durable root |
| `JWT_SECRET` | yes | — | HS256 secret (≥32 chars; reject placeholders) |
| `HTTP_ADDR` | no | `:8080` | Listen address |
| `CORS_ORIGINS` | no | empty | Comma-separated **web app** origins (canonical name; not `CORS_ALLOWED_ORIGINS`) |
| `ALLOW_REGISTER` | no | `false` | Public registration |
| `JWT_TTL_SECONDS` | no | `3600` | Access token lifetime |
| `VAULT_QUOTA_MB` | no | `10240` | Per-user quota in MiB (10240 = 10 GiB) |
| `MAX_UPLOAD_MB` | no | `50` | Single attachment max in MiB |
| `LOG_FORMAT` | no | `json` | `json` \| `text` |
| `LOG_LEVEL` | no | `info` | slog level |
| `SEED_ADMIN_EMAIL` | no | — | Create-if-missing on start |
| `SEED_ADMIN_PASSWORD` | no | — | With email |
| `SEED_ADMIN_DISPLAY_NAME` | no | `Admin` | Display name for seed |

---

## Done definition (Phase 1)

1. `docker compose up -d --build` yields healthy service.
2. Admin can create a user (CLI or seed).
3. TravelPlan SelfHosted “Test connection” succeeds against the container.
4. Full sync round-trip: manifest + travel JSON + attachment upload/download.
5. Unit/integration tests cover auth, path safety, and sync happy path.
6. README documents Compose as primary setup; this plan linked.
