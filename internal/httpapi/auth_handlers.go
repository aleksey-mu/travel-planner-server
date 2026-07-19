package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
	"github.com/aleksey-mu/travel-planner-server/internal/version"
)

const authBodyLimit = 64 << 10 // 64 KiB

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version.Version,
	})
}

func decodeAuthJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, authBodyLimit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json body")
		return false
	}
	return true
}

func (a *API) handleRegister(w http.ResponseWriter, _ *http.Request) {
	// Closed registration — invites / CLI only (hard-cut; ALLOW_REGISTER ignored).
	writeError(w, http.StatusForbidden, "REGISTER_DISABLED", "registration is disabled")
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeAuthJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}
	u, err := a.users.GetByEmail(r.Context(), req.Email)
	hash := auth.DummyPasswordHash
	if err == nil {
		hash = u.PasswordHash
	}
	// Always bcrypt-compare so missing users are not a timing oracle.
	ok := auth.CheckPassword(hash, req.Password)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}
	if u.IsDisabled() {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}
	token, expiresIn, err := a.tokens.Issue(u.ID, u.Email, u.PasswordVersion)
	if err != nil {
		a.log.Error("issue token", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accessToken": token,
		"expiresIn":   expiresIn,
		"user": map[string]string{
			"id":          u.ID,
			"email":       u.Email,
			"displayName": u.DisplayName,
		},
	})
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	u, err := a.users.GetByID(r.Context(), userID)
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found")
		return
	}
	if err != nil {
		a.log.Error("me", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":          u.ID,
		"email":       u.Email,
		"displayName": u.DisplayName,
	})
}
