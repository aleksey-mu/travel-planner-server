# Deploy on Coolify

Step-by-step for a **Docker Compose** resource from this repo. The compose file is the source of truth — env vars and volumes are declared there; Coolify surfaces them in the UI.

---

## 1. Create the resource

1. Coolify → **New resource** → **Docker Compose** → connect `aleksey-mu/travel-planner-server` (or your fork).
2. Branch: `main`.
3. Compose file: `docker-compose.yml` (default).
4. Build pack: Docker Compose (builds from `Dockerfile` via `build: .`).

---

## 2. Domain & port

1. **Configuration → Domains** → add your public hostname, e.g. `travelplan.example.com`.
2. Map domain → container port **8080** (HTTP). Enable Coolify HTTPS (Let’s Encrypt).
3. If deploy fails with **port 8080 already allocated**, set `HTTP_PUBLISH_PORT` to a free host port (e.g. `18080`) in Environment Variables — container still listens on **8080**; only the host binding changes.

---

## 3. Persistent storage

Compose defines a named volume `travelplan-data` mounted at `/data`. Coolify shows it under **Persistent Storage** (read-only reference — edit in `docker-compose.yml` if you need a bind mount).

Do **not** wipe this volume on redeploy unless you intend to reset all users and vaults.

---

## 4. Environment variables (required)

Open **Environment Variables** and set at least:

| Variable | Example | Notes |
|----------|---------|--------|
| `JWT_SECRET` | *(generate)* | `openssl rand -base64 48` — ≥32 chars, not a placeholder |
| `PUBLIC_BASE_URL` | `https://travelplan.example.com` | Must be `https://` — used for invite/reset links |
| `SERVER_NAME` | `Alex's NAS` | Shown on join / Settings / admin header |
| `TRUST_PROXY` | `true` | **Required** behind Coolify’s reverse proxy (rate limits + client IP) |
| `SEED_ADMIN_EMAIL` | `you@example.com` | First admin on **empty** DB only |
| `SEED_ADMIN_PASSWORD` | *(strong password)* | Remove after first successful login |

### Recommended

| Variable | Suggested |
|----------|-----------|
| `CORS_ORIGINS` | Your web app origin(s), comma-separated — e.g. `https://app.example.com` (native apps ignore CORS) |
| `HTTP_PUBLISH_PORT` | `8080` (default). Use another free port if host `8080` is taken (e.g. `18080`) |
| `MAX_USERS` | `100` (default) |
| `VAULT_QUOTA_MB` | `10240` (10 GiB per vault; `0` = unlimited) |

All other vars have defaults in `docker-compose.yml`. See `.env.example` for the full list.

**After first login:** clear `SEED_ADMIN_PASSWORD` in Coolify and redeploy.

---

## 5. Health check

| Setting | Value |
|---------|--------|
| Path | `/api/v1/health` |
| Port | `8080` |
| Expected | `200` with `{"status":"ok","version":"…"}` |

Compose and the image already define a container healthcheck (`travelplan-server healthcheck`).

---

## 6. Deploy

1. **Deploy** (or **Redeploy**).
2. Verify: `curl -s https://travelplan.example.com/api/v1/health`
3. Admin panel: `https://travelplan.example.com/admin/`
4. In TravelPlan app: **Self-hosted** → server URL = `https://travelplan.example.com` (no `/api/v1` suffix).

---

## 7. First invite

- Admin UI → **Create invite**, or terminal:

```bash
# Coolify → resource → Terminal (exec into travelplan-server container)
/travelplan-server create-invite --label friend
```

Share the invite URL. Friend opens TravelPlan `/join` or `https://YOUR_HOST/join/?token=…`.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `Bind for 0.0.0.0:8080 failed: port is already allocated` | Set `HTTP_PUBLISH_PORT` to a free port (e.g. `18080`); domain still maps to container port **8080** |
| Docker build fails on `apk add su-exec` | Fixed in repo — uses `shadow` + `su` (main Alpine repo only) |
| Container exits immediately | Check logs — usually missing/weak `JWT_SECRET` or `DATA_DIR` not `/data` |
| 429 on login from your IP | `TRUST_PROXY=true`; Coolify must forward `X-Forwarded-For` |
| Invite links missing host | Set `PUBLIC_BASE_URL` to your `https://` domain |
| Web app CORS errors | Add web origin to `CORS_ORIGINS` |
| Upgrade after schema change | Wipe persistent volume + redeploy (no DB migrations yet) |

---

## Dockerfile-only (alternative)

If you prefer a single-container app instead of Compose:

1. New resource → **Dockerfile** → same repo.
2. Port **8080**, health path `/api/v1/health`.
3. Mount persistent storage at **`/data`**.
4. Set the same env vars as above (`DATA_DIR=/data`, `HTTP_ADDR=:8080` are set in the image/entrypoint defaults; still set `DATA_DIR=/data` explicitly).

Compose is preferred — volume and healthcheck are already wired.
