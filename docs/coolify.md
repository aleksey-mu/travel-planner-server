# Deploy on Coolify

Docker Compose resource built from this repo. `docker-compose.yml` uses Coolify's magic variables, so the domain, port and JWT secret need no manual setup.

---

## 1. Create the resource

1. Coolify → **New resource** → **Docker Compose** → connect `aleksey-mu/travel-planner-server`, branch `main`.
2. Compose file: `docker-compose.yml` (default).

## 2. Domain and required variables

1. Set the domain on the `travelplan` service **with the port**: `https://travelplan.example.com:8080`. The `:8080` is the container port for Coolify's proxy; it is not part of the public URL. HTTPS is handled by the proxy.
2. In **Environment Variables** set:

| Variable | Value |
|----------|-------|
| `JWT_SECRET` | random string, ≥32 chars. PowerShell: `[Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(48))` |
| `PUBLIC_BASE_URL` | `https://travelplan.example.com` (no port) |

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
2. `curl -s https://travelplan.example.com/health`
3. Admin panel: `https://travelplan.example.com/admin/`
4. In the app: **Self-hosted** → server URL `https://travelplan.example.com`.

Create an invite from the admin UI, or in the resource terminal: `/travelplan-server create-invite --label friend`.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| 502 / "No internal port" | The domain must include the port: `https://travelplan.example.com:8080` |
| Container exits immediately | Check logs — `PUBLIC_BASE_URL must be https://` means the domain was saved as `http://` |
| 429 on login from your IP | Coolify must forward `X-Forwarded-For` (compose sets `TRUST_PROXY=true`) |
| Web app CORS errors | Add the web origin to `CORS_ORIGINS` |
| Upgrade after schema change | Wipe the persistent volume and redeploy (no DB migrations) |

## Dockerfile-only (alternative)

New resource → **Dockerfile**, port **8080**, health path `/health`, storage at `/data`. Set `JWT_SECRET`, `PUBLIC_BASE_URL` and `TRUST_PROXY=true`. Compose is preferred.
