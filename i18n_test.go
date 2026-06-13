package main

import "testing"

func TestPlural(t *testing.T) {
	tests := []struct {
		lang string
		n    int
		want string
	}{
		{lang: "ru", n: 1, want: "игрок"},
		{lang: "ru", n: 2, want: "игрока"},
		{lang: "ru", n: 5, want: "игроков"},
		{lang: "ru", n: 11, want: "игроков"},
		{lang: "ru", n: 21, want: "игрок"},
		{lang: "ru", n: 22, want: "игрока"},
		{lang: "ru", n: 25, want: "игроков"},
		{lang: "en", n: 1, want: "player"},
		{lang: "en", n: 2, want: "players"},
	}

	for _, tt := range tests {
		got := plural(tt.lang, tt.n, "игрок", "игрока", "игроков")
		if tt.lang == "en" {
			got = plural(tt.lang, tt.n, "player", "players", "players")
		}
		if got != tt.want {
			t.Fatalf("plural(%q, %d) = %q, want %q", tt.lang, tt.n, got, tt.want)
		}
	}
}

// Mirrors the real call in home.html so a missing few-form ("игрока") is caught.
func TestLeaderboardCountUsesCatalogForms(tb *testing.T) {
	form := func(n int) string {
		return plural("ru", n,
			t("ru", "leaderboard.player"),
			t("ru", "leaderboard.players_few"),
			t("ru", "leaderboard.players"))
	}
	cases := map[int]string{1: "игрок", 2: "игрока", 4: "игрока", 5: "игроков", 11: "игроков", 22: "игрока"}
	for n, want := range cases {
		if got := form(n); got != want {
			tb.Fatalf("leaderboard form for %d = %q, want %q", n, got, want)
		}
	}
}

func TestTeamName(t *testing.T) {
	tests := []struct {
		key  string
		lang string
		want string
	}{
		{key: "Brazil", lang: "en", want: "Brazil"},
		{key: "Brazil", lang: "ru", want: "Бразилия"},
		{key: "Atlantis", lang: "ru", want: "Atlantis"},
		{key: "TBD Round of 32 1 home", lang: "en", want: "TBD"},
		{key: "TBD Round of 32 1 home", lang: "ru", want: "не определено"},
	}

	for _, tt := range tests {
		if got := teamName(tt.key, tt.lang); got != tt.want {
			t.Fatalf("teamName(%q, %q) = %q, want %q", tt.key, tt.lang, got, tt.want)
		}
	}
}

func TestResolveTeamNameCanonicalizesBothLocalesForScoring(t *testing.T) {
	en := resolveTeamName("Brazil")
	ru := resolveTeamName("Бразилия")
	if en != "Brazil" || ru != "Brazil" {
		t.Fatalf("resolveTeamName canonical keys = en %q, ru %q; want Brazil", en, ru)
	}

	for _, guess := range []string{en, ru} {
		got := score(ScoreInput{
			PredHome:     1,
			PredAway:     0,
			ResHome:      0,
			ResAway:      1,
			Stage:        "Round of 16",
			PredHomeTeam: guess,
			HomeTeam:     "Brazil",
			PredAwayTeam: "Spain",
			AwayTeam:     "Canada",
		})
		if got != 1 {
			t.Fatalf("score team bonus with %q = %d, want 1", guess, got)
		}
	}
}
