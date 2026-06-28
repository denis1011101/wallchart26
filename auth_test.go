package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoginRequestLimiterPerIP(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	limiter := newLoginRequestLimiter(2, time.Minute, 10, time.Hour)

	if !limiter.Allow("203.0.113.10", now) {
		t.Fatal("first request should be allowed")
	}
	if !limiter.Allow("203.0.113.10", now.Add(10*time.Second)) {
		t.Fatal("second request should be allowed")
	}
	if limiter.Allow("203.0.113.10", now.Add(20*time.Second)) {
		t.Fatal("third request inside the IP window should be denied")
	}
	if !limiter.Allow("203.0.113.10", now.Add(time.Minute+time.Second)) {
		t.Fatal("request after the IP window should be allowed")
	}
}

func TestLoginRequestLimiterGlobalCap(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	limiter := newLoginRequestLimiter(10, time.Hour, 2, 24*time.Hour)

	if !limiter.Allow("203.0.113.10", now) {
		t.Fatal("first request should be allowed")
	}
	if !limiter.Allow("203.0.113.11", now) {
		t.Fatal("second request should be allowed")
	}
	if limiter.Allow("203.0.113.12", now) {
		t.Fatal("third request inside the global window should be denied")
	}
	if !limiter.Allow("203.0.113.12", now.Add(24*time.Hour+time.Second)) {
		t.Fatal("request after the global window should be allowed")
	}
}

func TestClientIPTrustProxyHeaders(t *testing.T) {
	req := &http.Request{
		Header:     http.Header{},
		RemoteAddr: "198.51.100.10:12345",
	}
	req.Header.Set("CF-Connecting-IP", "203.0.113.20")
	req.Header.Set("X-Forwarded-For", "203.0.113.30, 198.51.100.11")

	untrusted := &app{}
	if got := untrusted.clientIP(req); got != "198.51.100.10" {
		t.Fatalf("untrusted clientIP = %q, want remote addr", got)
	}

	trusted := &app{trustProxyHeaders: true}
	if got := trusted.clientIP(req); got != "203.0.113.20" {
		t.Fatalf("trusted clientIP = %q, want CF-Connecting-IP", got)
	}

	req.Header.Del("CF-Connecting-IP")
	if got := trusted.clientIP(req); got != "203.0.113.30" {
		t.Fatalf("trusted clientIP = %q, want left-most X-Forwarded-For", got)
	}
}

func TestPredictKnockoutRequiresScoresAndStoresMatchTeams(t *testing.T) {
	a := newTestApp(t)
	a.now = func() time.Time { return fixedTime(t, "2026-07-03T12:00:00Z") }
	insertUser(t, a, 1, "Ann", "2026-01-01T00:00:00Z")
	if _, err := a.db.Exec(
		`INSERT INTO sessions(token, user_id, expires_at, created_at) VALUES('session-token', 1, '2026-12-31T00:00:00Z', '2026-07-03T12:00:00Z')`,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := a.db.Exec(
		`INSERT INTO matches(id, stage, home, away, kickoff_utc) VALUES(200, 'Round of 16', 'Brazil', 'Japan', '2026-07-05T18:00:00Z')`,
	); err != nil {
		t.Fatalf("insert match: %v", err)
	}

	emptyForm := url.Values{"match_id": {"200"}}
	emptyReq := httptest.NewRequest(http.MethodPost, "/predict", strings.NewReader(emptyForm.Encode()))
	emptyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	emptyReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	emptyRec := httptest.NewRecorder()
	a.predict(emptyRec, emptyReq)
	if emptyRec.Code != http.StatusBadRequest {
		t.Fatalf("empty knockout prediction status = %d, want %d", emptyRec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(emptyRec.Body.String(), "Fill both scores") {
		t.Fatalf("empty knockout prediction body = %q, want Fill both scores", emptyRec.Body.String())
	}

	scoreForm := url.Values{"match_id": {"200"}, "home": {"2"}, "away": {"1"}}
	scoreReq := httptest.NewRequest(http.MethodPost, "/predict", strings.NewReader(scoreForm.Encode()))
	scoreReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	scoreReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	scoreRec := httptest.NewRecorder()
	a.predict(scoreRec, scoreReq)
	if scoreRec.Code != http.StatusNoContent {
		t.Fatalf("scored knockout prediction status = %d, want %d", scoreRec.Code, http.StatusNoContent)
	}

	var home, away int
	var homeTeam, awayTeam string
	if err := a.db.QueryRow(
		`SELECT home, away, home_team, away_team FROM predictions WHERE user_id = 1 AND match_id = 200`,
	).Scan(&home, &away, &homeTeam, &awayTeam); err != nil {
		t.Fatalf("query prediction: %v", err)
	}
	if home != 2 || away != 1 || homeTeam != "Brazil" || awayTeam != "Japan" {
		t.Fatalf("prediction = %d-%d %q/%q, want 2-1 Brazil/Japan", home, away, homeTeam, awayTeam)
	}
}

func TestLoadDotEnvDoesNotOverrideEnvironment(t *testing.T) {
	const existingKey = "WALLCHART26_TEST_DOTENV_EXISTING"
	const newKey = "WALLCHART26_TEST_DOTENV_NEW"
	const quotedKey = "WALLCHART26_TEST_DOTENV_QUOTED"
	defer os.Unsetenv(existingKey)
	defer os.Unsetenv(newKey)
	defer os.Unsetenv(quotedKey)
	defer os.Unsetenv("APP_ENV")

	if err := os.Setenv(existingKey, "real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("APP_ENV", "development"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), ".env")
	content := existingKey + "=from-file\n" + newKey + "=loaded\n" + quotedKey + "='quoted value'\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv(existingKey); got != "real" {
		t.Fatalf("%s = %q, want existing env to win", existingKey, got)
	}
	if got := os.Getenv(newKey); got != "loaded" {
		t.Fatalf("%s = %q, want loaded", newKey, got)
	}
	if got := os.Getenv(quotedKey); got != "quoted value" {
		t.Fatalf("%s = %q, want quotes stripped", quotedKey, got)
	}
}
