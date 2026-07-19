package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
	"github.com/aleksey-mu/travel-planner-server/internal/config"
	"github.com/aleksey-mu/travel-planner-server/internal/syncstore"
)

// API holds dependencies for HTTP handlers.
type API struct {
	cfg    *config.Config
	users  *auth.UserStore
	tokens *auth.TokenManager
	store  *syncstore.Store
	log    *slog.Logger
}

// NewAPI constructs an API and wires per-user quota resolution onto the store.
func NewAPI(cfg *config.Config, users *auth.UserStore, tokens *auth.TokenManager, store *syncstore.Store, log *slog.Logger) *API {
	a := &API{cfg: cfg, users: users, tokens: tokens, store: store, log: log}
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

	mux.HandleFunc("GET /api/v1/health", a.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/register", a.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", a.handleLogin)
	mux.HandleFunc("GET /api/v1/server/info", a.handleServerInfo)
	mux.HandleFunc("GET /api/v1/invites/{token}", a.handleGetInvite)

	mux.Handle("GET /api/v1/me", requireAuth(http.HandlerFunc(a.handleMe)))

	mux.Handle("POST /api/v1/admin/invites", requireAdmin(http.HandlerFunc(a.handleAdminCreateInvite)))
	mux.Handle("GET /api/v1/admin/invites", requireAdmin(http.HandlerFunc(a.handleAdminListInvites)))
	mux.Handle("DELETE /api/v1/admin/invites/{id}", requireAdmin(http.HandlerFunc(a.handleAdminRevokeInvite)))

	mux.Handle("POST /api/v1/sync/bootstrap", requireAuth(http.HandlerFunc(a.handleBootstrap)))
	mux.Handle("GET /api/v1/sync/quota", requireAuth(http.HandlerFunc(a.handleQuota)))
	mux.Handle("GET /api/v1/sync/manifest", requireAuth(http.HandlerFunc(a.handleGetManifest)))
	mux.Handle("PUT /api/v1/sync/manifest", requireAuth(http.HandlerFunc(a.handlePutManifest)))

	mux.Handle("GET /api/v1/sync/travels/{travelId}", requireAuth(http.HandlerFunc(a.handleGetTravel)))
	mux.Handle("PUT /api/v1/sync/travels/{travelId}", requireAuth(http.HandlerFunc(a.handlePutTravel)))
	mux.Handle("DELETE /api/v1/sync/travels/{travelId}", requireAuth(http.HandlerFunc(a.handleDeleteTravel)))

	mux.Handle("GET /api/v1/sync/user-data/{fileName}", requireAuth(http.HandlerFunc(a.handleGetUserData)))
	mux.Handle("PUT /api/v1/sync/user-data/{fileName}", requireAuth(http.HandlerFunc(a.handlePutUserData)))

	// Specific file routes before wildcards.
	mux.Handle("PUT /api/v1/sync/files/{travelId}/{itemId}/{fileName}", requireAuth(http.HandlerFunc(a.handlePutFile)))
	mux.Handle("POST /api/v1/sync/files/{travelId}/{itemId}/ensure-folder", requireAuth(http.HandlerFunc(a.handleEnsureFolder)))
	mux.Handle("GET /api/v1/sync/files/{fileId...}", requireAuth(http.HandlerFunc(a.handleGetFile)))
	mux.Handle("DELETE /api/v1/sync/files/{fileId...}", requireAuth(http.HandlerFunc(a.handleDeleteFile)))
}
