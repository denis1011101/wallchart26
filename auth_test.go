package main

import (
	"net/http"
	"os"
	"path/filepath"
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

func TestEmptyKnockoutPrediction(t *testing.T) {
	tests := []struct {
		name         string
		stage        string
		homeOK       bool
		predHomeTeam string
		predAwayTeam string
		want         bool
	}{
		{name: "empty knockout", stage: "Round of 16", want: true},
		{name: "group empty is not handled here", stage: "Group", want: false},
		{name: "score filled", stage: "Round of 16", homeOK: true, want: false},
		{name: "home team only", stage: "Round of 16", predHomeTeam: "Brazil", want: false},
		{name: "away team only", stage: "Round of 16", predAwayTeam: "Japan", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := emptyKnockoutPrediction(tt.stage, tt.homeOK, tt.predHomeTeam, tt.predAwayTeam)
			if got != tt.want {
				t.Fatalf("emptyKnockoutPrediction() = %v, want %v", got, tt.want)
			}
		})
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
