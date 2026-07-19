package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
	"github.com/google/uuid"
)

type acceptInviteRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type ackRecoveryCodesRequest struct {
	Generation int `json:"generation"`
}

func (a *API) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var req acceptInviteRequest
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if !a.checkAuthRateLimit(w, r, "accept", auth.NormalizeEmail(req.Email)) {
		return
	}
	if req.Email == "" || req.Password == "" || req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "email, password, and displayName are required")
		return
	}

	userID := uuid.NewString()
	tokenStr, expiresIn, err := a.tokens.Issue(userID, auth.NormalizeEmail(req.Email), 0)
	if err != nil {
		a.log.Error("issue token before accept", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not issue token")
		return
	}

	u, codes, generation, err := a.users.AcceptInvite(
		r.Context(), token, req.Email, req.Password, req.DisplayName, a.cfg.MaxUsers, userID,
	)
	if err != nil {
		writeAcceptInviteError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{
		"accessToken":              tokenStr,
		"expiresIn":                expiresIn,
		"needsRecoveryCodesAck":    true,
		"recoveryCodes":            codes,
		"recoveryCodesGeneration":  generation,
		"user": map[string]string{
			"id":          u.ID,
			"email":       u.Email,
			"displayName": u.DisplayName,
		},
	})
}

func writeAcceptInviteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInviteNotFound):
		writeError(w, http.StatusNotFound, "INVITE_NOT_FOUND", "invite not found")
	case errors.Is(err, auth.ErrInviteExpired):
		writeError(w, http.StatusGone, "INVITE_EXPIRED", "invite expired")
	case errors.Is(err, auth.ErrInviteRevoked):
		writeError(w, http.StatusGone, "INVITE_REVOKED", "invite revoked")
	case errors.Is(err, auth.ErrInviteUsed):
		writeError(w, http.StatusGone, "INVITE_USED", "invite already used")
	case errors.Is(err, auth.ErrConflict):
		writeError(w, http.StatusConflict, "EMAIL_TAKEN", "email already registered")
	case errors.Is(err, auth.ErrUserLimit):
		writeError(w, http.StatusForbidden, "SERVER_FULL", "user limit reached")
	case errors.Is(err, auth.ErrDisplayNameRequired):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "displayName is required")
	default:
		if strings.Contains(err.Error(), "password must") {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func (a *API) handleRegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	codes, generation, err := a.users.RegenerateRecoveryCodes(r.Context(), userID)
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found")
		return
	}
	if err != nil {
		a.log.Error("regenerate recovery codes", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"recoveryCodes":           codes,
		"recoveryCodesGeneration": generation,
		"needsRecoveryCodesAck":   true,
	})
}

func (a *API) handleAckRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	var req ackRecoveryCodesRequest
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	err := a.users.AckRecoveryCodes(r.Context(), userID, req.Generation)
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found")
		return
	}
	if errors.Is(err, auth.ErrAckGenerationMismatch) {
		writeError(w, http.StatusConflict, "GENERATION_MISMATCH", "recovery codes changed; regenerate and try again")
		return
	}
	if errors.Is(err, auth.ErrAckNoCodes) {
		writeError(w, http.StatusBadRequest, "NO_RECOVERY_CODES", "regenerate recovery codes before acknowledging")
		return
	}
	if err != nil {
		a.log.Error("ack recovery codes", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"needsRecoveryCodesAck": false,
	})
}
