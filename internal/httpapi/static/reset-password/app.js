(() => {
  const params = new URLSearchParams(location.search);
  const el = (id) => document.getElementById(id);

  const api = (path, opts = {}) => {
    const headers = Object.assign({ Accept: "application/json" }, opts.headers || {});
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

  el("token").value = params.get("token") || "";
  if (el("token").value && history.replaceState) {
    history.replaceState({}, "", location.pathname + location.hash);
  }

  const isLoopback = () => {
    const h = location.hostname;
    return h === "localhost" || h === "127.0.0.1" || h === "::1" || h === "[::1]";
  };
  if (location.protocol === "http:" && !isLoopback()) {
    el("load-error").textContent =
      "Password reset requires HTTPS (or loopback). Open the link on the https:// URL for this server.";
    el("btn-load").disabled = true;
  }

  el("btn-load").onclick = async () => {
    el("load-error").textContent = "";
    el("info").classList.add("hidden");
    el("form").classList.add("hidden");
    const token = el("token").value.trim();
    if (!token) {
      el("load-error").textContent = "Reset token is required.";
      return;
    }
    try {
      const info = await api("/api/v1/password-reset/" + encodeURIComponent(token));
      const name = info.serverName || location.host;
      const admins = (info.adminDisplayNames || []).join(", ") || "(no admins listed)";
      el("summary").textContent = name + " — admins: " + admins;
      el("expires").textContent = "Link expires " + (info.expiresAt || "—");
      el("info").classList.remove("hidden");
      el("form").classList.remove("hidden");
    } catch (e) {
      el("load-error").textContent = e.message || "Reset link not valid";
    }
  };

  el("btn-save").onclick = async () => {
    el("form-error").textContent = "";
    const p1 = el("password").value;
    const p2 = el("password2").value;
    if (!p1 || p1 !== p2) {
      el("form-error").textContent = "Passwords must match.";
      return;
    }
    try {
      await api("/api/v1/password-reset/" + encodeURIComponent(el("token").value.trim()), {
        method: "POST",
        body: JSON.stringify({ password: p1 }),
      });
      el("done").classList.remove("hidden");
      el("btn-save").disabled = true;
      el("password").value = "";
      el("password2").value = "";
      el("token").value = "";
    } catch (e) {
      el("form-error").textContent = e.message || "Could not reset password";
    }
  };

  if (el("token").value.trim()) {
    el("btn-load").click();
  }
})();
