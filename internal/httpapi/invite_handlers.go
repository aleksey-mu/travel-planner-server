package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
)

type createInviteRequest struct {
	Label       string `json:"label"`
	ExpiresDays *int   `json:"expiresDays"`
}

type trustPayload struct {
	PublicBaseURL      string   `json:"publicBaseUrl"`
	ServerName         string   `json:"serverName"`
	AdminDisplayNames  []string `json:"adminDisplayNames"`
	UserCount          int64    `json:"userCount"`
	MaxUsers           int      `json:"maxUsers"`
}

func (a *API) buildTrustPayload(r *http.Request) (trustPayload, error) {
	names, err := a.users.ListEnabledAdminDisplayNames(r.Context())
	if err != nil {
		return trustPayload{}, err
	}
	n, err := a.users.Count(r.Context())
	if err != nil {
		return trustPayload{}, err
	}
	return trustPayload{
		PublicBaseURL:     a.cfg.PublicBaseURL,
		ServerName:        a.cfg.EffectiveServerName(),
		AdminDisplayNames: names,
		UserCount:         n,
		MaxUsers:          a.cfg.MaxUsers,
	}, nil
}

func (a *API) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	p, err := a.buildTrustPayload(r)
	if err != nil {
		a.log.Error("server info", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) handleGetInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, err := a.users.ValidateInviteToken(r.Context(), token)
	if err != nil {
		writeInviteValidateError(w, err)
		return
	}
	p, err := a.buildTrustPayload(r)
	if err != nil {
		a.log.Error("invite trust", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"publicBaseUrl":     p.PublicBaseURL,
		"serverName":        p.ServerName,
		"adminDisplayNames": p.AdminDisplayNames,
		"userCount":         p.UserCount,
		"maxUsers":          p.MaxUsers,
		"expiresAt":         inv.ExpiresAt,
	})
}

func writeInviteValidateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInviteNotFound):
		writeError(w, http.StatusNotFound, "INVITE_NOT_FOUND", "invite not found")
	case errors.Is(err, auth.ErrInviteExpired):
		writeError(w, http.StatusGone, "INVITE_EXPIRED", "invite expired")
	case errors.Is(err, auth.ErrInviteRevoked):
		writeError(w, http.StatusGone, "INVITE_REVOKED", "invite revoked")
	case errors.Is(err, auth.ErrInviteUsed):
		writeError(w, http.StatusGone, "INVITE_USED", "invite already used")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func (a *API) handleAdminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var req createInviteRequest
	if r.ContentLength != 0 {
		if !decodeAuthJSON(w, r, &req) {
			return
		}
	}
	days := a.cfg.InviteTTLDays
	if req.ExpiresDays != nil {
		if *req.ExpiresDays <= 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "expiresDays must be positive")
			return
		}
		days = *req.ExpiresDays
	}
	createdBy := auth.UserIDFromContext(r.Context())
	created, err := a.users.CreateInvite(r.Context(), createdBy, req.Label, days)
	if err != nil {
		a.log.Error("create invite", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not create invite")
		return
	}
	writeJSON(w, http.StatusCreated, inviteCreateResponse(a.cfg.PublicBaseURL, created))
}

func (a *API) handleAdminListInvites(w http.ResponseWriter, r *http.Request) {
	list, err := a.users.ListInvites(r.Context())
	if err != nil {
		a.log.Error("list invites", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, inv := range list {
		items = append(items, inviteListItem(inv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": items})
}

func (a *API) handleAdminRevokeInvite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id required")
		return
	}
	err := a.users.RevokeInvite(r.Context(), id)
	if errors.Is(err, auth.ErrInviteNotFound) {
		writeError(w, http.StatusNotFound, "INVITE_NOT_FOUND", "invite not found")
		return
	}
	if err != nil {
		a.log.Error("revoke invite", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func inviteListItem(inv *auth.Invite) map[string]any {
	item := map[string]any{
		"id":        inv.ID,
		"label":     inv.Label,
		"expiresAt": inv.ExpiresAt,
		"maxUses":   inv.MaxUses,
		"useCount":  inv.UseCount,
		"createdAt": inv.CreatedAt,
		"revokedAt": nil,
	}
	if inv.RevokedAt.Valid {
		item["revokedAt"] = inv.RevokedAt.String
	}
	return item
}

func inviteCreateResponse(publicBase string, created *auth.CreatedInvite) map[string]any {
	inv := created.Invite
	resp := map[string]any{
		"id":        inv.ID,
		"token":     created.RawToken,
		"label":     inv.Label,
		"expiresAt": inv.ExpiresAt,
		"maxUses":   inv.MaxUses,
		"useCount":  inv.UseCount,
		"createdAt": inv.CreatedAt,
	}
	if publicBase != "" {
		resp["inviteUrl"] = strings.TrimSuffix(publicBase, "/") + "/join?token=" + url.QueryEscape(created.RawToken)
	}
	return resp
}
