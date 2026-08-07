# Phase 2 — Implementation plan (spec-driven)

**Status:** ready to execute  
**Product source of truth:** [plan-multi-user.md](./plan-multi-user.md)  
**Client notes:** `travel-plans/.agents/plans/self-hosted-multi-user.md`  
**Rule:** one step at a time. Do not start step *N+1* until step *N* **Accept** is green. **Hard-cut:** no legacy shims, no dual API shapes, no DB upgrade paths — wipe `DATA_DIR` when schema/API changes.

---

## How to work

1. Read the product § cited in the step.  
2. Implement **only** that step’s scope.  
3. Add/extend tests listed under **Tests**.  
4. Run Accept commands; paste results in the PR/commit message.  
5. Stop. Review. Then next step.

**Repos order:** Server steps **S0→S10** first (API stable), then client **C0→C4**. Thin server HTML join/reset can ship with S4/S6; polish later.

---

## Package map (target)

```text
internal/
  config/          # + SERVER_NAME, MAX_USERS, INVITE_TTL_DAYS, PASSWORD_RESET_TTL_SECONDS, rate limits
  auth/            # users (+columns), jwt (pwd claim), middleware (disabled/admin live)
  invites/         # NEW — invite tokens
  recovery/        # NEW — recovery codes + ack
  resetlink/       # NEW — password reset tokens
  admin/           # NEW or httpapi — admin user ops helpers
  syncstore/       # quota: 0 = unlimited; /sync/quota shape
  httpapi/         # routes + handlers + rate limit + static /admin /join /reset-password
  ratelimit/       # NEW — crude IP+key limiter
cmd/server/        # CLI extensions
```

---

## Server steps

### S0 — Config + schema columns ✅

### S1 — JWT `pwd` + disabled + live admin ✅

**Product:** §5, §7  
**Do:**

- Embed `pwd` (= `password_version`) in JWT; middleware rejects mismatch.  
- Reject `disabled_at` on protected routes + login.  
- `AdminMiddleware` re-reads `is_admin` from DB each request.  
- `SetPassword` / `BumpPasswordVersion` bump `password_version`.

**Tests:** bump invalidates token; disabled → 401 sync/login; demote → admin 403.  
**Accept:** `go test ./internal/config/... ./internal/auth/... ./internal/httpapi/...`

---

### S2 — Quota hard-cut (`0` = unlimited) + `/sync/quota` shape ✅

**Product:** §3.9  
**Do:**

- `EffectiveQuotaLimit` / `CanAcceptGrowth`: `UnlimitedConfigured` (-1); keep **413** vs **507**.  
- `GET /sync/quota` → `{ usageBytes, configuredLimitBytes|null, effectiveLimitBytes|null, mode, defaultLimitBytes }`.  
- Per-user `quota_bytes` via resolver on writes; `VAULT_QUOTA_MB=0` allowed.

**Tests:** ResolveConfigured table; unlimited disk; API modes default/unlimited/personal.  
**Accept:** `go test ./internal/syncstore/... ./internal/httpapi/...`

---

### S3 — Lift single-owner gate + kill open register ✅

**Product:** §1, §4  
**Do:**

- Remove `ErrOwnerExists`; `Create(…, asAdmin, maxUsers)`.  
- `POST /auth/register` always **403**.  
- CLI: `--display-name` required, `--admin`; enforce `MAX_USERS`.  
- First user always admin.

**Tests:** two users; third at MAX_USERS=2 fails; register 403; CLI create-user; seed skips when users exist.  
**Accept:** `go test ./internal/auth/... ./internal/httpapi/... ./cmd/server/...`

---

### S4 — Invites (create / get / revoke) + trust payloads ✅

**Product:** §3.1, §3.6, §3.11  
**Do:**

- Tables `invites`; hash tokens; `max_uses=1`; TTL from env.  
- `GET /server/info` → `publicBaseUrl`, `serverName`, `adminDisplayNames[]`, `userCount`, `maxUsers`.  
- `GET /invites/{token}` → validate + same trust fields + `expiresAt`.  
- `POST/GET/DELETE /admin/invites` (admin auth).  
- CLI `create-invite`, `revoke-invite`.  
- Thin HTML optional stub OK; full page can wait.

**Tests:** expired/revoked/invalid; serverName fallback to hostname; all enabled admin names.  
**Accept:** `go test ./internal/auth/... ./internal/httpapi/... ./internal/config/... ./cmd/server/...`

---

### S5 — Accept + recovery codes + ack ✅

**Product:** §3.1 mid-join, §5  
**Do:**

- `POST /invites/{token}/accept` atomic: check MAX_USERS (count **all** rows), create user, mint **10** codes, increment use_count, return **login-shaped tokens + `recoveryCodes[]`**.  
- 409 email taken; 403 full; second accept fails.  
- `POST /me/recovery-codes`, `POST /me/recovery-codes/ack`.  
- Login includes `needsRecoveryCodesAck`; if unacked → client will force regenerate (server: regenerate endpoint clears ack).  
- Concurrent accept: only one wins (SQLite transaction).

**Tests:** accept once; concurrent; email 409; full 403; ack clears flag; unacked → regenerate returns new codes.  
**Accept:** `go test ./internal/auth/... ./internal/httpapi/... ./internal/config/... ./cmd/server/...`

---

### S6 — Recover + admin reset links ✅

**Product:** §3.2  
**Do:**

- `POST /auth/recover` → login-shaped tokens; consume one code; bump `pwd`.  
- Table `password_reset_tokens`; admin `POST .../password-reset`; `GET/POST /password-reset/{token}`.  
- Admin set-password: bump `pwd`, keep codes, revoke outstanding reset tokens.  
- TTL 1h.

**Tests:** recover once; reuse fail; reset once; expired; set-password revokes tokens.  
**Accept:** `go test ./internal/auth/... ./internal/httpapi/... ./internal/config/... ./cmd/server/...`

---

### S7 — Admin user ops + last-enabled-admin rules ✅

**Product:** §3.4, §3.7, §3.9  
**Do:**

- `GET /admin/users`, `PATCH` (`isAdmin`, `disabled`, `quotaMode`/`quotaBytes`), `DELETE` other.  
- `DELETE /me` with sole-admin rules.  
- Refuse **disable** and **demote** of last **enabled** admin (self or other); concurrent demote safe.  
- Promote disabled OK. Enable user.  
- `GET /users` enabled-only directory.  
- CLI: list/set-quota/disable/enable/delete/promote/demote.

**Tests:** matrix §3.4; concurrent demote; quotaMode PATCH; DELETE /me blocked when sole+members.  
**Accept:** cannot disable last admin; promote+disable self when N>1 works.

---

### S8 — Rate limits (ship bar) ✅

**Product:** §7, §9 P2  
**Do:**

- Limit login, recover, accept, password-reset by IP (+ email where applicable).  
- Return 429 with stable error code.

**Tests:** trip limiter in httptest.  
**Accept:** 429 after N rapid recover attempts.

---

### S9 — Admin panel `/admin/` ✅

**Product:** §5 surfaces  
**Do:**

- Static HTML+JS: login → JWT sessionStorage; users (usage/quota modes); invites (show expiresAt); reset link; promote/demote/disable/enable/remove; header `SERVER_NAME`.  
- CSP baseline.  
- No TravelPlan JWT.

**Tests:** httptest or go test serving login+list; manual smoke OK for UI polish.  
**Accept:** admin can create invite and set user quota unlimited in UI.

---

### S10 — Server README + thin `/join` + `/reset-password` HTML ✅

**Product:** §10, §3.1  
**Do:**

- README multi-user ops: SERVER_NAME, invites, recovery, reset link, MAX_USERS, empty re-seed, Coolify.  
- Thin HTML pages calling same APIs (fallback).  
- `.env.example` complete.

**Accept:** README steps work on fresh Compose; healthcheck green.

---

## Client steps (TravelPlan)

### C0 — Types + adapter for new API shapes ✅

**Do:** Extend self-host client: `/server/info`, invite get/accept, `/sync/quota` new shape, `/me` ack flags, recover, password-reset. Specs for parsing.  
**Accept:** unit tests for DTO mapping (unlimited null limits, modes A–F).

---

### C1 — `/join` wizard ✅

**Product:** §3.1  
**Do:** 3 steps; trust; expiry; full; accept→codes→ack; mid-join force on login; HTTPS block; native query prefill.  
**Tests:** unit + browser journey.  
**Accept:** browser join happy path + expired invite on step 1.

---

### C2 — Forgot password + `/reset-password` ✅

**Product:** §3.2  
**Do:** Cold **server URL** + recover; ask-admin copy; reset-password route + deep link.  
**Accept:** browser recover + reset token flow.

---

### C3 — Settings (quota A–F, trust, codes, delete, Open admin) ✅

**Product:** §3.4, §3.7, §3.9  
**Do:** Quota from `/sync/quota`; ask-admin hint; trust serverName+admins; regenerate+ack; delete account rules; Open admin deep-link.  
**Accept:** unit cases A–F; browser delete member; sole-admin CTA.

---

### C4 — i18n + capability matrix + people list ✅

**Do:** All locale keys; `GET /users` nicknames UI; update capability-matrix / feature-ideas.  
**Accept:** `translation-files.spec.ts`; matrix cells for selfhost multi-user.

---

## Cross-cutting Accept (done definition)

From [plan-multi-user.md](./plan-multi-user.md) §11 + §14:

| # | Gate |
|---|------|
| 1 | Invite → join (serverName + admins) → sync own vault |
| 2 | Recover with code offline from admin |
| 3 | Admin reset link works once |
| 4 | Admin promote/quota/usage/remove |
| 5 | Settings A–F + ask admin |
| 6 | Sole-admin rules |
| 7 | MAX_USERS + single-use invite TTL |
| 8 | CLI parity |
| 9 | `go test ./...` + client unit + `test:browser` matrix |
| 10 | Docs + matrix updated |

---

## Suggested commits (one per step)

```text
S0  feat(server): multi-user schema + config envs
S1  feat(server): jwt pwd + disabled + live admin
S2  feat(server): unlimited quota + sync/quota shape
S3  feat(server): lift single-owner; block register
S4  feat(server): invites + server info trust payload
S5  feat(server): invite accept + recovery codes ack
S6  feat(server): recover + password reset links
S7  feat(server): admin user ops + last-admin rules
S8  feat(server): rate limits on auth endpoints
S9  feat(server): /admin panel
S10 docs(server): multi-user README + thin join/reset pages
C0  feat(app): self-host multi-user API client
C1  feat(app): /join wizard
C2  feat(app): forgot password + reset-password
C3  feat(app): settings quota trust delete
C4  feat(app): i18n + people list + matrix
```

---

## First action

Start **S0** only. After Accept, stop for review before S1.
