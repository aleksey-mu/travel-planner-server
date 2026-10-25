# Deploy on Coolify

Docker Compose resource built from this repo. `docker-compose.yml` uses Coolify's magic variables, so the domain, port and JWT secret need no manual setup.

---

## 1. Create the resource

1. Coolify → **New resource** → **Docker Compose** → connect `aleksey-mu/travel-planner-server`, branch `main`.
2. Compose file: `docker-compose.yml` (default).

## 2. Domain

Open **Configuration → Services → travelplan** and set the domain (e.g. `https://travelplan.example.com`). The compose file declares `SERVICE_URL_TRAVELPLAN_8080: /`, which tells Coolify the service listens on port **8080**; HTTPS is handled by its proxy. If you type the domain yourself, include the port: `https://travelplan.example.com:8080` (Coolify warns "No internal port" otherwise; the port is not part of the public URL). No `ports:` mapping is used, so there are no host-port conflicts.

From that domain Coolify fills in automatically:

| Variable | Source |
|----------|--------|
| `PUBLIC_BASE_URL` | `SERVICE_URL_TRAVELPLAN_8080` (your domain; must be `https://`) |
| `JWT_SECRET` | `SERVICE_PASSWORD_64_JWT` (generated once) |

## 3. Optional environment variables

| Variable | Notes |
|----------|-------|
| `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` | First admin on an empty DB. Clear the password after first login and redeploy |
| `CORS_ORIGINS` | Web app origin(s), comma-separated |
| `SERVER_NAME` | Defaults to the domain hostname |
| `MAX_USERS`, `VAULT_QUOTA_MB`, `MAX_UPLOAD_MB` | Defaults `100`, `10240`, `50` |

## 4. Storage

Volume `travelplan-data` → `/data` (SQLite + vaults). Do not wipe on redeploy unless you intend to reset all users and data.

## 5. Deploy and verify

1. **Deploy**.
2. `curl -s https://travelplan.example.com/api/v1/health`
3. Admin panel: `https://travelplan.example.com/admin/`
4. In the app: **Self-hosted** → server URL `https://travelplan.example.com` (no `/api/v1`).

Create an invite from the admin UI, or in the resource terminal: `/travelplan-server create-invite --label friend`.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Domain field missing / 502 | Set the domain on the `travelplan` service; it must proxy to port 8080 |
| Container exits immediately | Check logs — `PUBLIC_BASE_URL must be https://` means the domain was saved as `http://` |
| 429 on login from your IP | Coolify must forward `X-Forwarded-For` (compose sets `TRUST_PROXY=true`) |
| Web app CORS errors | Add the web origin to `CORS_ORIGINS` |
| Upgrade after schema change | Wipe the persistent volume and redeploy (no DB migrations) |

## Dockerfile-only (alternative)

New resource → **Dockerfile**, port **8080**, health path `/api/v1/health`, storage at `/data`. Set `JWT_SECRET`, `PUBLIC_BASE_URL` and `TRUST_PROXY=true` yourself. Compose is preferred.
