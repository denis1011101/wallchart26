package main

import "testing"

func TestScore(t *testing.T) {
	tests := []struct {
		name string
		in   ScoreInput
		want int
	}{
		{
			name: "exact score",
			in:   ScoreInput{PredHome: 2, PredAway: 1, ResHome: 2, ResAway: 1, Stage: "Group"},
			want: 4,
		},
		{
			name: "same goal difference including draw",
			in:   ScoreInput{PredHome: 1, PredAway: 1, ResHome: 2, ResAway: 2, Stage: "Group"},
			want: 2,
		},
		{
			name: "same winner only",
			in:   ScoreInput{PredHome: 1, PredAway: 0, ResHome: 3, ResAway: 1, Stage: "Group"},
			want: 1,
		},
		{
			name: "miss",
			in:   ScoreInput{PredHome: 0, PredAway: 1, ResHome: 1, ResAway: 0, Stage: "Group"},
			want: 0,
		},
		{
			name: "knockout team bonus",
			in: ScoreInput{
				PredHome: 2, PredAway: 1, ResHome: 2, ResAway: 1,
				Stage: "Quarter-final", PredHomeTeam: "  brazil ", PredAwayTeam: "JaPan", HomeTeam: "Brazil", AwayTeam: "Japan",
			},
			want: 6,
		},
		{
			name: "knockout one participant bonus",
			in: ScoreInput{
				PredHome: 2, PredAway: 0, ResHome: 1, ResAway: 0,
				Stage: "Round of 16", PredHomeTeam: "Spain", PredAwayTeam: "Mexico", HomeTeam: "Spain", AwayTeam: "Canada",
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := score(tt.in); got != tt.want {
				t.Fatalf("score() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExactScoreIgnoresBonuses(t *testing.T) {
	if !exactScore(2, 1, 2, 1) {
		t.Fatal("exactScore() should be true for the same score")
	}
	if exactScore(2, 0, 1, 0) {
		t.Fatal("exactScore() should be false for matching outcome without exact score")
	}
}
