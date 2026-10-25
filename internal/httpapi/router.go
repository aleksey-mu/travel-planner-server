package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
	"github.com/aleksey-mu/travel-planner-server/internal/config"
	"github.com/aleksey-mu/travel-planner-server/internal/ratelimit"
	"github.com/aleksey-mu/travel-planner-server/internal/syncstore"
)

// API holds dependencies for HTTP handlers.
type API struct {
	cfg     *config.Config
	users   *auth.UserStore
	tokens  *auth.TokenManager
	store   *syncstore.Store
	log     *slog.Logger
	limiter *ratelimit.Limiter
}

// NewAPI constructs an API and wires per-user quota resolution onto the store.
func NewAPI(cfg *config.Config, users *auth.UserStore, tokens *auth.TokenManager, store *syncstore.Store, log *slog.Logger) *API {
	a := &API{
		cfg:     cfg,
		users:   users,
		tokens:  tokens,
		store:   store,
		log:     log,
		limiter: ratelimit.New(cfg.AuthRateLimitMax, cfg.AuthRateLimitWindow),
	}
	store.SetUserQuotaResolver(func(userID string) (int64, error) {
		u, err := users.GetByID(context.Background(), userID)
		if err != nil {
			return 0, err
		}
		configured, _ := syncstore.ResolveConfigured(u.QuotaBytes, cfg.DefaultQuotaBytes)
		return configured, nil
	})
	return a
}

// Handler returns the root HTTP handler with middleware.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	a.registerRoutes(mux)
	return Chain(mux,
		RecoverMiddleware(a.log),
		RequestIDMiddleware,
		LoggingMiddleware(a.log),
		CORSMiddleware(a.cfg.CORSOrigins),
	)
}

func (a *API) registerRoutes(mux *http.ServeMux) {
	requireAuth := auth.Middleware(a.tokens, a.users)
	requireAdmin := func(h http.Handler) http.Handler {
		return requireAuth(auth.AdminMiddleware(a.users)(h))
	}
	requireSync := func(h http.HandlerFunc) http.Handler {
		return requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := auth.UserIDFromContext(r.Context())
			u, err := a.users.GetByID(r.Context(), userID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found")
				return
			}
			if u.NeedsRecoveryCodesAck() {
				writeError(w, http.StatusForbidden, "RECOVERY_CODES_ACK_REQUIRED", "acknowledge recovery codes before sync")
				return
			}
			h.ServeHTTP(w, r)
		}))
	}

	a.registerPublicStatic(mux)

	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("POST /auth/register", a.handleRegister)
	mux.HandleFunc("POST /auth/login", a.handleLogin)
	mux.HandleFunc("POST /auth/recover", a.handleRecover)
	mux.HandleFunc("GET /server/info", a.handleServerInfo)
	mux.HandleFunc("GET /invites/{token}", a.handleGetInvite)
	mux.HandleFunc("POST /invites/{token}/accept", a.handleAcceptInvite)
	mux.HandleFunc("GET /password-reset/{token}", a.handleGetPasswordReset)
	mux.HandleFunc("POST /password-reset/{token}", a.handleCompletePasswordReset)

	mux.Handle("GET /me", requireAuth(http.HandlerFunc(a.handleMe)))
	mux.Handle("DELETE /me", requireAuth(http.HandlerFunc(a.handleDeleteMe)))
	mux.Handle("POST /me/recovery-codes", requireAuth(http.HandlerFunc(a.handleRegenerateRecoveryCodes)))
	mux.Handle("POST /me/recovery-codes/ack", requireAuth(http.HandlerFunc(a.handleAckRecoveryCodes)))
	mux.Handle("GET /users", requireAuth(http.HandlerFunc(a.handleListUsers)))

	mux.Handle("POST /admin/invites", requireAdmin(http.HandlerFunc(a.handleAdminCreateInvite)))
	mux.Handle("GET /admin/invites", requireAdmin(http.HandlerFunc(a.handleAdminListInvites)))
	mux.Handle("DELETE /admin/invites/{id}", requireAdmin(http.HandlerFunc(a.handleAdminRevokeInvite)))
	mux.Handle("POST /admin/users/{id}/password-reset", requireAdmin(http.HandlerFunc(a.handleAdminCreatePasswordReset)))
	mux.Handle("GET /admin/users", requireAdmin(http.HandlerFunc(a.handleAdminListUsers)))
	mux.Handle("PATCH /admin/users/{id}", requireAdmin(http.HandlerFunc(a.handleAdminPatchUser)))
	mux.Handle("DELETE /admin/users/{id}", requireAdmin(http.HandlerFunc(a.handleAdminDeleteUser)))

	mux.Handle("POST /sync/bootstrap", requireSync(a.handleBootstrap))
	mux.Handle("GET /sync/quota", requireSync(a.handleQuota))
	mux.Handle("GET /sync/manifest", requireSync(a.handleGetManifest))
	mux.Handle("PUT /sync/manifest", requireSync(a.handlePutManifest))

	mux.Handle("GET /sync/travels/{travelId}", requireSync(a.handleGetTravel))
	mux.Handle("PUT /sync/travels/{travelId}", requireSync(a.handlePutTravel))
	mux.Handle("DELETE /sync/travels/{travelId}", requireSync(a.handleDeleteTravel))

	mux.Handle("GET /sync/user-data/{fileName}", requireSync(a.handleGetUserData))
	mux.Handle("PUT /sync/user-data/{fileName}", requireSync(a.handlePutUserData))

	// Specific file routes before wildcards.
	mux.Handle("PUT /sync/files/{travelId}/{itemId}/{fileName}", requireSync(a.handlePutFile))
	mux.Handle("POST /sync/files/{travelId}/{itemId}/ensure-folder", requireSync(a.handleEnsureFolder))
	mux.Handle("GET /sync/files/{fileId...}", requireSync(a.handleGetFile))
	mux.Handle("DELETE /sync/files/{fileId...}", requireSync(a.handleDeleteFile))
}
