(() => {
  const TOKEN_KEY = "travelplan_admin_token";
  const api = (path, opts = {}) => {
    const headers = Object.assign({ Accept: "application/json" }, opts.headers || {});
    const token = sessionStorage.getItem(TOKEN_KEY);
    if (token) headers.Authorization = "Bearer " + token;
    if (opts.body && !headers["Content-Type"]) headers["Content-Type"] = "application/json";
    return fetch(path, Object.assign({}, opts, { headers })).then(async (res) => {
      const text = await res.text();
      let data = null;
      if (text) {
        try {
          data = JSON.parse(text);
        } catch (_) {
          data = { raw: text };
        }
      }
      if (!res.ok) {
        const err = new Error((data && data.error && data.error.message) || res.statusText);
        err.code = data && data.error && data.error.code;
        err.status = res.status;
        throw err;
      }
      return data;
    });
  };

  const el = (id) => document.getElementById(id);
  const fmtBytes = (n) => {
    if (n == null) return "—";
    const u = ["B", "KiB", "MiB", "GiB"];
    let v = Number(n);
    let i = 0;
    while (v >= 1024 && i < u.length - 1) {
      v /= 1024;
      i++;
    }
    return (i === 0 ? String(v) : v.toFixed(v >= 10 ? 0 : 1)) + " " + u[i];
  };
  const fmtLimit = (v) => (v == null ? "unlimited" : fmtBytes(v));

  let meId = null;

  function clearPanelSecrets() {
    el("users").innerHTML = "";
    el("invites").innerHTML = "";
    el("invite-flash").textContent = "";
    el("invite-flash").classList.add("hidden");
    el("panel-error").textContent = "";
    el("user-count").textContent = "";
  }

  function showLogin() {
    clearPanelSecrets();
    el("view-login").classList.remove("hidden");
    el("view-panel").classList.add("hidden");
    el("btn-logout").classList.add("hidden");
  }
  function showPanel() {
    el("view-login").classList.add("hidden");
    el("view-panel").classList.remove("hidden");
    el("btn-logout").classList.remove("hidden");
  }

  async function loadServerLabel() {
    try {
      const info = await api("/api/v1/server/info");
      const name = info.serverName || "(unnamed server)";
      const cap = info.maxUsers === 0 ? "unlimited" : String(info.maxUsers);
      el("server-label").textContent = name + " · " + info.userCount + "/" + cap + " users";
    } catch (_) {
      el("server-label").textContent = "";
    }
  }

  async function ensureAdminSession() {
    const token = sessionStorage.getItem(TOKEN_KEY);
    if (!token) {
      showLogin();
      return false;
    }
    try {
      const me = await api("/api/v1/me");
      meId = me.id;
      await api("/api/v1/admin/users");
      showPanel();
      return true;
    } catch (e) {
      sessionStorage.removeItem(TOKEN_KEY);
      showLogin();
      if (e.status === 403) {
        el("login-error").textContent = "This account is not an admin.";
      }
      return false;
    }
  }

  async function refresh() {
    el("panel-error").textContent = "";
    const [usersRes, invitesRes] = await Promise.all([
      api("/api/v1/admin/users"),
      api("/api/v1/admin/invites"),
    ]);
    renderUsers(usersRes.users || []);
    renderInvites(invitesRes.invites || []);
    await loadServerLabel();
  }

  function renderUsers(users) {
    el("user-count").textContent = users.length + " accounts";
    const root = el("users");
    root.innerHTML = "";
    const enabledAdmins = users.filter((u) => u.isAdmin && !u.disabled).length;
    users.forEach((u) => {
      const row = document.createElement("div");
      row.className = "user";

      const top = document.createElement("div");
      top.className = "user-top";
      const left = document.createElement("div");
      const name = document.createElement("span");
      name.className = "name";
      name.textContent = u.displayName || "(no name)";
      left.appendChild(name);
      left.appendChild(document.createTextNode(" "));
      const addBadge = (text, cls) => {
        const b = document.createElement("span");
        b.className = "badge" + (cls ? " " + cls : "");
        b.textContent = text;
        left.appendChild(b);
        left.appendChild(document.createTextNode(" "));
      };
      if (u.isAdmin) addBadge("admin", "admin");
      if (u.disabled) addBadge("disabled", "disabled");
      addBadge(u.mode || "default", "");
      const email = document.createElement("div");
      email.className = "meta";
      email.textContent = u.email;
      left.appendChild(email);
      top.appendChild(left);
      row.appendChild(top);

      const usage = document.createElement("div");
      usage.className = "meta usage";
      usage.textContent =
        "Usage " +
        fmtBytes(u.usageBytes) +
        " · configured " +
        fmtLimit(u.configuredLimitBytes) +
        " · effective " +
        fmtLimit(u.effectiveLimitBytes);
      row.appendChild(usage);

      const quota = document.createElement("div");
      quota.className = "quota-row";
      const modeSel = document.createElement("select");
      ["default", "unlimited", "personal"].forEach((m) => {
        const o = document.createElement("option");
        o.value = m;
        o.textContent = m;
        if (m === u.mode) o.selected = true;
        modeSel.appendChild(o);
      });
      const mibIn = document.createElement("input");
      mibIn.type = "number";
      mibIn.min = "1";
      mibIn.step = "1";
      mibIn.placeholder = "MiB (personal)";
      mibIn.style.width = "9rem";
      if (u.mode === "personal" && u.configuredLimitBytes != null) {
        mibIn.value = String(Math.max(1, Math.round(Number(u.configuredLimitBytes) / (1024 * 1024))));
      }
      const applyQuota = document.createElement("button");
      applyQuota.type = "button";
      applyQuota.className = "xs primary";
      applyQuota.textContent = "Set quota";
      applyQuota.onclick = async () => {
        try {
          const body = { quotaMode: modeSel.value };
          if (modeSel.value === "personal") {
            const mib = Number(mibIn.value);
            if (!mib || mib <= 0) throw new Error("personal requires MiB > 0");
            body.quotaBytes = Math.floor(mib * 1024 * 1024);
          }
          await api("/api/v1/admin/users/" + encodeURIComponent(u.id), {
            method: "PATCH",
            body: JSON.stringify(body),
          });
          await refresh();
        } catch (e) {
          el("panel-error").textContent = e.message;
        }
      };
      quota.append(modeSel, mibIn, applyQuota);
      row.appendChild(quota);

      const actions = document.createElement("div");
      actions.className = "actions";
      const addBtn = (label, cls, fn) => {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "xs " + (cls || "");
        b.textContent = label;
        b.onclick = fn;
        actions.appendChild(b);
      };
      const canSelfAdminAction = u.id !== meId || enabledAdmins > 1;
      if (!u.isAdmin) {
        addBtn("Promote", "", async () => {
          try {
            await api("/api/v1/admin/users/" + encodeURIComponent(u.id), {
              method: "PATCH",
              body: JSON.stringify({ isAdmin: true }),
            });
            await refresh();
          } catch (e) {
            el("panel-error").textContent = e.message;
          }
        });
      } else if (canSelfAdminAction) {
        addBtn("Demote", "", async () => {
          try {
            await api("/api/v1/admin/users/" + encodeURIComponent(u.id), {
              method: "PATCH",
              body: JSON.stringify({ isAdmin: false }),
            });
            if (u.id === meId) {
              sessionStorage.removeItem(TOKEN_KEY);
              meId = null;
              showLogin();
              el("login-error").textContent = "You demoted yourself; sign in again if still an admin.";
              return;
            }
            await refresh();
          } catch (e) {
            el("panel-error").textContent = e.message;
          }
        });
      }
      if (u.disabled) {
        addBtn("Enable", "", async () => {
          try {
            await api("/api/v1/admin/users/" + encodeURIComponent(u.id), {
              method: "PATCH",
              body: JSON.stringify({ disabled: false }),
            });
            await refresh();
          } catch (e) {
            el("panel-error").textContent = e.message;
          }
        });
      } else if (canSelfAdminAction) {
        addBtn("Disable", "", async () => {
          try {
            await api("/api/v1/admin/users/" + encodeURIComponent(u.id), {
              method: "PATCH",
              body: JSON.stringify({ disabled: true }),
            });
            if (u.id === meId) {
              sessionStorage.removeItem(TOKEN_KEY);
              meId = null;
              showLogin();
              el("login-error").textContent = "Account disabled.";
              return;
            }
            await refresh();
          } catch (e) {
            el("panel-error").textContent = e.message;
          }
        });
      }
      addBtn("Reset link", "", async () => {
        try {
          const res = await api("/api/v1/admin/users/" + encodeURIComponent(u.id) + "/password-reset", {
            method: "POST",
          });
          const flash = el("invite-flash");
          flash.classList.remove("hidden");
          flash.textContent =
            "Reset link for " +
            u.email +
            ": " +
            (res.resetUrl || res.token || JSON.stringify(res));
        } catch (e) {
          el("panel-error").textContent = e.message;
        }
      });
      if (u.id !== meId) {
        addBtn("Remove", "danger", async () => {
          const ok = confirm("Delete " + u.displayName + " (" + u.email + ") permanently?");
          if (!ok) return;
          try {
            await api("/api/v1/admin/users/" + encodeURIComponent(u.id), { method: "DELETE" });
            await refresh();
          } catch (e) {
            el("panel-error").textContent = e.message;
          }
        });
      }
      row.appendChild(actions);
      root.appendChild(row);
    });
  }

  function renderInvites(invites) {
    const root = el("invites");
    root.innerHTML = "";
    if (!invites.length) {
      root.innerHTML = '<p class="hint">No invites yet.</p>';
      return;
    }
    invites.forEach((inv) => {
      const row = document.createElement("div");
      row.className = "invite";
      const status = inv.revokedAt
        ? "revoked"
        : inv.usedAt || (inv.useCount && inv.useCount > 0)
          ? "used"
          : "open";
      row.innerHTML =
        '<div class="invite-top"><div><span class="name"></span> <span class="badge"></span>' +
        '<div class="meta"></div></div><div class="actions"></div></div>';
      row.querySelector(".name").textContent = inv.label || inv.id;
      row.querySelector(".badge").textContent = status;
      row.querySelector(".meta").textContent = "Expires " + (inv.expiresAt || "—");
      if (status === "open") {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "xs danger";
        b.textContent = "Revoke";
        b.onclick = async () => {
          try {
            await api("/api/v1/admin/invites/" + encodeURIComponent(inv.id), { method: "DELETE" });
            await refresh();
          } catch (e) {
            el("panel-error").textContent = e.message;
          }
        };
        row.querySelector(".actions").appendChild(b);
      }
      root.appendChild(row);
    });
  }

  el("btn-login").onclick = async () => {
    el("login-error").textContent = "";
    try {
      const data = await api("/api/v1/auth/login", {
        method: "POST",
        body: JSON.stringify({
          email: el("login-email").value.trim(),
          password: el("login-password").value,
        }),
      });
      sessionStorage.setItem(TOKEN_KEY, data.accessToken);
      meId = data.user && data.user.id;
      await api("/api/v1/admin/users");
      el("login-password").value = "";
      showPanel();
      await refresh();
    } catch (e) {
      sessionStorage.removeItem(TOKEN_KEY);
      if (e.status === 403) {
        el("login-error").textContent = "This account is not an admin.";
      } else {
        el("login-error").textContent = e.message || "Sign-in failed";
      }
    }
  };

  el("btn-logout").onclick = () => {
    sessionStorage.removeItem(TOKEN_KEY);
    meId = null;
    showLogin();
  };

  el("btn-create-invite").onclick = async () => {
    try {
      const res = await api("/api/v1/admin/invites", {
        method: "POST",
        body: JSON.stringify({}),
      });
      const flash = el("invite-flash");
      flash.classList.remove("hidden");
      flash.textContent =
        "Invite created. Copy now (token shown once): " +
        (res.inviteUrl || res.token || JSON.stringify(res));
      await refresh();
    } catch (e) {
      el("panel-error").textContent = e.message;
    }
  };

  loadServerLabel();
  ensureAdminSession().then((ok) => {
    if (ok) return refresh();
  });
})();
