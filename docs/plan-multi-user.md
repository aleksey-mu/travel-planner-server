# Phase 2 — Multi-user self-hosted vaults

**Status:** planned (not started)  
**Depends on:** Phase 1 single-owner server ([plan-basic-sync.md](./plan-basic-sync.md)) shipped  
**Client companion:** `travel-plans` — `.agents/plans/self-hosted-multi-user.md`  
**Last updated:** 2026-07-19

---

## 1. Goal

Allow **N friends** on one deployment, each with a **private vault** (`users/{userId}/`), without SMTP/email.

**In scope**

| Capability | Purpose |
|------------|---------|
| **Invite links** | Closed registration — admin (or CLI) issues one-time join URLs |
| **Recovery codes** | User resets password without admin / without email |
| **Admin panel** | Web UI for invite, reset password, disable user, list users |
| **CLI** | Same ops over SSH / Coolify terminal when UI is down |

**Out of scope (Phase 2)**

- Shared trips / live collaborative editing  
- Email / SMTP / magic-link login  
- Open `ALLOW_REGISTER=true` on the public internet  
- Per-user custom quotas UI (keep global `VAULT_QUOTA_MB` unless trivial)  
- OAuth / SSO  

**Product rule:** multi-user = **N private vaults on one host**, not household shared itineraries.

---

## 2. Roles

| Role | Who | Powers |
|------|-----|--------|
| **Owner / admin** | First user (or flagged `is_admin`) | Create invites, list/disable users, reset password, regenerate another user’s recovery codes only as last resort (prefer user self-serve) |
| **Member** | Joined via invite | Own vault sync, own recovery codes, change own password while logged in |
| **Operator** | SSH / Coolify | Full CLI — bypass when admin panel unreachable |

Phase 2: support **at least one admin**. Nice: second admin via CLI flag so “admin away” is less painful.

---

## 3. User journeys

### 3.1 Add a friend (invite)

1. Admin opens admin panel → **Create invite** (optional label, expiry e.g. 7 days, max uses = 1).  
2. Server returns URL: `https://{host}/join?token={opaque}`.  
3. Friend opens URL (browser or TravelPlan deep link) → sets email + password → account created → vault bootstrapped.  
4. Token consumed (or decremented); expired/used → clear error.

### 3.2 Forgot password (recovery codes)

1. At first successful login (or Settings), app shows **recovery codes** once; user must acknowledge “I saved them”.  
2. Codes stored as **hashes** only (like passwords).  
3. Login screen → “Forgot password” → email + one unused code → set new password → that code invalidated (or all codes rotated — lock one policy).  
4. Logged-in user can **regenerate** codes (invalidates old set).

### 3.3 Admin away + codes lost

Not recoverable without host access. Operator uses CLI `set-password` / `reset-recovery`. Document this limit in README.

### 3.4 Disable user

Admin disables account → middleware rejects JWT for that user; data retained on disk until explicit purge (purge = later).

---

## 4. Data model (SQLite)

Extend `server.db` (hard-cut OK; app/server not released).

### `users` (extend)

| Column | Notes |
|--------|--------|
| existing | `id`, `email`, `password_hash`, `display_name`, `created_at`, `updated_at` |
| `is_admin` | INTEGER 0/1 — first Create sets true; later members false |
| `disabled_at` | TEXT NULL — if set, auth fails |
| `password_version` | INTEGER — bump on password change; embed in JWT claims to invalidate old tokens |

**Remove** Phase 1 `Count >= 1` / `ErrOwnerExists` gate on Create (replace with invite-only or admin Create).

### `invites` (new)

| Column | Notes |
|--------|--------|
| `id` | UUID |
| `token_hash` | UNIQUE — store hash of raw token only |
| `created_by` | user id |
| `label` | optional |
| `expires_at` | RFC3339 |
| `max_uses` | default 1 |
| `use_count` | |
| `created_at` | |
| `revoked_at` | NULL |

### `recovery_codes` (new)

| Column | Notes |
|--------|--------|
| `id` | |
| `user_id` | FK |
| `code_hash` | |
| `used_at` | NULL until consumed |
| `created_at` | |

Policy (locked): **10 codes** per generation; regenerating deletes unused old rows.

---

## 5. API sketch

Base: `/api/v1`. Admin routes require Bearer + `is_admin`.

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/auth/login` | — | unchanged (+ reject disabled) |
| POST | `/auth/recover` | — | `{ email, recoveryCode, newPassword }` |
| GET | `/invites/{token}` | — | validate invite metadata (no secret leak) |
| POST | `/invites/{token}/accept` | — | `{ email, password, displayName }` → user + tokens |
| GET | `/me` | user | include `isAdmin`, `hasRecoveryCodes` |
| POST | `/me/password` | user | change password (needs current) |
| POST | `/me/recovery-codes` | user | regenerate → returns plaintext codes **once** |
| GET | `/admin/users` | admin | list |
| POST | `/admin/users/{id}/disable` | admin | soft-disable |
| POST | `/admin/users/{id}/password` | admin | set password |
| POST | `/admin/invites` | admin | create → returns raw token / URL once |
| GET | `/admin/invites` | admin | list active |
| DELETE | `/admin/invites/{id}` | admin | revoke |

Sync routes unchanged — still scoped by JWT `sub`.

**Admin panel:** either static HTML+JS served by Go at `/admin/` (simple, no Angular deploy) **or** pages inside TravelPlan when `isAdmin`. Prefer **server-served `/admin/`** for Phase 2 so Coolify-only operators need no app build; TravelPlan can deep-link later.

---

## 6. CLI (extend `cmd/server`)

```text
create-user --email --password [--display-name] [--admin]
set-password --email --password
list-users
disable-user --email
enable-user --email
create-invite [--expires-days 7] [--label …]   # prints URL path + token
revoke-invite --id
reset-recovery --email   # invalidates codes; user must use admin-set password then regenerate
```

Remove single-owner Create refusal; first user without `--admin` still gets `is_admin=true` if table empty.

---

## 7. Security

| Topic | Rule |
|-------|------|
| Invite tokens | ≥128-bit random; store hash only; HTTPS required in README |
| Recovery codes | Random; hash (bcrypt or salted SHA-256 with pepper from `JWT_SECRET`); rate-limit `/auth/recover` |
| Admin panel | Same origin as API; session = JWT; recommend bind to private network or SSO later |
| JWT | Include `pwd` / `password_version`; reject mismatch |
| Disable | Middleware checks `disabled_at` + user exists (already checks exists) |
| CSRF | Admin cookie session later if cookie auth; with Bearer-from-localStorage use same as app |
| Timing | Keep dummy bcrypt on unknown email for login + recover |

---

## 8. Client (TravelPlan) work

| Item | Notes |
|------|--------|
| Join flow | Route or dialog: paste invite URL / token → accept |
| Recovery UI | Login “Forgot password” + first-login codes modal |
| Settings | Regenerate recovery codes; change password |
| Optional | Link “Open server admin” when `me.isAdmin` (external `/admin/`) |

No shared-trip UI. Capability matrix: collaboration stays ❌; multi-account host becomes ✅ for `*-selfhost`.

---

## 9. Implementation phases

### P0 — Lift single-owner + schema

- [ ] `is_admin`, `disabled_at`, `password_version` on users  
- [ ] Remove `ErrOwnerExists` Create cap  
- [ ] JWT claim + middleware version / disabled checks  
- [ ] CLI: `list-users`, `--admin` on create-user  

**Accept:** two users via CLI; disabled user gets 401; password change invalidates old token.

### P1 — Invites

- [ ] `invites` table + create/accept/revoke API  
- [ ] CLI `create-invite` / `revoke-invite`  
- [ ] Minimal join page (server HTML or app dialog)  

**Accept:** invite URL creates second user; reuse fails; expiry fails.

### P2 — Recovery codes

- [ ] Generate on first login / explicit endpoint  
- [ ] `/auth/recover`  
- [ ] Regenerate while authenticated  
- [ ] App modal + forgot-password UI  

**Accept:** recover works with unused code; reused code fails; regenerate invalidates old.

### P3 — Admin panel

- [ ] `/admin/` UI: users list, invite create (copy link), disable, set password  
- [ ] README: Coolify/VPS ops for invites + recovery  

**Accept:** admin can onboard a friend without CLI; member cannot hit admin APIs.

### P4 — Polish

- [ ] Rate limits on login/recover/accept  
- [ ] Second admin via CLI  
- [ ] Optional purge-user (delete vault dir) — gated, confirmed  

---

## 10. README updates (when shipping)

Replace “single-owner only” with:

1. First boot still seeds **admin**.  
2. Add friends via **invite** (panel or CLI).  
3. Users save **recovery codes**.  
4. Lost codes → admin/CLI reset.

Keep VPS **or** Coolify as one deploy step with two options.

---

## 11. Done definition

1. Admin invites friend → friend syncs own vault.  
2. Friend resets password with recovery code while admin offline.  
3. Admin can disable a user and create a new invite.  
4. All of the above also possible via CLI.  
5. No email/SMTP required.  
6. Tests: invite accept/reuse, recover, disabled user, password_version, admin authz.  
7. Capability matrix + feature-ideas Decision 14 updated to Phase 2 multi-user (private vaults).

---

## 12. Open questions (resolve before P3)

1. Admin panel: **server `/admin/`** vs in-app only? → default **server `/admin/`**.  
2. Recovery: consume **one** code vs rotate **all** on any recover? → default **consume one**.  
3. Invite URL host: derive from `PUBLIC_BASE_URL` env (required for correct links behind Coolify).  
4. Max users soft cap? → optional `MAX_USERS` env (0 = unlimited).
