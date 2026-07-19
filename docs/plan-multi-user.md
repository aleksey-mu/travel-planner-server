# Phase 2 — Multi-user self-hosted vaults

**Status:** planned — **build-ready**; impl steps S0–S2 done — see [plan-multi-user-impl.md](./plan-multi-user-impl.md)  
**Depends on:** Phase 1 single-owner server ([plan-basic-sync.md](./plan-basic-sync.md)) shipped  
**Implementation (spec-driven steps):** [plan-multi-user-impl.md](./plan-multi-user-impl.md) — **S0→S10** then **C0→C4**  
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
| **Admin reset links** | Admin shares a one-time password-reset URL (no SMTP) when user lost codes |
| **Admin panel** | Invite, list users, usage/quota, promote/demote admin, disable/remove, reset links |
| **CLI** | Same ops over SSH / Coolify terminal when UI is down |
| **Per-user quota** | Global default + personal / **unlimited** override + Settings A–F + tests |
| **Max users** | `MAX_USERS=100`; disabled count; env overridable |
| **Server name** | `SERVER_NAME` env — join / Settings / admin header |

**Out of scope (Phase 2)**

- Shared trips / live collaborative editing  
- Email / SMTP / magic-link login (reset links are **hand-shared**, not emailed)  
- Open `ALLOW_REGISTER=true` on the public internet  
- OAuth / SSO  
- Billing / soft-quota emails  

**Product rule:** multi-user = **N private vaults on one host**, not household shared itineraries.

---

## 2. Roles

| Role | Who | Powers |
|------|-----|--------|
| **Admin** (`is_admin`) | First user + any promoted | Invites, list users, usage/quota, promote/demote, disable/remove, create reset links, set password (escape) |
| **Member** | Joined via invite | Own vault sync, recovery codes, forgot-password (code **or** ask admin for link), change password, delete self |
| **Operator** | SSH / Coolify | Full CLI — bypass when admin panel unreachable |

**N admins** allowed (§3.10). Last-admin protection on demote / self-delete.

---

## 3. User journeys

### 3.1 Add a friend (invite) — **locked**

TravelPlan UI is often **serverless** (native / no app host). The invite must identify the **sync server**, not a TravelPlan web origin.

**Canonical invite URL (app or share text):**

```text
/join?server=https%3A%2F%2Fsync.example.com&token={opaque}
```

| Rule | Lock |
|------|------|
| `server` query param | Public sync base URL (not a secret). Safe in the URL. Always URL-encoded. |
| `token` query param | One-time invite secret (treat like a magic link: HTTPS, short TTL, **single use**). |
| **`max_uses`** | Always **1** — one invite link = one account. No multi-use invites in Phase 2. |
| **TTL** | Default **7 days** (`INVITE_TTL_DAYS=7`). Shown clearly to **admin** when creating/listing (expires at …) and to **joiner** on step 1 (invite expires … / expired → block Continue). |
| Join UI | TravelPlan route **`/join`** (and optionally server page `https://{sync}/join`). Prepopulate **server** + **token** from query when present; fields stay **editable** so the user can verify the host before submitting a password. |
| Partial links | Token only → user must enter server. Server only → user must enter token. Neither → empty form (paste). |
| Do not | Put password in the URL. Token-only links with no visible server field on the client. Host join on a private TravelPlan web deploy. Multi-use invite tokens. |

**Admin “copy invite” output** (from panel/CLI), using `PUBLIC_BASE_URL`:

1. **App-oriented link** (preferred for chat):  
   `{PUBLIC_BASE_URL}` is the sync host; share either  
   - deep/path form for the app: documented as `/join?server={urlencoded PUBLIC_BASE_URL}&token=…`, or  
   - full sync join page: `{PUBLIC_BASE_URL}/join?token=…` (server implied by origin; no `server` param needed on that page).
2. **Paste bundle** (native-friendly fallback): plain lines `Server: …` / `Invite: …` / optional QR of the app query URL.

**Flow (TravelPlan `/join` — locked wizard)**

| Step | Screen | Content |
|------|--------|---------|
| **1 — Understand** | Info / trust | Server URL (editable if not from query), **serverName** + **all admin nicknames**, invite **expires at**, `userCount`/`maxUsers` if full → block Continue with “server full”. Trust copy + checkbox. Primary: **Continue** (valid invite + reachable server + checkbox). Non-loopback `http://` → blocked (§3.8). |
| **2 — Create account** | Form | Email, **display name**, password ×2. Submit → `POST /invites/{token}/accept`. Response = **login-shaped tokens + `recoveryCodes[]` plaintext once**. Email taken → **409** inline; server full → **403**. |
| **3 — Recovery codes** | One-shot | Show codes from accept response (copy/download). Verify-one paste + “stored safe” ack → `POST /me/recovery-codes/ack` → set `recovery_codes_acked_at` → enter app. Do not skip. |

Do not combine trust + password on one scroll. Do not show codes before account exists.

**Mid-join abandon (locked):** Accept already created user + code hashes, but plaintext only in that response. If wizard dies before ack: next successful login → `needsRecoveryCodesAck: true` → **force regenerate** (invalidate old hashes, show new plaintext once) → same verify+ack UI. Never leave an account that can sync without completed ack.

On step 1, validate invite + server (`GET /invites/{token}`, use `userCount`/`maxUsers` from invite or `/server/info`) before Continue.

### 3.2 Forgot password — **locked**

From self-host login (or cold entry), user chooses:

| Path | Flow |
|------|------|
| **A — Recovery code** | **Server URL** (required if not already linked / not in query) + email + one unused code → new password ×2 → code consumed → **login-shaped tokens** (same as login). |
| **B — Admin reset link** | Copy: “Ask your server admin for a reset link.” Admin shares URL; user opens `/reset-password?server=…&token=…`. |

**Cold start (no session):** Path A always shows editable **server** field (prefill from last-used self-host URL if app remembers it). Path B needs the shared link (includes `server`).

**Admin reset link (no SMTP):**

1. Admin panel / CLI → **Create password-reset link**.  
2. URL: `/reset-password?server=…&token=…` (or `{PUBLIC_BASE_URL}/reset-password?token=…`).  
3. TravelPlan shows **serverName** + **adminDisplayNames** (no trust checkbox) → password ×2 → login-shaped tokens.  
4. Token: one-time, TTL **1 hour** (`PASSWORD_RESET_TTL_SECONDS`), hashed. Bumps `password_version`.  
5. Admin **set password** = escape hatch — bumps `password_version`; **keeps** recovery codes; **revokes** outstanding reset tokens for that user.

**Codes onboarding:** §3.1 step 3 + forced re-show until ack. Logged-in user can regenerate (clears ack until new ack).

### 3.3 Admin away + codes lost

User asks admin for a **reset link** (§3.2 B). If admin unreachable and codes lost → only host operator CLI (`set-password` / `create-reset-link`). Document in README.

### 3.4 Disable / remove users & admins — **locked**

| Action | Who | Behavior |
|--------|-----|----------|
| **Disable** | Admin → **other** user | Soft: cannot login; vault retained. **Refuse** if target is the **last enabled admin** (same as demote). |
| **Remove (delete)** | Admin → **other** user | Hard purge; confirm nickname + email |
| **Promote admin** | Admin | Set `is_admin=1` — allowed on **disabled** users (they still can’t use admin until enabled) |
| **Demote admin** | Admin | Clear `is_admin`; refuse if that would leave **zero enabled admins** |
| **Enable** | Admin | Clear `disabled_at`; login works again |
| **List** | Admin | nickname, email, flags, usage / quota |

**Self-actions** — only if ≥1 **other enabled admin** remains afterward. **Disable self** / **demote self** / disable-or-demote last enabled admin → refuse.

| Use case | What admin sees | Options |
|----------|-----------------|---------|
| **Sole admin, alone** | Only user | **Delete account** OK (export + re-seed note). Disable/demote self hidden. |
| **Sole admin, members exist** | Banner + block self-destruct | CTA: **Open admin panel** (`{serverUrl}/admin/`) — Settings has **no** promote picker. |
| **One of N admins** | Full self controls | Demote / disable / `DELETE /me` after confirm. |
| **Admin on another** | Full | Promote / demote / disable / enable / remove / reset / quota / password. Last-enabled-admin disable/demote refused. |

**Empty server:** last user `DELETE /me` (or purge) → zero users OK. Next boot: `SEED_ADMIN_*` / CLI `create-user` creates first admin again (document in README). Vault dirs for deleted ids already gone.

**`/admin/` auth:** separate login page on the sync host (email+password → JWT in sessionStorage). Native “Open admin” opens **system browser** — no app JWT; admin signs in there. Intentional.

### 3.5 Display name (nickname) — **locked**

Friends on one host need a human label when admins (and later members) see “who is on this server.” Email alone is a poor UX and should stay private from non-admins.

| Rule | Lock |
|------|------|
| Field | Existing `display_name` — product name: **display name / nickname** |
| Required | Yes on invite accept and `create-user` (trim; min 1 char; max e.g. 64) |
| Unique | **Not** required (duplicates OK; disambiguate with email only in admin UI) |
| Visibility | **Admin** user list: nickname + email. **Members** (Phase 2 minimal): nickname (+ optional id) via a read-only directory endpoint — **not** email. |
| Editable | `PATCH /me` `{ displayName }` while logged in; also admin can set on create |
| Not | Separate `nickname` column — reuse `display_name`. Not used for auth. |

Phase 2 ships admin visibility for sure; member-visible directory (`GET /users` or `/admin/users` public subset) is in scope so “show users to other users” is possible without shared trips.

### 3.6 Trust & transparency — **locked**

Self-host is **not** end-to-end encrypted against the server operator. Users must understand that before they put trips/attachments on a friend’s machine.

**Show clearly (join screen + Settings → Account / self-host):**

| Fact | Copy intent (i18n later) |
|------|---------------------------|
| Who runs the server | **Server name** (§3.11) + **all** enabled admins’ nicknames — e.g. “**Alex’s NAS** — admins: **Alex**, **Sam**” |
| Where data lives | “Your trips and files are stored on **this server**, not only on your device.” |
| Who can access | “Server admins can access data on disk (and can reset your password). TravelPlan does not hide your vault from the host.” |
| Not a consumer cloud | “Only join servers and people you trust.” |
| HTTPS | Non-loopback `http://` is **blocked** (§3.8) — not a soft warn. |

**API support:** `GET /invites/{token}` and `GET /server/info` return `{ publicBaseUrl, serverName, adminDisplayNames: string[] }` (enabled admins only; no emails). Join + Settings show **server name** + **all** admin names.

**Ack:** Step 1 trust checkbox required before Continue (not buried on the password form). Optional: re-show a short trust banner in Settings while linked.

### 3.7 Delete account — **locked**

| Actor | Behavior |
|-------|----------|
| **Member** | Settings → **Delete account**. Confirm: export nudge → vault wiped → type email/DELETE. `DELETE /me`. |
| **Admin (sole, no other users)** | Same delete + “server will have no owner — re-seed.” |
| **Admin (sole, members remain)** | **Blocked** in Settings/admin — show promote / remove-members path (§3.4). |
| **Admin (another admin exists)** | May delete own account like a member (`DELETE /me`). |
| **Admin deletes another user** | Admin panel / CLI: purge; confirm nickname + email. |

Disable (§3.4) = soft; delete = hard purge. Prefer export nudge on every destructive path.

### 3.8 HTTPS — **locked (always)**

Passwords, JWTs, invite tokens, and recovery codes must not travel in cleartext on real networks. Same rule for **single-owner and multi-user**.

| Rule | Lock |
|------|------|
| **Client** | Self-host server URL must be `https://…`, **or** loopback `http://localhost` / `http://127.0.0.1` / `http://[::1]` (any port). All other `http://` (incl. LAN IPs) → **block** with clear copy |
| **`PUBLIC_BASE_URL`** | Must be `https://…` when set (invite links, Coolify public URL) |
| **Server** | Behind Coolify/Caddy/Traefik terminate TLS; honor `X-Forwarded-Proto`. Reject invite/recover/admin (and prefer reject login) when request is plain HTTP and not loopback |
| **Exceptions** | Loopback only. Optional `ALLOW_INSECURE_HTTP=true` for lab LAN (default **false**; README scare-comment) — not for normal friends/VPS use |
| **Phase 1** | No special carve-out for solo vault on `http://192.168…` — use HTTPS (or localhost) |

Do not document “LAN HTTP is fine for Phase 1” as a supported mode. Local dogfood = `http://127.0.0.1:8080`; real host = HTTPS.

### 3.9 Quotas — **locked**

| Rule | Lock |
|------|------|
| **Default** | `VAULT_QUOTA_MB` = server default for users with no override |
| **Per-user override** | `quota_bytes`: **`NULL`** = use default; **`0`** = **unlimited**; **`>0`** = personal cap (bytes). Admin UI: MiB input + “Unlimited” + “Use server default”. Hint on Unlimited: vault only limited by free disk. |
| **`MAX_USERS`** | Default **100**. Counts **all** user rows (incl. **disabled**); purge frees a seat. Enforced on **invite accept** and **CLI/admin create-user**. Creating a new invite while full: **allowed** (link can wait until a seat frees) but accept fails with clear error; admin list shows “N/100 users”. Env `0` = unlimited users (forks). |
| **Effective write limit** | Finite configured → `min(configured, usage + free disk)`. **`0` / unlimited** → disk floor only. Over finite quota → **413** `QUOTA_EXCEEDED`. No free disk → **507** `DISK_FULL`. |
| **Quota API shape** | **Canonical client read:** `GET /sync/quota` → `{ usageBytes, configuredLimitBytes \| null, effectiveLimitBytes \| null, mode, defaultLimitBytes }` (`null` limit = unlimited). `/me` may mirror. |
| **Admin PATCH quota** | Prefer `{ quotaMode: "default"\|"unlimited"\|"personal", quotaBytes?: number }` — avoid JSON `null`-vs-omit ambiguity. `personal` requires `quotaBytes > 0`. |
| **Admin UI** | Per user: usage, mode, effective limit; edit via `quotaMode` |
| **Member Settings** | See display matrix below; copy: **ask an admin** if they need more space / a higher limit |
| **Not Phase 2** | Billing, soft-quota email |

**Settings (member) display — test all cases:**

| Case | `quota_bytes` | Configured | Show (intent) |
|------|---------------|------------|----------------|
| A Server default | `NULL` | `VAULT_QUOTA_MB` > 0 | “{used} of {limit}” + meta “server default” |
| B Personal cap | `>0` | that value | “{used} of {limit}” + meta “personal limit” + “Ask an admin to change your quota” |
| C Unlimited | `0` | unlimited | “{used} · unlimited” + hint disk may still block + “Ask an admin if you need a set limit” |
| D Default unlimited* | `NULL` and `VAULT_QUOTA_MB=0` | unlimited | Same as C, meta “server default (unlimited)” |
| E Disk tighter | any finite | effective &lt; configured | Show **effective** as the hard stop; fine print “limited by free disk on the server” |
| F Near / over | — | — | Existing sync error path; Settings still shows usage; nudge ask admin |

\* Prefer shipping `VAULT_QUOTA_MB` &gt; 0 as default env; unlimited default is operator choice.

**Tests required:** API effective limit for NULL / 0 / positive / disk; 413 vs 507; accept + create-user blocked at `MAX_USERS` (disabled count); Settings A–F; admin `quotaMode` PATCH; concurrent accept / last-admin demote.

### 3.10 Multiple admins — **locked**

**N admins** allowed. Promote/demote in admin panel + CLI. Self-actions and last-admin rules in §3.4 / §3.7.

### 3.11 Server name — **locked**

Friendly host label so join/Settings is not only a raw URL.

| Rule | Lock |
|------|------|
| Env | `SERVER_NAME` (e.g. `Alex's home NAS`) |
| Required | No — if empty, UI falls back to hostname from `PUBLIC_BASE_URL` / server URL |
| Visibility | Join step 1, Settings trust, admin panel header, invite validate payload |
| Not | Auth identity; not unique; not a substitute for admin nicknames |
| Change | Restart / redeploy to pick up env (no DB column in Phase 2) |

---

## 4. Data model (SQLite)

Greenfield `server.db` only. **No schema upgrades** from earlier builds — wipe `DATA_DIR` when the shape changes (app/server not released).

### `users` (extend)

| Column | Notes |
|--------|--------|
| existing | `id`, `email`, `password_hash`, **`display_name`** (nickname — required, see §3.5), `created_at`, `updated_at` |
| `is_admin` | INTEGER 0/1 — **N admins allowed**; first Create sets true |
| `disabled_at` | TEXT NULL — if set, auth fails |
| `password_version` | INTEGER — bump on password change; embed in JWT claims to invalidate old tokens |
| `quota_bytes` | INTEGER NULL — `NULL` = server default; `0` = unlimited; `>0` = personal cap |
| `recovery_codes_acked_at` | TEXT NULL — set when user completes verify+ack; NULL → force codes UI on login |

**Remove** Phase 1 `Count >= 1` / `ErrOwnerExists` gate on Create (replace with invite-only or admin Create).

### `invites` (new)

| Column | Notes |
|--------|--------|
| `id` | UUID |
| `token_hash` | UNIQUE — store hash of raw token only |
| `created_by` | user id |
| `label` | optional |
| `expires_at` | RFC3339 |
| `max_uses` | always **1** (Phase 2) |
| `use_count` | |
| `created_at` | |
| `revoked_at` | NULL |

Default `expires_at` = now + `INVITE_TTL_DAYS` (7).

### `recovery_codes` (new)

| Column | Notes |
|--------|--------|
| `id` | |
| `user_id` | FK |
| `code_hash` | |
| `used_at` | NULL until consumed |
| `created_at` | |

### `password_reset_tokens` (new)

Same shape as invites (hash, expiry, single use), keyed to `user_id`. Created by admin; consumed by `/reset-password`.

Policy (locked): **10 recovery codes** per generation; regenerating deletes unused old rows.

---

## 5. API sketch

Base: `/api/v1`. Admin routes require Bearer + `is_admin` (re-checked from DB).

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/auth/login` | — | reject disabled; include `needsRecoveryCodesAck` |
| POST | `/auth/recover` | — | → **login-shaped tokens** |
| GET | `/password-reset/{token}` | — | `{ serverName, adminDisplayNames[], expiresAt }` |
| POST | `/password-reset/{token}` | — | → **login-shaped tokens** |
| GET | `/server/info` | — | `{ publicBaseUrl, serverName, adminDisplayNames[], userCount, maxUsers }` |
| GET | `/invites/{token}` | — | trust + `expiresAt` + `userCount`/`maxUsers` |
| POST | `/invites/{token}/accept` | — | **tokens + `recoveryCodes[]`** once; 409 email; 403 full; atomic |
| GET | `/me` | user | profile + `needsRecoveryCodesAck` + trust labels + quota mirror |
| PATCH | `/me` | user | `{ displayName }` |
| POST | `/me/password` | user | current+new; bump pwd; wrong current → 401 |
| POST | `/me/recovery-codes` | user | regenerate plaintext once; clear ack |
| POST | `/me/recovery-codes/ack` | user | set `recovery_codes_acked_at` |
| DELETE | `/me` | user | purge self; sole-admin rules |
| GET | `/users` | user | **enabled** only; `{ id, displayName }[]` by name; no emails |
| GET | `/sync/quota` | user | **canonical** Settings/sync quota (§3.9 shape) |
| GET | `/admin/users` | admin | incl. disabled |
| PATCH | `/admin/users/{id}` | admin | `isAdmin` / `disabled` / `quotaMode`; refuse last-enabled-admin disable/demote |
| DELETE | `/admin/users/{id}` | admin | purge other; refuse self |
| POST | `/admin/users/{id}/password` | admin | keep codes; revoke reset tokens |
| POST | `/admin/users/{id}/password-reset` | admin | create reset link |
| POST | `/admin/invites` | admin | one-time + expiresAt |
| GET | `/admin/invites` | admin | list |
| DELETE | `/admin/invites/{id}` | admin | revoke |

**Surfaces:** TravelPlan `/join` + `/reset-password` = canonical; server HTML = thin Coolify fallback; `/admin/` = own login (no app JWT).

---

## 6. CLI (extend `cmd/server`)

```text
create-user --email --password --display-name [--admin]   # display-name required
set-password --email --password
create-reset-link --email
list-users
set-quota --email --mb N|default|unlimited
disable-user --email          # refuse last enabled admin
enable-user --email
delete-user --email
promote-admin --email         # OK if disabled
demote-admin --email          # refuse last enabled admin
create-invite [--expires-days 7] [--label …]
revoke-invite --id
reset-recovery --email
```

Remove single-owner Create refusal; first user without `--admin` still gets `is_admin=true` if table empty.

---

## 7. Security

| Topic | Rule |
|-------|------|
| Invite tokens | ≥128-bit random; store hash only |
| Password-reset tokens | Same; shorter TTL (e.g. 1h); single use |
| Transport | **HTTPS always** except loopback (§3.8) |
| Invite / reset URLs | `server` param is non-secret; `token` is secret — never log full URLs |
| Recovery codes | Random; hash; **rate-limit** login/recover/accept/reset (required before release — not optional polish) |
| Display names | Public to enabled members; no emails on `GET /users` |
| Admin panel | Own login → JWT sessionStorage; CSP baseline |
| JWT | Claim `pwd` = `password_version`; reject mismatch |
| Admin flag | Re-read `is_admin` + `disabled_at` every admin route |
| Last enabled admin | Refuse demote **and** disable (self or other) under SQLite lock |
| Invites | Accept increments `use_count` atomically |
| Disable | Middleware on all protected routes incl. sync |
| Timing | Dummy bcrypt on unknown email for login + recover |

---

## 8. Client (TravelPlan) work

| Item | Notes |
|------|--------|
| `/join` wizard | Trust → account → codes; ack API; mid-join force on next login |
| Forgot password | **Server URL** + code **or** ask admin; `/reset-password` deep link |
| Settings | `/sync/quota` A–F; trust; password; regenerate+ack; displayName; sole-admin → Open admin |
| Delete account | Export CTA; empty-server re-seed copy when last user |
| Member list | Enabled nicknames only |
| Admin link | `{serverUrl}/admin/` (browser login) |

No shared-trip UI. Capability matrix: collaboration stays ❌; multi-account host becomes ✅ for `*-selfhost`.

---

## 9. Implementation phases

### P0 — Lift single-owner + schema

- [ ] `is_admin`, `disabled_at`, `password_version`, `quota_bytes`, `recovery_codes_acked_at`  
- [ ] Remove `ErrOwnerExists`; open register **403**  
- [ ] JWT `pwd` + middleware; live admin checks  
- [ ] Quota hard-cut: `0` = unlimited; 413 vs 507; `/sync/quota` shape  
- [ ] `MAX_USERS=100`, `SERVER_NAME`, TTL envs  
- [ ] CLI create-user (display-name required), list-users, …

**Accept:** two users via CLI; disabled 401; password bump invalidates token.

### P1 — Invites + trust + join

- [ ] Invites API + atomic accept + codes in accept response  
- [ ] Client `/join` + thin server HTML fallback  
- [ ] Trust: serverName + adminDisplayNames; step-1 full/expired  

**Accept:** join → codes → ack; mid-join abandon → force regenerate on login.

### P2 — Recovery + reset + rate limits

- [ ] `/auth/recover` (login-shaped) + cold server URL UI  
- [ ] Reset tokens + `/reset-password` (app + thin HTML)  
- [ ] Regenerate + ack endpoints  
- [ ] **Rate limits** on login/recover/accept/reset  

**Accept:** recover + reset link; rate limit trips in tests.

### P3 — Admin panel + delete + quotas UI

- [ ] `/admin/` login + users/invites/quota/promote/disable/enable/remove/reset  
- [ ] Last-enabled-admin refuse disable+demote  
- [ ] Client delete + Settings quota A–F + Open admin  
- [ ] README: re-seed empty server, Coolify, `SERVER_NAME`  

**Accept:** §14 matrix green; empty-server re-seed documented.

### P4 — Optional polish

- [ ] Audit log (admin-only)  
- [ ] `SERVER_NAME` editable without restart (later)  

---

## 10. README updates (when shipping)

Replace “single-owner only” with:

1. First boot still seeds **admin**.  
2. Set `SERVER_NAME` (friendly label) + `PUBLIC_BASE_URL`.  
3. Add friends via **invite** (panel or CLI).  
4. Users save **recovery codes**; forgot password = code **or** ask admin for reset link.  
5. Admin: promote others, remove users, see usage, set per-user quota (default / personal / **unlimited**).  
6. Max **100** users by default (`MAX_USERS`). Invites: **1 use**, TTL shown.  
7. Empty server after last user deleted → re-seed / `create-user` for first admin again.

Keep VPS **or** Coolify as one deploy step with two options.

---

## 11. Done definition

1. Admin invites friend → friend sees **server name** + **all admin names** + trust → syncs own vault.  
2. Friend resets password with **recovery code** while admin offline.  
3. Friend (or after losing codes) resets via **admin reset link**.  
4. Admin can promote/demote, disable/remove, reset links, see **usage**, set quota (incl. **unlimited**).  
5. Member Settings shows correct quota case; can ask admin to adjust.  
6. Member/admin delete respects sole-admin rules (§3.4).  
7. Invites: single-use + visible TTL; accept blocked at `MAX_USERS=100`.  
8. CLI covers critical ops; no SMTP.  
9. **§14 test matrix green** (server + client UI).  
10. Capability matrix updated.

---

## 12. Open questions

**None.** TTLs locked: reset **1h**, invite **7d**. Plan is build-ready; implement P0→P3.

---

## 13. Also consider (later — not Phase 2 blockers)

| Topic | Phase 2? |
|-------|----------|
| Volume backup / restore tool | README note only |
| Explicit “sign out everywhere” beyond `pwd` | later |
| Email change | later |
| Audit log | P4 optional |
| Shared trips / E2E encryption | ❌ |
| NTP / domain rename docs | README |
| Admin death → CLI emergency | README |

---

## 14. Testing — **required (complete)**

Hard-cut. Not done until green.

### 14.1 Server (Go)

| Area | Cases |
|------|--------|
| Authz | disabled on sync+admin; `pwd` bump; live `is_admin` after demote |
| Register | 403 if present |
| Invites | once; concurrent; expired; 409 email; MAX_USERS (disabled count); create-user cap; step payload userCount |
| Trust | serverName fallback; all enabled admin names |
| Codes | accept returns plaintext; ack sets timestamp; unacked login → regenerate; recover login-shaped; regenerate clears ack |
| Reset | validate payload; consume once; set-password keeps codes, revokes tokens |
| Admin | promote disabled OK; **disable last enabled admin refused**; concurrent demote; enable; purge; refuse delete self; admin login |
| Quota | NULL/0/>0; disk; 413 vs 507; `/sync/quota` mode shape; quotaMode PATCH |
| Empty server | last delete → seed/create-user works |
| Rate limit | login/recover/accept/reset |
| CLI | display-name required; disable last admin fails |

### 14.2 Client UI (TravelPlan) — unit + browser

| Flow | Cases |
|------|--------|
| `/join` | prefill; trust; full/expired on step 1; email taken; codes+ack; mid-join abandon → force; native deep link; http block |
| Forgot | cold **server URL** + recover; ask-admin copy |
| `/reset-password` | deep link; expired; success tokens |
| Settings | `/sync/quota` A–F; trust; password wrong/right; regenerate+ack; sole-admin → Open admin; delete last user |
| Directory | enabled only |
| i18n | all keys |

### 14.3 Admin panel

| Flow | Cases |
|------|--------|
| Unauthenticated → login → JWT | |
| Member rejected | |
| Quota modes; invite TTL shown; reset link; enable/disable; last-admin refuse | |
| Header SERVER_NAME | |

### 14.4 Done bar

`go test ./...` + client unit + `npm run test:browser` + Docker smoke (invite→join→sync→recover→reset→quota).
