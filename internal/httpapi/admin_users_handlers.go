package httpapi

import (
	"errors"
	"net/http"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
)

type adminUserPatchRequest struct {
	IsAdmin    *bool   `json:"isAdmin"`
	Disabled   *bool   `json:"disabled"`
	QuotaMode  *string `json:"quotaMode"`
	QuotaBytes *int64  `json:"quotaBytes"`
}

func (a *API) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := a.users.ListAllUsers(r.Context())
	if err != nil {
		a.log.Error("list users", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, u := range list {
		items = append(items, a.adminUserJSON(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": items})
}

func (a *API) adminUserJSON(u *auth.User) map[string]any {
	qv, err := a.store.BuildQuotaForUser(u.ID, u.QuotaBytes, a.cfg.DefaultQuotaBytes)
	if err != nil {
		qv.Mode = "default"
	}
	return map[string]any{
		"id":                   u.ID,
		"email":                u.Email,
		"displayName":          u.DisplayName,
		"isAdmin":              u.IsAdmin,
		"disabled":             u.IsDisabled(),
		"usageBytes":           qv.UsageBytes,
		"configuredLimitBytes": qv.ConfiguredLimitBytes,
		"effectiveLimitBytes":  qv.EffectiveLimitBytes,
		"mode":                 qv.Mode,
		"defaultLimitBytes":    qv.DefaultLimitBytes,
	}
}

func (a *API) handleAdminPatchUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req adminUserPatchRequest
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	err := a.users.ApplyAdminUserPatch(r.Context(), id, auth.AdminUserPatch{
		IsAdmin:    req.IsAdmin,
		Disabled:   req.Disabled,
		QuotaMode:  req.QuotaMode,
		QuotaBytes: req.QuotaBytes,
	})
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	case errors.Is(err, auth.ErrLastEnabledAdmin):
		writeError(w, http.StatusConflict, "LAST_ENABLED_ADMIN", "cannot disable or demote the last enabled admin")
		return
	case errors.Is(err, auth.ErrInvalidQuotaMode):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	case err != nil:
		a.log.Error("patch user", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	u, err := a.users.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, a.adminUserJSON(u))
}

func (a *API) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor := auth.UserIDFromContext(r.Context())
	if id == actor {
		writeError(w, http.StatusBadRequest, "CANNOT_DELETE_SELF", "use DELETE /me to delete your own account")
		return
	}
	if err := a.users.DeleteUserHard(r.Context(), id); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		if errors.Is(err, auth.ErrLastEnabledAdmin) {
			writeError(w, http.StatusConflict, "LAST_ENABLED_ADMIN", "cannot delete the last enabled admin while other users remain")
			return
		}
		a.log.Error("admin delete user", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if err := a.store.PurgeUser(id); err != nil {
		a.log.Error("purge user vault after delete", "userId", id, "err", err)
		writeError(w, http.StatusInternalServerError, "PURGE_FAILED", "user deleted but vault purge failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleDeleteMe(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	err := a.users.DeleteSelf(r.Context(), userID)
	if errors.Is(err, auth.ErrSoleAdminWithMembers) {
		writeError(w, http.StatusConflict, "SOLE_ADMIN_WITH_MEMBERS", "promote another admin or remove members first")
		return
	}
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found")
		return
	}
	if err != nil {
		a.log.Error("delete me", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if err := a.store.PurgeUser(userID); err != nil {
		a.log.Error("purge user vault after delete me", "userId", userID, "err", err)
		writeError(w, http.StatusInternalServerError, "PURGE_FAILED", "account deleted but vault purge failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := a.users.ListEnabledDirectory(r.Context())
	if err != nil {
		a.log.Error("directory", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": list})
}
