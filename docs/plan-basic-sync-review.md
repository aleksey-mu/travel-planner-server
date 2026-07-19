# Review: Phase 1 basic sync plan

**Reviewed:** `docs/plan-basic-sync.md`  
**Against:** `travel-plans/docs/self-hosted-server.md`, `.agents/plans/self-hosted-sync-ui.md`, client inventory (`SelfHostedAuthService` / `SelfHostedFolderClient`)  
**Date:** 2026-07-19

---

## Verdict

**Ship-with-fixes**

The plan is implementable end-to-end: endpoint surface matches the live client, stack choices are current, Docker story is almost day-one ready, and security notes for path/JWT are solid. A few small contract/ops clarifications (and one secret-placeholder trap) should land before coding so implementers do not guess.

Do **not** rework architecture or expand scope.

---

## Critical fixes

### C1. `JWT_SECRET` example passes the length gate

`.env.example` uses `change-me-to-a-long-random-string-at-least-32-chars` (≥32). Config validation “length ≥ 32” would **accept** this and ship a public known secret.

**Fix:** Reject a denylist of example strings **or** require entropy (e.g. fail if secret equals any documented placeholder). README: one-liner to generate (`openssl rand -base64 48`). Do not treat “≥32 chars” as sufficient alone.

### C2. Dockerfile HEALTHCHECK vs distroless contradiction

Sketch uses `gcr.io/distroless/static-debian12:nonroot` **and** `HEALTHCHECK CMD ["/travelplan-server", "healthcheck"]`, then comments that distroless may not work for HEALTHCHECK. Done definition #1 requires a healthy Compose service.

**Fix:** Lock one path in the plan (recommended: **alpine non-root + `healthcheck` subcommand**, or distroless + Compose `healthcheck` that runs the same binary dial). Remove the dual sketch as “either.”

### C3. Document client manifest “delete” = PUT empty (no `DELETE /sync/manifest`)

Client inventory: `deleteFile('manifest.json')` → `PUT /sync/manifest` with empty travels — **not** HTTP DELETE. Plan correctly omits DELETE for manifest but never states why.

**Fix:** One row in §4.2 / §10: “No `DELETE /sync/manifest`; client clears via PUT empty default. Server must accept that PUT (already required).”

---

## Important fixes

### I1. Lock CORS env name across repos

| Doc | Name |
|-----|------|
| Server plan | `CORS_ORIGINS` |
| UI plan §6 | `CORS_ALLOWED_ORIGINS` (example) |
| API sketch | headers only, no env name |

**Fix:** Canonical name = **`CORS_ORIGINS`**. Patch UI plan / operator copy to match when touching that doc (UI already says “exact names live in server plan”).

### I2. Spec freeze for A1/A2 before or with first PR

Plan defers updating `self-hosted-server.md` to implement time. UI plan P0 wants fileId + `item-types.json` frozen first.

**Fix:** Treat doc align (`item-types.json`, dual `files/` accept, empty-manifest PUT) as Phase 0 / first commit checklist item — even a short “contract delta” section is enough so client and server agents do not diverge.

### I3. Acceptance coverage gaps

Happy-path curls are good; add negative/edge accepts (curl or `httptest`):

| Case | Why |
|------|-----|
| `GET/DELETE .../sync/files/files/...` **and** bare path | Client double-`files/` today |
| Path traversal (`../`, absolute, backslash) → 400 | Security claim |
| `DELETE /sync/travels/{id}` → 204 (missing OK) | Client travel delete |
| `PUT` over quota / over `MAX_UPLOAD_BYTES` → 413 codes | Done definition / UI toasts |
| Sync route without Bearer → 401 | Auth middleware |
| `PUT` empty manifest (clear) → 204 | Client “delete manifest” |
| Register when disabled → 403; email conflict → 409 | Auth modes |

Unit list already mentions path sanitize + JWT alg; wire at least traversal + double-`files/` into Phase 3/4 accept text.

### I4. Compose day-one friction for non-devs

- `env_file: .env` fails hard if `.env` missing — README must lead with `cp .env.example .env` (already sketched; make it failure-mode explicit).
- `SEED_ADMIN_PASSWORD` left in `.env` forever — note “rotate / remove after first boot” or prefer `create-user` for anything beyond a lab box.
- LAN / phone test: document that app URL is `http://<host-lan-ip>:8080` and `CORS_ORIGINS` must include the **web app** origin (not the API URL).
- Resolve binary name consistency: image entrypoint `/travelplan-server` vs module `travel-planner-server` (fine if documented once).

### I5. `HTTP_ADDR` required vs default

Config table: `HTTP_ADDR` **Required: yes**, **Default: `:8080`**. Pick one (recommended: optional, default `:8080`; only `DATA_DIR` + `JWT_SECRET` hard-required).

### I6. Filename / URL encoding edge

Client `downloadFileBlob` concatenates `files/...` into the path **without** per-segment `encodeURIComponent`; PUT uses encoded segments. Server plan should say: decode path values; accept `%2F`-style only via ServeMux wildcards as already designed; reject `fileName` with `/` `\`. Optional client A1 fix remains recommended so GET/DELETE stop double-prefixing.

---

## Nits

- `Access-Control-Allow-Methods` includes `PATCH` though no PATCH routes — harmless; can drop.
- `HTTP_PUBLISH_PORT` is Compose-only; either add to config reference as “compose publish mapping” or leave out of server env table explicitly.
- Open Q #1 (distroless vs alpine) duplicates C2 — close it when locking runtime.
- Open Q #2 (eager root vs bootstrap-only) — plan already recommends bootstrap-only; promote to locked decision.
- Quota TOCTOU under concurrent PUTs — acceptable for Phase 1 family server; one-line “best-effort” note optional.
- No `DELETE /sync/user-data/{file}` — client inventory does not need it; OK to omit.
- `meta/` on disk unused — fine; avoid inventing handlers.
- Register exists server-side; no client register UI — aligned with UI non-goals; keep CLI/seed as primary.
- bcrypt cost 12 is fine; no need for argon2 in Phase 1.
- `godotenv` optional — prefer Compose `env_file` as source of truth; godotenv only for bare-metal `go run`.

---

## Completeness vs API contract

| Endpoint / behavior | Plan | Client today | Notes |
|---------------------|------|--------------|-------|
| `GET /health` | ✅ | Auth | |
| `POST /auth/login` | ✅ | Auth | |
| `GET /me` | ✅ | Auth | |
| `POST /auth/register` | ✅ gated | **No client** | OK (CLI/seed/open register) |
| `POST /sync/bootstrap` | ✅ | FolderClient | |
| `GET /sync/quota` | ✅ | FolderClient | |
| `GET/PUT /sync/manifest` | ✅ | FolderClient | Empty default GET ✅ |
| `DELETE /sync/manifest` | ❌ intentional | Uses PUT empty | **C3** |
| Travels GET/PUT/DELETE | ✅ | FolderClient | |
| User-data GET/PUT | ✅ | FolderClient | Allowlist → `item-types.json` (breaking vs sketch) |
| Files PUT / GET / DELETE / ensure-folder | ✅ | FolderClient | Dual `files/` accept ✅ |
| Refresh token | ❌ non-goal | Password re-login | Aligned |
| Error envelope | ✅ | Expected | |
| CORS | ✅ | Web required | Env name **I1** |

**Breaking vs sketch (OK, already called out):** `item-types.json` replaces `custom-item-presets.json`; GET/DELETE accept `files/`-prefixed ids. Update API sketch when freezing (I2).

Every endpoint the client calls is implementable from this plan.

---

## Docker / Compose day-one

**Strengths:** multi-stage build, volume, healthcheck intent, seed + `create-user`, `.env.example`, operator curl path, TLS deferred to reverse proxy.

**Gaps:** C2 (healthy service), C1 (secret), I4 (`.env` missing, LAN/CORS, seed hygiene). After those, non-dev story `cp → edit secret → compose up → create-user → paste URL in app` is credible.

---

## Stack modernity

| Choice | Assessment |
|--------|------------|
| Go 1.26 | Current stable (1.26.x as of 2026-07) — good |
| stdlib `ServeMux` method+wildcards | Appropriate for ~15 routes |
| `golang-jwt/jwt/v5` | Current major |
| `golang.org/x/crypto/bcrypt` | Standard |
| `modernc.org/sqlite` | Right for `CGO_ENABLED=0` |
| `log/slog` | Stdlib — good |
| No Gin/GORM | Correct for this surface |

**No outdated stack choices.** Optional: pin `toolchain go1.26.x` in `go.mod` when scaffolding.

---

## Security (summary)

Well covered: traversal + `Rel`, symlink policy on quota walk, HS256-only, bcrypt compare, generic login 401, register default off, per-user root isolation, `MaxBytesReader`, no secret logging, non-root container.

Remaining: **C1** known secret, seed password longevity (**I4**), rate limit deferred (acceptable if documented for internet-exposed hosts), TLS-at-proxy (already noted).

---

## Alignment with UI plan

| Topic | Status |
|-------|--------|
| Cloud-storage parity / non-goals (no sharing, no register UI) | Aligned |
| Password re-login, no refresh | Aligned |
| A1 fileId dual-accept + prefer client strip | Aligned; server plan correct |
| A2 `item-types.json` | Aligned |
| CORS required for web; exact origin | Aligned |
| CORS **env name** | **Mismatch** → I1 |
| Breaking changes OK | Both say yes |
| Phase mapping P0–P5 | Compatible; server Phase 0–5 ≈ UI P1–P5 |
| Capability matrix / migration wizard | UI-only; out of server Phase 1 — OK |

No server breaking change beyond A1/A2 that would invalidate the UI plan.

---

## Acceptance tests sufficient?

**Almost.** Phased curls + suggested unit files are enough to unblock an implementer. Not sufficient alone for “path safety + auth + quota” done definition until **I3** negatives are listed and Phase 4 `httptest` is treated as required for mux integration (optional → required for auth+files happy/negative).

Phase 5 manual client smoke is appropriate; do not require live Go server in Angular CI (UI plan already mocks).

---

## Recommended next implementation slice

1. **Contract freeze (half day):** Patch `self-hosted-server.md` for `item-types.json` + fileId dual-accept + no manifest DELETE; apply C1–C3 / I1 / I5 nits into `plan-basic-sync.md`.
2. **Phase 0 skeleton:** `go mod`, `serve`/`healthcheck`/`create-user` stubs, config validation (strong secret), `GET /health`, **alpine** Dockerfile + Compose that goes healthy on empty `.env` after `cp` + generated `JWT_SECRET`.
3. **Phase 1 auth:** SQLite users, login/me/JWT, seed + create-user — enough for TravelPlan “Test connection.”
4. **Then** Phase 2 JSON sync → Phase 3 files/quota → Phase 4 CORS + table tests → Phase 5 manual app round-trip.

Do not start attachments before auth+bootstrap+manifest work; client “Test connection” only needs health + login + `/me`.

---

## Optional plan patches applied

Small in-place edits to `plan-basic-sync.md` for C1–C3, I1, I5, runtime lock, and acceptance bullets — see git diff on that file.
