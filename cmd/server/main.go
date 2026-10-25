package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aleksey-mu/travel-planner-server/internal/auth"
	"github.com/aleksey-mu/travel-planner-server/internal/config"
	"github.com/aleksey-mu/travel-planner-server/internal/httpapi"
	"github.com/aleksey-mu/travel-planner-server/internal/syncstore"
	"github.com/aleksey-mu/travel-planner-server/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			os.Exit(runServe(os.Args[2:]))
		case "create-user":
			os.Exit(runCreateUser(os.Args[2:]))
		case "set-password":
			os.Exit(runSetPassword(os.Args[2:]))
		case "create-invite":
			os.Exit(runCreateInvite(os.Args[2:]))
		case "revoke-invite":
			os.Exit(runRevokeInvite(os.Args[2:]))
		case "create-reset-link":
			os.Exit(runCreateResetLink(os.Args[2:]))
		case "list-users":
			os.Exit(runListUsers(os.Args[2:]))
		case "set-quota":
			os.Exit(runSetQuota(os.Args[2:]))
		case "disable-user":
			os.Exit(runDisableUser(os.Args[2:]))
		case "enable-user":
			os.Exit(runEnableUser(os.Args[2:]))
		case "delete-user":
			os.Exit(runDeleteUser(os.Args[2:]))
		case "promote-admin":
			os.Exit(runPromoteAdmin(os.Args[2:]))
		case "demote-admin":
			os.Exit(runDemoteAdmin(os.Args[2:]))
		case "healthcheck":
			os.Exit(runHealthcheck())
		case "help", "-h", "--help":
			printUsage()
			os.Exit(0)
		default:
			// Default to serve if first arg looks like a flag
			if strings.HasPrefix(os.Args[1], "-") {
				os.Exit(runServe(os.Args[1:]))
			}
			fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
			printUsage()
			os.Exit(2)
		}
	}
	os.Exit(runServe(nil))
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `travelplan-server %s

Usage:
  travelplan-server [serve]
  travelplan-server create-user --email EMAIL --password PASS --display-name NAME [--admin]
  travelplan-server set-password --email EMAIL --password PASS
  travelplan-server create-invite [--expires-days N] [--label TEXT]
  travelplan-server revoke-invite --id ID
  travelplan-server create-reset-link --email EMAIL
  travelplan-server list-users
  travelplan-server set-quota --email EMAIL --mb N|default|unlimited
  travelplan-server disable-user --email EMAIL
  travelplan-server enable-user --email EMAIL
  travelplan-server delete-user --email EMAIL
  travelplan-server promote-admin --email EMAIL
  travelplan-server demote-admin --email EMAIL
  travelplan-server healthcheck

`, version.Version)
}

func setupLogger(*config.Config) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func loadConfigOrExit() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "DATA_DIR: %v\n", err)
		os.Exit(1)
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err == nil {
		cfg.DataDir = abs
	}
	return cfg
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	_ = fs.Parse(args)

	cfg := loadConfigOrExit()
	log := setupLogger(cfg)
	slog.SetDefault(log)

	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		log.Error("open user store", "err", err)
		return 1
	}
	defer users.Close()

	if err := seedAdmin(context.Background(), users, cfg, log); err != nil {
		log.Error("seed admin", "err", err)
		return 1
	}

	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTTTLSeconds)
	store := syncstore.New(cfg.DataDir, cfg.DefaultQuotaBytes, cfg.MaxUploadBytes)
	api := httpapi.NewAPI(cfg, users, tokens, store, log)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "version", version.Version)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	return 0
}

func seedAdmin(ctx context.Context, users *auth.UserStore, cfg *config.Config, log *slog.Logger) error {
	email := strings.TrimSpace(cfg.SeedAdminEmail)
	if email == "" {
		return nil
	}
	if cfg.SeedAdminPassword == "" {
		return fmt.Errorf("SEED_ADMIN_PASSWORD required when SEED_ADMIN_EMAIL is set")
	}

	u, err := users.CreateIfEmpty(ctx, email, cfg.SeedAdminPassword, cfg.SeedAdminDisplayName, cfg.MaxUsers)
	if errors.Is(err, auth.ErrUsersExist) {
		_, getErr := users.GetByEmail(ctx, email)
		if getErr == nil {
			log.Info("seed admin already exists", "email", auth.NormalizeEmail(email))
			log.Warn("SEED_ADMIN_PASSWORD is still set; remove it from the container environment after first boot")
			return nil
		}
		if !errors.Is(getErr, auth.ErrNotFound) {
			return getErr
		}
		// Leftover SEED_ADMIN_* must not create an extra admin once any user exists.
		log.Info("seed skipped: users already exist")
		return nil
	}
	if errors.Is(err, auth.ErrConflict) {
		log.Info("seed admin already exists", "email", auth.NormalizeEmail(email))
		return nil
	}
	if errors.Is(err, auth.ErrUserLimit) {
		log.Warn("seed skipped: MAX_USERS reached")
		return nil
	}
	if err != nil {
		return err
	}
	log.Info("seeded admin user", "userId", u.ID, "email", u.Email)
	log.Warn("remove SEED_ADMIN_PASSWORD from the environment after first boot")
	return nil
}

// loadCLIConfig is swapped in tests to avoid os.Exit from loadConfigOrExit.
var loadCLIConfig = loadConfigOrExit

func runCreateUser(args []string) int {
	fs := flag.NewFlagSet("create-user", flag.ContinueOnError)
	email := fs.String("email", "", "user email")
	password := fs.String("password", "", "user password")
	displayName := fs.String("display-name", "", "display name (required)")
	asAdmin := fs.Bool("admin", false, "grant admin")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *email == "" || *password == "" || strings.TrimSpace(*displayName) == "" {
		fmt.Fprintln(os.Stderr, "create-user requires --email, --password, and --display-name")
		return 2
	}
	cfg := loadCLIConfig()
	return createUser(cfg, *email, *password, *displayName, *asAdmin)
}

func createUser(cfg *config.Config, email, password, displayName string, asAdmin bool) int {
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	u, err := users.Create(context.Background(), email, password, displayName, asAdmin, cfg.MaxUsers)
	if errors.Is(err, auth.ErrUserLimit) {
		fmt.Fprintf(os.Stderr, "user limit reached (MAX_USERS=%d)\n", cfg.MaxUsers)
		return 1
	}
	if errors.Is(err, auth.ErrConflict) {
		fmt.Fprintln(os.Stderr, "email already registered")
		return 1
	}
	if errors.Is(err, auth.ErrDisplayNameRequired) {
		fmt.Fprintln(os.Stderr, "display name is required")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "create-user: %v\n", err)
		return 1
	}
	fmt.Println(u.ID)
	return 0
}

func runSetPassword(args []string) int {
	fs := flag.NewFlagSet("set-password", flag.ContinueOnError)
	email := fs.String("email", "", "user email")
	password := fs.String("password", "", "new password")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *email == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "set-password requires --email and --password")
		return 2
	}
	cfg := loadConfigOrExit()
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	err = users.SetPassword(context.Background(), *email, *password)
	if errors.Is(err, auth.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "user not found")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "set-password: %v\n", err)
		return 1
	}
	fmt.Println("ok")
	return 0
}

func runCreateInvite(args []string) int {
	fs := flag.NewFlagSet("create-invite", flag.ContinueOnError)
	expiresDays := fs.Int("expires-days", 0, "TTL in days (default INVITE_TTL_DAYS)")
	label := fs.String("label", "", "optional label")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCLIConfig()
	days := cfg.InviteTTLDays
	if *expiresDays != 0 {
		if *expiresDays < 0 {
			fmt.Fprintln(os.Stderr, "--expires-days must be positive")
			return 2
		}
		days = *expiresDays
	}
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	adminID, err := users.FirstEnabledAdminID(context.Background())
	if errors.Is(err, auth.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "no enabled admin to attribute invite")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "create-invite: %v\n", err)
		return 1
	}
	created, err := users.CreateInvite(context.Background(), adminID, *label, days)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create-invite: %v\n", err)
		return 1
	}
	fmt.Printf("id=%s\n", created.Invite.ID)
	fmt.Printf("token=%s\n", created.RawToken)
	fmt.Printf("expiresAt=%s\n", created.Invite.ExpiresAt)
	if cfg.PublicBaseURL != "" {
		fmt.Printf("inviteUrl=%s/join/?token=%s\n",
			strings.TrimSuffix(cfg.PublicBaseURL, "/"), created.RawToken)
	}
	return 0
}

func runRevokeInvite(args []string) int {
	fs := flag.NewFlagSet("revoke-invite", flag.ContinueOnError)
	id := fs.String("id", "", "invite id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(os.Stderr, "revoke-invite requires --id")
		return 2
	}
	cfg := loadCLIConfig()
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	err = users.RevokeInvite(context.Background(), *id)
	if errors.Is(err, auth.ErrInviteNotFound) {
		fmt.Fprintln(os.Stderr, "invite not found")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "revoke-invite: %v\n", err)
		return 1
	}
	fmt.Println("ok")
	return 0
}

func runCreateResetLink(args []string) int {
	fs := flag.NewFlagSet("create-reset-link", flag.ContinueOnError)
	email := fs.String("email", "", "user email")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *email == "" {
		fmt.Fprintln(os.Stderr, "create-reset-link requires --email")
		return 2
	}
	cfg := loadCLIConfig()
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	u, err := users.GetByEmail(context.Background(), *email)
	if errors.Is(err, auth.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "user not found")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "create-reset-link: %v\n", err)
		return 1
	}
	created, err := users.CreatePasswordResetToken(context.Background(), u.ID, cfg.PasswordResetTTLSeconds)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create-reset-link: %v\n", err)
		return 1
	}
	fmt.Printf("id=%s\n", created.Token.ID)
	fmt.Printf("token=%s\n", created.RawToken)
	fmt.Printf("expiresAt=%s\n", created.Token.ExpiresAt)
	if cfg.PublicBaseURL != "" {
		fmt.Printf("resetUrl=%s/reset-password/?token=%s\n",
			strings.TrimSuffix(cfg.PublicBaseURL, "/"), created.RawToken)
	}
	return 0
}

func runListUsers(args []string) int {
	fs := flag.NewFlagSet("list-users", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCLIConfig()
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	list, err := users.ListAllUsers(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "list-users: %v\n", err)
		return 1
	}
	for _, u := range list {
		quota := "default"
		if u.QuotaBytes.Valid {
			if u.QuotaBytes.Int64 == 0 {
				quota = "unlimited"
			} else {
				quota = fmt.Sprintf("%d", u.QuotaBytes.Int64)
			}
		}
		fmt.Printf("%s\t%s\t%s\tadmin=%v\tdisabled=%v\tquota=%s\n",
			u.ID, u.Email, u.DisplayName, u.IsAdmin, u.IsDisabled(), quota)
	}
	return 0
}

func runSetQuota(args []string) int {
	fs := flag.NewFlagSet("set-quota", flag.ContinueOnError)
	email := fs.String("email", "", "user email")
	mb := fs.String("mb", "", "default | unlimited | positive MiB")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *email == "" || *mb == "" {
		fmt.Fprintln(os.Stderr, "set-quota requires --email and --mb")
		return 2
	}
	cfg := loadCLIConfig()
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	err = users.SetQuotaCLI(context.Background(), *email, *mb)
	if errors.Is(err, auth.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "user not found")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "set-quota: %v\n", err)
		return 1
	}
	fmt.Println("ok")
	return 0
}

func runDisableUser(args []string) int {
	return runEmailPatchCLI("disable-user", args, func(users *auth.UserStore, email string) error {
		return users.DisableUserCLI(context.Background(), email)
	})
}

func runEnableUser(args []string) int {
	return runEmailPatchCLI("enable-user", args, func(users *auth.UserStore, email string) error {
		return users.EnableUserCLI(context.Background(), email)
	})
}

func runPromoteAdmin(args []string) int {
	return runEmailPatchCLI("promote-admin", args, func(users *auth.UserStore, email string) error {
		return users.PromoteAdminCLI(context.Background(), email)
	})
}

func runDemoteAdmin(args []string) int {
	return runEmailPatchCLI("demote-admin", args, func(users *auth.UserStore, email string) error {
		return users.DemoteAdminCLI(context.Background(), email)
	})
}

func runEmailPatchCLI(name string, args []string, fn func(*auth.UserStore, string) error) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	email := fs.String("email", "", "user email")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *email == "" {
		fmt.Fprintf(os.Stderr, "%s requires --email\n", name)
		return 2
	}
	cfg := loadCLIConfig()
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	err = fn(users, *email)
	if errors.Is(err, auth.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "user not found")
		return 1
	}
	if errors.Is(err, auth.ErrLastEnabledAdmin) {
		fmt.Fprintln(os.Stderr, "cannot disable or demote the last enabled admin")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 1
	}
	fmt.Println("ok")
	return 0
}

func runDeleteUser(args []string) int {
	fs := flag.NewFlagSet("delete-user", flag.ContinueOnError)
	email := fs.String("email", "", "user email")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *email == "" {
		fmt.Fprintln(os.Stderr, "delete-user requires --email")
		return 2
	}
	cfg := loadCLIConfig()
	users, err := auth.OpenUserStore(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer users.Close()

	u, err := users.GetByEmail(context.Background(), *email)
	if errors.Is(err, auth.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "user not found")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "delete-user: %v\n", err)
		return 1
	}
	if err := users.DeleteUserHard(context.Background(), u.ID); err != nil {
		if errors.Is(err, auth.ErrLastEnabledAdmin) {
			fmt.Fprintln(os.Stderr, "cannot delete the last enabled admin while other users remain")
			return 1
		}
		fmt.Fprintf(os.Stderr, "delete-user: %v\n", err)
		return 1
	}
	store := syncstore.New(cfg.DataDir, cfg.DefaultQuotaBytes, cfg.MaxUploadBytes)
	if err := store.PurgeUser(u.ID); err != nil {
		fmt.Fprintf(os.Stderr, "delete-user: user removed but vault purge failed: %v\n", err)
		return 1
	}
	fmt.Println("ok")
	return 0
}

func runHealthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host := "127.0.0.1"
	port := "8080"
	if strings.HasPrefix(addr, ":") {
		port = strings.TrimPrefix(addr, ":")
	} else if h, p, err := net.SplitHostPort(addr); err == nil {
		if h != "" && h != "0.0.0.0" && h != "::" {
			host = h
		}
		port = p
	}
	url := fmt.Sprintf("http://%s:%s/health", host, port)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
