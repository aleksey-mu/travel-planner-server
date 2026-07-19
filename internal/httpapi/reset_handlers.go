package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
)

type recoverRequest struct {
	Email        string `json:"email"`
	RecoveryCode string `json:"recoveryCode"`
	Password     string `json:"password"`
}

type passwordResetBody struct {
	Password string `json:"password"`
}

func (a *API) handleRecover(w http.ResponseWriter, r *http.Request) {
	var req recoverRequest
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if !a.checkAuthRateLimit(w, r, "recover", auth.NormalizeEmail(req.Email)) {
		return
	}
	if req.Email == "" || req.RecoveryCode == "" || req.Password == "" {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or recovery code")
		return
	}
	u, err := a.users.RecoverWithCode(r.Context(), req.Email, req.RecoveryCode, req.Password)
	if err != nil {
		writeRecoverError(w, err)
		return
	}
	a.writeLoginShaped(w, http.StatusOK, u)
}

func writeRecoverError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound), errors.Is(err, auth.ErrRecoveryCodeInvalid):
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or recovery code")
	default:
		if strings.Contains(err.Error(), "password must") {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func (a *API) writeLoginShaped(w http.ResponseWriter, status int, u *auth.User) {
	token, expiresIn, err := a.tokens.Issue(u.ID, u.Email, u.PasswordVersion)
	if err != nil {
		a.log.Error("issue token", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not issue token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{
		"accessToken":           token,
		"expiresIn":             expiresIn,
		"needsRecoveryCodesAck": u.NeedsRecoveryCodesAck(),
		"user": map[string]string{
			"id":          u.ID,
			"email":       u.Email,
			"displayName": u.DisplayName,
		},
	})
}

func (a *API) handleGetPasswordReset(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("token")
	if !a.checkAuthRateLimit(w, r, "password-reset", rateLimitIdentityHash(raw)) {
		return
	}
	tok, err := a.users.GetPasswordResetToken(r.Context(), raw)
	if err != nil {
		writeResetValidateError(w, err)
		return
	}
	names, err := a.users.ListEnabledAdminDisplayNames(r.Context())
	if err != nil {
		a.log.Error("reset trust", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	_ = tok // validated
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"serverName":        a.cfg.EffectiveServerName(),
		"adminDisplayNames": names,
		"expiresAt":         tok.ExpiresAt,
	})
}

func (a *API) handleCompletePasswordReset(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("token")
	if !a.checkAuthRateLimit(w, r, "password-reset", rateLimitIdentityHash(raw)) {
		return
	}
	var req passwordResetBody
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "password is required")
		return
	}
	u, err := a.users.CompletePasswordReset(r.Context(), raw, req.Password)
	if err != nil {
		writeResetValidateError(w, err)
		return
	}
	a.writeLoginShaped(w, http.StatusOK, u)
}

func writeResetValidateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrResetNotFound):
		writeError(w, http.StatusNotFound, "RESET_NOT_FOUND", "password reset not found")
	case errors.Is(err, auth.ErrResetExpired):
		writeError(w, http.StatusGone, "RESET_EXPIRED", "password reset expired")
	case errors.Is(err, auth.ErrResetUsed):
		writeError(w, http.StatusGone, "RESET_USED", "password reset already used")
	case errors.Is(err, auth.ErrResetRevoked):
		writeError(w, http.StatusGone, "RESET_REVOKED", "password reset revoked")
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "RESET_NOT_FOUND", "password reset not found")
	default:
		if strings.Contains(err.Error(), "password must") {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func (a *API) handleAdminCreatePasswordReset(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	created, err := a.users.CreatePasswordResetToken(r.Context(), userID, a.cfg.PasswordResetTTLSeconds)
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	}
	if err != nil {
		a.log.Error("create password reset", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not create reset link")
		return
	}
	resp := map[string]any{
		"id":        created.Token.ID,
		"token":     created.RawToken,
		"expiresAt": created.Token.ExpiresAt,
		"createdAt": created.Token.CreatedAt,
	}
	if a.cfg.PublicBaseURL != "" {
		resp["resetUrl"] = strings.TrimSuffix(a.cfg.PublicBaseURL, "/") +
			"/reset-password/?token=" + url.QueryEscape(created.RawToken)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, resp)
}
