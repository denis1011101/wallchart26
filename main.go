package main

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:generate templ generate

//go:embed fixtures.json static
var embedded embed.FS

const (
	sessionCookieName = "wallchart_session"
	langCookieName    = "lang"
	tzCookieName      = "tz"
	loginCodeTTL      = 10 * time.Minute
	sessionTTL        = 365 * 24 * time.Hour

	loginIPLimit      = 5
	loginIPWindow     = 15 * time.Minute
	loginGlobalLimit  = 200
	loginGlobalWindow = 24 * time.Hour
	smtpSendTimeout   = 10 * time.Second
	shutdownGrace     = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

type app struct {
	db                *sql.DB
	adminEmails       map[string]struct{}
	cookieSecure      bool
	smtp              smtpConfig
	loginRateLimiter  *loginRequestLimiter
	trustProxyHeaders bool
	now               func() time.Time
}

type user struct {
	ID        int64
	Name      string
	Email     string
	EmailNorm string
}

type matchRow struct {
	ID           int64
	Stage        string
	GroupName    string
	Home         string
	Away         string
	Kickoff      time.Time
	HomeScore    sql.NullInt64
	AwayScore    sql.NullInt64
	PredHome     sql.NullInt64
	PredAway     sql.NullInt64
	Locked       bool
	PredHomeTeam sql.NullString
	PredAwayTeam sql.NullString
}

type leaderboardRow struct {
	UserID      int64
	Name        string
	Points      int
	Exact       int
	Predictions int
}

type pageData struct {
	Title       string
	Lang        string
	Timezone    string
	CurrentUser *user
	Matches     []matchRow
	Leaderboard []leaderboardRow
	Users       []user
	ViewedUser  *user
	Message     string
	AuthEmail   string
	AuthName    string
	TeamOptions []string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if err := loadDotEnv(".env"); err != nil {
		log.Printf("load .env: %v", err)
	}

	dbPath := getenv("DATABASE_PATH", "wallchart26.sqlite")
	if err := os.MkdirAll(filepath.Dir(normalizeDBPath(dbPath)), 0o755); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		return err
	}
	if err := seedFixtures(ctx, db); err != nil {
		return err
	}

	a := &app{
		db:                db,
		adminEmails:       parseEmailSet(os.Getenv("ADMIN_EMAILS")),
		cookieSecure:      getenv("COOKIE_SECURE", "true") != "false",
		smtp:              loadSMTPConfig(),
		loginRateLimiter:  newLoginRequestLimiter(loginIPLimit, loginIPWindow, loginGlobalLimit, loginGlobalWindow),
		trustProxyHeaders: getenv("TRUST_PROXY_HEADERS", "false") == "true",
		now:               func() time.Time { return time.Now().UTC() },
	}

	mux := http.NewServeMux()
	staticFiles, err := fs.Sub(embedded, "static")
	if err != nil {
		return err
	}
	mux.Handle("GET /static/", cacheStatic(http.StripPrefix("/static/", http.FileServerFS(staticFiles))))
	mux.HandleFunc("GET /", a.home)
	mux.HandleFunc("GET /leaderboard", a.leaderboardFragment)
	mux.HandleFunc("GET /lang", a.setLang)
	mux.HandleFunc("GET /login", a.login)
	mux.HandleFunc("POST /auth/request", a.authRequest)
	mux.HandleFunc("POST /auth/verify", a.authVerify)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /me", a.me)
	mux.HandleFunc("POST /predict", a.predict)
	mux.HandleFunc("GET /admin", a.admin)
	mux.HandleFunc("POST /admin/result", a.adminResult)
	mux.HandleFunc("GET /u/{id}", a.userPage)

	addr := getenv("ADDR", ":8080")
	log.Printf("Wallchart '26 listening on %s", addr)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		log.Println("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		next.ServeHTTP(w, r)
	})
}

func loadDotEnv(path string) error {
	if os.Getenv("APP_ENV") == "production" {
		return nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
