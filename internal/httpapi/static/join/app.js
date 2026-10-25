(() => {
  const params = new URLSearchParams(location.search);
  const el = (id) => document.getElementById(id);
  let invite = null;
  let accessToken = null;
  let generation = null;
  let recoveryCodes = [];

  const api = (path, opts = {}) => {
    const headers = Object.assign({ Accept: "application/json" }, opts.headers || {});
    if (accessToken) headers.Authorization = "Bearer " + accessToken;
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

  const tokenFromQuery = params.get("token") || "";
  el("token").value = tokenFromQuery;
  if (tokenFromQuery && history.replaceState) {
    const clean = location.pathname + location.hash;
    history.replaceState({}, "", clean);
  }

  const isLoopback = () => {
    const h = location.hostname;
    return h === "localhost" || h === "127.0.0.1" || h === "::1" || h === "[::1]";
  };

  if (location.protocol === "http:" && !isLoopback()) {
    el("trust-error").textContent =
      "This join page requires HTTPS (or loopback). Open the invite on the https:// URL for this server.";
    el("btn-load").disabled = true;
  }

  const updateAckEnabled = () => {
    const typed = el("code-check").value.trim().toUpperCase().replace(/\s+/g, "");
    const okCode = recoveryCodes.some((c) => c.replace(/\s+/g, "").toUpperCase() === typed);
    el("btn-ack").disabled = !(el("codes-saved").checked && okCode);
  };

  el("btn-load").onclick = async () => {
    el("trust-error").textContent = "";
    el("trust-box").classList.add("hidden");
    const token = el("token").value.trim();
    if (!token) {
      el("trust-error").textContent = "Invite token is required.";
      return;
    }
    try {
      invite = await api("/invites/" + encodeURIComponent(token));
      const name = invite.serverName || location.host;
      const admins = (invite.adminDisplayNames || []).join(", ") || "(no admins listed)";
      el("trust-summary").textContent = name + " — admins: " + admins;
      el("trust-expires").textContent = "Invite expires " + (invite.expiresAt || "—");
      const full =
        invite.maxUsers > 0 && invite.userCount != null && invite.userCount >= invite.maxUsers;
      el("trust-full").classList.toggle("hidden", !full);
      el("btn-continue").disabled = full || !el("trust-ok").checked;
      el("trust-box").classList.remove("hidden");
    } catch (e) {
      el("trust-error").textContent = e.message || "Invite not valid";
    }
  };

  el("trust-ok").onchange = () => {
    const full = !el("trust-full").classList.contains("hidden");
    el("btn-continue").disabled = full || !el("trust-ok").checked;
  };

  el("btn-continue").onclick = () => {
    el("step-account").classList.remove("hidden");
    el("step-account").scrollIntoView({ behavior: "smooth" });
  };

  el("btn-accept").onclick = async () => {
    el("account-error").textContent = "";
    const p1 = el("password").value;
    const p2 = el("password2").value;
    if (!p1 || p1 !== p2) {
      el("account-error").textContent = "Passwords must match.";
      return;
    }
    const token = el("token").value.trim();
    try {
      const res = await api("/invites/" + encodeURIComponent(token) + "/accept", {
        method: "POST",
        body: JSON.stringify({
          email: el("email").value.trim(),
          password: p1,
          displayName: el("display-name").value.trim(),
        }),
      });
      accessToken = res.accessToken;
      generation = res.recoveryCodesGeneration;
      recoveryCodes = res.recoveryCodes || [];
      el("codes").textContent = recoveryCodes.join("\n");
      el("password").value = "";
      el("password2").value = "";
      el("token").value = "";
      el("step-codes").classList.remove("hidden");
      el("step-codes").scrollIntoView({ behavior: "smooth" });
      el("btn-accept").disabled = true;
      updateAckEnabled();
    } catch (e) {
      el("account-error").textContent = e.message || "Could not join";
    }
  };

  el("codes-saved").onchange = updateAckEnabled;
  el("code-check").oninput = updateAckEnabled;

  el("btn-ack").onclick = async () => {
    el("codes-error").textContent = "";
    try {
      await api("/me/recovery-codes/ack", {
        method: "POST",
        body: JSON.stringify({ generation: generation }),
      });
      el("codes-done").classList.remove("hidden");
      el("btn-ack").disabled = true;
      accessToken = null;
      recoveryCodes = [];
      el("codes").textContent = "";
      el("code-check").value = "";
    } catch (e) {
      el("codes-error").textContent = e.message || "Could not acknowledge codes";
    }
  };

  if (el("token").value.trim()) {
    el("btn-load").click();
  }
})();
