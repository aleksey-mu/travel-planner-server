# TravelPlan self-hosted server

Sync backend for **TravelPlan** that keeps trip data on infrastructure you control.

Instead of linking Google Drive / OneDrive / Dropbox, the app talks to this server over HTTPS. Data lives on the server disk under a durable volume (`DATA_DIR`).

**Status (multi-user in progress)**

- **Breaking / unreleased:** wipe `DATA_DIR` when upgrading builds — no DB or API compatibility.
- Auth: email + password → JWT (no OAuth, no SMTP).
- Today CLI/seed still allow **one** account until multi-user S3 lands; target is N private vaults (invites) — see `docs/plan-multi-user.md`.
- Quota API is the Phase 2 JSON shape (below), not `{usage,limit}`.

---

## What you need

| Item | Notes |
|------|--------|
| Host | VPS **or** Coolify (pick one in step 2) |
| Public URL | **HTTPS required** (e.g. `https://travelplan.example.com`). App allows `http://127.0.0.1` only for local Docker. |
| Persistent volume | Mounted at `/data` |
| Owner credentials | Email + password for the TravelPlan app |

---

## 1. Configure environment

Use the same variables whether you deploy on a VPS or Coolify: copy from `.env.example` into a `.env` file, **or** enter them in Coolify’s environment UI.

| Variable | Purpose |
|----------|---------|
| `JWT_SECRET` | Signing key for access tokens. Required, ≥32 chars, not a placeholder. Generate once: `openssl rand -base64 48` |
| `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` | Creates the owner on first boot if that email does not exist. **Unset the password after first successful login** |
| `SEED_ADMIN_DISPLAY_NAME` | Display name (default `Admin`) |
| `CORS_ORIGINS` | Allowed **web app** origins (comma-separated). Needed when TravelPlan runs in a browser. Native apps ignore CORS. Example: `https://app.example.com` |
| `SERVER_NAME` | Optional friendly host label |
| `MAX_USERS` | Soft account cap (default `100`; `0` = unlimited). Enforced when multi-user create/invite ships |
| `VAULT_QUOTA_MB` | Default vault cap in MiB (`10240` ≈ 10 GiB). **`0` = unlimited** (disk still applies) |
| `MAX_UPLOAD_MB` | Max size of a single attachment, in MiB (default `50`) |
| `HTTP_PUBLISH_PORT` | Host port for Compose on a VPS (default `8080`). Unused when Coolify’s proxy fronts the container |
| `ALLOW_REGISTER` | Keep `false` |
| `DATA_DIR` | `/data` when using the provided Compose file |

Minimum to set before deploy: `JWT_SECRET`, owner seed (or create the user later with CLI), and `CORS_ORIGINS` if you use the web app.

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

Create / reset owner without seed:

```bash
docker compose exec travelplan-server /travelplan-server create-user \
  --email you@example.com --password '…' --display-name You

docker compose exec travelplan-server /travelplan-server set-password \
  --email you@example.com --password '…'
```

Until multi-user create ships, a second `create-user` still fails (single account). Wipe `/data` when switching major builds.

### Option B — Coolify

1. New resource → **Docker Compose** (preferred) or Dockerfile, pointing at this repo.
2. Apply the same environment variables from step 1 in Coolify.
3. Keep a **persistent volume** on `/data` (Compose already defines `travelplan-data:/data`). Do not wipe it on redeploy.
4. Attach your domain with Coolify’s HTTPS proxy. Health path: `/api/v1/health`.
5. After first login, clear `SEED_ADMIN_PASSWORD` in Coolify and redeploy.

If you deploy Dockerfile-only (no Compose), set `DATA_DIR=/data`, `HTTP_ADDR=:8080`, mount a volume at `/data`, expose port `8080`. Entrypoint requires `DATA_DIR` to be `/data` or a path under `/data`.

---

## 3. Connect TravelPlan

1. In the app: **Self-hosted** (login or sync settings).
2. Server URL = your public base, **no** `/api/v1` suffix — e.g. `https://travelplan.example.com`.
3. Sign in with the owner email/password.
4. Test connection → save → sync.

**TLS:** use HTTPS for any non-local host. Local dogfood only: `http://127.0.0.1:8080`. Plain `http://` LAN IPs are not a supported mode.

---

## 4. Storage behaviour

- Default vault cap: `VAULT_QUOTA_MB` (MiB). `0` = unlimited (free disk still applies).
- Per-user overrides come later (admin); `quota_bytes` already in schema.
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

## 5. Operations cheatsheet

```bash
docker compose ps
docker compose logs -f travelplan-server
docker compose up -d --build
docker compose exec travelplan-server /travelplan-server set-password --email … --password …
```

CLI in the image: `serve` | `create-user` | `set-password` | `healthcheck`

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
| `MAX_USERS` | no | `100` | Account soft cap (`0` = unlimited) |
| `INVITE_TTL_DAYS` | no | `7` | Invite lifetime (multi-user) |
| `PASSWORD_RESET_TTL_SECONDS` | no | `3600` | Admin reset-link TTL |
| `LOG_FORMAT` / `LOG_LEVEL` | no | `json` / `info` | |
| `SEED_ADMIN_*` | no | — | First-boot owner; remove password after |

**Current product plan:** [docs/plan-multi-user.md](docs/plan-multi-user.md) · step build: [docs/plan-multi-user-impl.md](docs/plan-multi-user-impl.md).  
Historical Phase 1 notes (superseded for quota/tenancy): [docs/plan-basic-sync.md](docs/plan-basic-sync.md).
