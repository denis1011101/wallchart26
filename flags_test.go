package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderFlag(t *testing.T, key string) string {
	t.Helper()
	var buf bytes.Buffer
	child := templ.Raw(teamName(key, "en"))
	if err := Flag(key).Render(templ.WithChildren(context.Background(), child), &buf); err != nil {
		t.Fatalf("render Flag(%q): %v", key, err)
	}
	return buf.String()
}

func TestFlagRendersImageAndName(t *testing.T) {
	out := renderFlag(t, "Brazil")
	if !strings.Contains(out, "/static/flags/br.svg") {
		t.Errorf("Flag(Brazil) missing flag image: %s", out)
	}
	if !strings.Contains(out, "Brazil") {
		t.Errorf("Flag(Brazil) dropped the team name: %s", out)
	}
}

func TestFlagPlaceholderRendersNameWithoutImage(t *testing.T) {
	out := renderFlag(t, "TBD Winner A")
	if strings.Contains(out, "/static/flags/") {
		t.Errorf("placeholder should have no flag image: %s", out)
	}
}

func TestEveryTeamHasFlagAndAsset(t *testing.T) {
	for _, key := range embeddedTeamOptions() {
		iso := teamFlagISO(key)
		if iso == "" {
			t.Errorf("team %q has no flag ISO mapping", key)
			continue
		}
		path := filepath.Join("static", "flags", iso+".svg")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("team %q -> %q: missing flag asset %s", key, iso, path)
		}
	}
}

func TestTeamFlagISOPlaceholders(t *testing.T) {
	for _, key := range []string{"", "  ", "TBD", "TBD Winner Group A", "Atlantis"} {
		if got := teamFlagISO(key); got != "" {
			t.Errorf("teamFlagISO(%q) = %q, want empty", key, got)
		}
	}
}
