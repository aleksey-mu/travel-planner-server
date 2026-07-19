# TravelPlan self-hosted server

Sync backend for **TravelPlan** that keeps trip data on infrastructure you control.

Instead of linking Google Drive / OneDrive / Dropbox, the app talks to this server over HTTPS. Data lives on the server disk under a durable volume (`DATA_DIR`).

**Multi-user (friends-scale)**

- **Breaking / unreleased:** wipe `DATA_DIR` when upgrading builds — no DB or API compatibility.
- Auth: email + password → JWT (no OAuth, no SMTP). Open register always **403**; accounts come from CLI/`SEED_ADMIN_*` or **one-time invites**.
- Default soft cap: **`MAX_USERS=100`** (`0` = unlimited). Invites are **one use**, TTL default **7 days**.
- Admin UI: `{PUBLIC_BASE_URL}/admin/` (separate browser login; not the TravelPlan app JWT).
- Thin browser fallbacks: `/join/?token=…`, `/reset-password/?token=…` (prefer the TravelPlan app when available).

---

## What you need

| Item | Notes |
|------|--------|
| Host | VPS **or** Coolify (pick one in step 2) |
| Public URL | **HTTPS required** (e.g. `https://travelplan.example.com`). App allows `http://127.0.0.1` only for local Docker. |
| Persistent volume | Mounted at `/data` |
| Owner credentials | Email + password for the first admin |

---

## 1. Configure environment

Use the same variables whether you deploy on a VPS or Coolify: copy from `.env.example` into a `.env` file, **or** enter them in Coolify’s environment UI.

| Variable | Purpose |
|----------|---------|
| `JWT_SECRET` | Signing key for access tokens. Required, ≥32 chars, not a placeholder. Generate once: `openssl rand -base64 48` |
| `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` | Creates the **first admin** on empty DB. **Unset the password after first successful login** |
| `SEED_ADMIN_DISPLAY_NAME` | Display name (default `Admin`) |
| `SERVER_NAME` | Friendly host label shown on join / Settings / admin header |
| `PUBLIC_BASE_URL` | `https://…` origin for invite + reset URLs (required for copy-paste links) |
| `CORS_ORIGINS` | Allowed **web app** origins (comma-separated). Native apps ignore CORS |
| `MAX_USERS` | Soft account cap (default `100`; `0` = unlimited). Counts **all** rows incl. disabled |
| `INVITE_TTL_DAYS` | Invite lifetime (default `7`) |
| `PASSWORD_RESET_TTL_SECONDS` | Admin reset-link TTL (default `3600`) |
| `AUTH_RATE_LIMIT_MAX` / `AUTH_RATE_LIMIT_WINDOW_SECONDS` | Brute-force limits (default `20` / `900`) |
| `TRUST_PROXY` | `true` only behind a reverse proxy that sets `X-Real-IP` / `X-Forwarded-For` (Coolify etc.) |
| `VAULT_QUOTA_MB` | Default vault cap in MiB (`10240` ≈ 10 GiB). **`0` = unlimited** (disk still applies) |
| `MAX_UPLOAD_MB` | Max size of a single attachment, in MiB (default `50`) |
| `HTTP_PUBLISH_PORT` | Host port for Compose on a VPS (default `8080`). Unused when Coolify’s proxy fronts the container |
| `ALLOW_REGISTER` | Keep `false` (ignored — register always disabled) |
| `DATA_DIR` | `/data` when using the provided Compose file |

Minimum before deploy: `JWT_SECRET`, owner seed **or** later `create-user`, plus `SERVER_NAME` + `PUBLIC_BASE_URL` for friend invites. Set `CORS_ORIGINS` if you use the web app. Behind Coolify/Caddy set `TRUST_PROXY=true`.

Health check: `GET /api/v1/health` → `{"status":"ok","version":"…"}`.

---

## 2. Deploy (choose one)

### Option A — VPS + Docker Compose

1. Clone this repo on the VPS; put the env from step 1 in `.env` next to `docker-compose.yml`.
2. Start:

```bash
docker compose up -d --build
```

3. Put TLS in front (Caddy, nginx, Traefik, or your panel). Proxy the public hostname to container port `8080`.
4. Verify: `curl -s https://YOUR_HOST/api/v1/health`
5. After first successful owner login, remove `SEED_ADMIN_PASSWORD` from `.env` and run `docker compose up -d` again.

### Option B — Coolify

1. New resource → **Docker Compose** (preferred) or Dockerfile, pointing at this repo.
2. Apply the same environment variables from step 1 in Coolify (set `TRUST_PROXY=true`).
3. Keep a **persistent volume** on `/data` (Compose already defines `travelplan-data:/data`). Do not wipe it on redeploy.
4. Attach your domain with Coolify’s HTTPS proxy. Health path: `/api/v1/health`.
5. After first login, clear `SEED_ADMIN_PASSWORD` in Coolify and redeploy.

If you deploy Dockerfile-only (no Compose), set `DATA_DIR=/data`, `HTTP_ADDR=:8080`, mount a volume at `/data`, expose port `8080`. Entrypoint requires `DATA_DIR` to be `/data` or a path under `/data`.

---

## 3. First admin and friends

1. **First boot** seeds an admin when `SEED_ADMIN_*` is set **and** the users table is empty. Prefer CLI if you skip seed:

```bash
docker compose exec travelplan-server /travelplan-server create-user \
  --email you@example.com --password '…' --display-name You --admin
```

2. Set **`SERVER_NAME`** (e.g. `Alex’s NAS`) and **`PUBLIC_BASE_URL=https://your.host`**.
3. **Invite friends** (one-time link, TTL shown):

```bash
docker compose exec travelplan-server /travelplan-server create-invite \
  [--expires-days 7] [--label friend]
```

Or open **`https://YOUR_HOST/admin/`**, sign in as admin → **Create invite**. Share the URL (token shown once).

4. Friend opens TravelPlan **`/join`** (or the thin page `https://YOUR_HOST/join/?token=…`) → trusts **server name + admin nicknames** → creates account → **saves recovery codes** → ack.
5. **Forgot password:** recovery code in the app, **or** ask an admin for a reset link (`create-reset-link` / admin panel → Reset link → `/reset-password/?token=…`).
6. **Admin duties** (panel or CLI): promote/demote, disable/enable, remove, per-user quota (`default` / `personal` MiB / **`unlimited`**), reset links. Last enabled admin cannot be demoted/disabled/deleted while members remain.
7. **Empty server:** if the last account is deleted, the DB has zero users. Next boot: set `SEED_ADMIN_*` again **or** run `create-user` to recreate the first admin. Deleted vault dirs are already gone.

`MAX_USERS` (default 100) caps seats (disabled users still count). Wipe `/data` when switching major builds.

---

## 4. Connect TravelPlan

1. In the app: **Self-hosted** (login or sync settings).
2. Server URL = your public base, **no** `/api/v1` suffix — e.g. `https://travelplan.example.com`.
3. Sign in with email/password.
4. Test connection → save → sync.

**TLS:** use HTTPS for any non-local host. Local dogfood only: `http://127.0.0.1:8080`. Plain `http://` LAN IPs are not a supported mode.

**Admin panel:** open `{serverUrl}/admin/` in a system browser and sign in there (app JWT is not reused).

---

## 5. Storage behaviour

- Default vault cap: `VAULT_QUOTA_MB` (MiB). `0` = unlimited (free disk still applies).
- Admins set per-user mode: server default, personal MiB, or unlimited.
- Hard floor: free space on the volume that holds `DATA_DIR`.
- `GET /api/v1/sync/quota` returns:

```json
{
  "usageBytes": 0,
  "configuredLimitBytes": 10737418240,
  "effectiveLimitBytes": 10737418240,
  "mode": "default",
  "defaultLimitBytes": 10737418240
}
```

`configuredLimitBytes` / `effectiveLimitBytes` are `null` when unlimited. `mode` is `default` | `personal` | `unlimited`.

- Over finite quota → `413 QUOTA_EXCEEDED`. Disk exhausted → `507 DISK_FULL`.

---

## 6. Operations cheatsheet

```bash
docker compose ps
docker compose logs -f travelplan-server
docker compose up -d --build
docker compose exec travelplan-server /travelplan-server healthcheck
docker compose exec travelplan-server /travelplan-server set-password --email … --password …
docker compose exec travelplan-server /travelplan-server create-reset-link --email …
docker compose exec travelplan-server /travelplan-server list-users
docker compose exec travelplan-server /travelplan-server set-quota --email … --mb unlimited
```

CLI in the image: `serve` | `create-user` | `set-password` | `create-invite` | `revoke-invite` | `create-reset-link` | `list-users` | `set-quota` | `disable-user` | `enable-user` | `delete-user` | `promote-admin` | `demote-admin` | `healthcheck`

**Admin unreachable / lost codes:** operator CLI `set-password` or `create-reset-link` (host trust boundary).

**Volume backup:** back up the Compose volume mounted at `/data` (SQLite + vault files). Restore = replace volume contents; wipe when changing incompatible builds.

---

## Environment reference

| Env | Required | Default | Notes |
|-----|----------|---------|-------|
| `DATA_DIR` | yes | — | Compose sets `/data` |
| `JWT_SECRET` | yes | — | ≥32 chars |
| `HTTP_ADDR` | no | `:8080` | Listen address in-container |
| `HTTP_PUBLISH_PORT` | no | `8080` | VPS Compose host mapping |
| `CORS_ORIGINS` | no | empty | Web origins only |
| `ALLOW_REGISTER` | no | `false` | Keep false |
| `JWT_TTL_SECONDS` | no | `3600` | |
| `VAULT_QUOTA_MB` | no | `10240` | Vault cap MiB; **`0` = unlimited** |
| `MAX_UPLOAD_MB` | no | `50` | Per-file cap (MiB) |
| `SERVER_NAME` | no | empty | Friendly host label |
| `PUBLIC_BASE_URL` | no | empty | `https://…` for invite/reset URLs |
| `MAX_USERS` | no | `100` | Account soft cap (`0` = unlimited) |
| `INVITE_TTL_DAYS` | no | `7` | Invite lifetime |
| `PASSWORD_RESET_TTL_SECONDS` | no | `3600` | Admin reset-link TTL |
| `AUTH_RATE_LIMIT_MAX` | no | `20` | `0` disables |
| `AUTH_RATE_LIMIT_WINDOW_SECONDS` | no | `900` | |
| `TRUST_PROXY` | no | `false` | Coolify / reverse proxy |
| `LOG_FORMAT` / `LOG_LEVEL` | no | `json` / `info` | |
| `SEED_ADMIN_*` | no | — | First-boot admin on **empty** DB; remove password after |

**Current product plan:** [docs/plan-multi-user.md](docs/plan-multi-user.md) · step build: [docs/plan-multi-user-impl.md](docs/plan-multi-user-impl.md).  
Historical Phase 1 notes (superseded for quota/tenancy): [docs/plan-basic-sync.md](docs/plan-basic-sync.md).
