# TravelPlan self-hosted server

Sync backend for **TravelPlan** that keeps trip data on infrastructure you control.

Instead of linking Google Drive / OneDrive / Dropbox, the app talks to this server over HTTPS. Data lives on the server disk under a durable volume (`DATA_DIR`).

**Multi-user (friends-scale)**

- **Unreleased:** no DB migrations or API compatibility — wipe `DATA_DIR` when upgrading builds.
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
| `JWT_SECRET` | Signing key, ≥32 chars. Generate: `openssl rand -base64 48` (PowerShell: `[Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(48))`) |
| `PUBLIC_BASE_URL` | `https://…` origin for invite + reset links. Your public domain, no port |
| `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` | Creates the **first admin** on an empty DB. Remove the password after first login |
| `CORS_ORIGINS` | Web app origin(s), comma-separated. Native apps ignore CORS |
| `SERVER_NAME` | Label shown on join / Settings / admin. Defaults to the `PUBLIC_BASE_URL` hostname |
| `MAX_USERS` | Account cap (default `100`, `0` = unlimited; disabled users count) |
| `VAULT_QUOTA_MB` | Default vault cap in MiB (default `10240`, `0` = unlimited; disk still applies) |
| `MAX_UPLOAD_MB` | Max single attachment in MiB (default `50`) |

Everything else is fixed: access tokens 1 h, invites 7 days, reset links 1 h, auth rate limit 20 attempts / 15 min, JSON logs. `TRUST_PROXY=true` is set in the compose file; `DATA_DIR` is `/data` in the image (`./data` when running the binary directly).

Health check: `GET /api/v1/health` → `{"status":"ok","version":"…"}`.

---

## 2. Deploy (choose one)

### Option A — VPS + Docker Compose

1. Clone this repo; put `JWT_SECRET` and `PUBLIC_BASE_URL` in `.env` (see `.env.example`).
2. `docker compose up -d --build`. The container only `expose`s `8080`; put a TLS reverse proxy (Caddy, nginx, Traefik) in front and proxy your hostname to it.
3. Verify: `curl -s https://YOUR_HOST/api/v1/health`
4. After first owner login, remove `SEED_ADMIN_PASSWORD` from `.env` and re-run `docker compose up -d`.

### Option B — Coolify

**Full walkthrough:** [docs/coolify.md](docs/coolify.md)

1. New resource → **Docker Compose** → this repo, `docker-compose.yml`.
2. Set the domain on the `travelplan` service as `https://your.domain:8080` (the port is for the proxy only), and set `JWT_SECRET` and `PUBLIC_BASE_URL`.
3. Optionally set `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` and `CORS_ORIGINS`.
4. Volume `travelplan-data` → `/data` is already in compose — do not wipe on redeploy.

Dockerfile-only is supported; see [docs/coolify.md](docs/coolify.md#dockerfile-only-alternative).

---

## 3. First admin and friends

1. **First boot** seeds an admin when `SEED_ADMIN_*` is set **and** the users table is empty. Prefer CLI if you skip seed:

```bash
docker compose exec travelplan /travelplan-server create-user \
  --email you@example.com --password '…' --display-name You --admin
```

2. Optionally set **`SERVER_NAME`** (e.g. `Alex’s NAS`); `PUBLIC_BASE_URL` must be your `https://` host.
3. **Invite friends** (one-time link, TTL shown):

```bash
docker compose exec travelplan /travelplan-server create-invite \
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
docker compose logs -f travelplan
docker compose up -d --build
docker compose exec travelplan /travelplan-server healthcheck
docker compose exec travelplan /travelplan-server set-password --email … --password …
docker compose exec travelplan /travelplan-server create-reset-link --email …
docker compose exec travelplan /travelplan-server list-users
docker compose exec travelplan /travelplan-server set-quota --email … --mb unlimited
```

CLI in the image: `serve` | `create-user` | `set-password` | `create-invite` | `revoke-invite` | `create-reset-link` | `list-users` | `set-quota` | `disable-user` | `enable-user` | `delete-user` | `promote-admin` | `demote-admin` | `healthcheck`

**Admin unreachable / lost codes:** operator CLI `set-password` or `create-reset-link` (host trust boundary).

**Volume backup:** back up the Compose volume mounted at `/data` (SQLite + vault files). Restore = replace volume contents; wipe when changing incompatible builds.

---

## Environment reference

See the table in [step 1](#1-configure-environment). Optional extras honored by the binary: `DATA_DIR` (default `./data`), `HTTP_ADDR` (default `:8080`), `TRUST_PROXY` (default `false`).

**Current product plan:** [docs/plan-multi-user.md](docs/plan-multi-user.md) · step build: [docs/plan-multi-user-impl.md](docs/plan-multi-user-impl.md).  
Historical Phase 1 notes (superseded for quota/tenancy): [docs/plan-basic-sync.md](docs/plan-basic-sync.md).
