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
			in:   ScoreInput{PredHome: 2, PredAway: 1, ResHome: 2, ResAway: 1},
			want: 4,
		},
		{
			name: "same goal difference including draw",
			in:   ScoreInput{PredHome: 1, PredAway: 1, ResHome: 2, ResAway: 2},
			want: 2,
		},
		{
			name: "same winner only",
			in:   ScoreInput{PredHome: 1, PredAway: 0, ResHome: 3, ResAway: 1},
			want: 1,
		},
		{
			name: "miss",
			in:   ScoreInput{PredHome: 0, PredAway: 1, ResHome: 1, ResAway: 0},
			want: 0,
		},
		{
			name: "knockout scores only, no team bonus",
			in:   ScoreInput{PredHome: 2, PredAway: 1, ResHome: 2, ResAway: 1},
			want: 4,
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

func TestAwardPointsDoublesPlayoffs(t *testing.T) {
	exact := ScoreInput{PredHome: 2, PredAway: 1, ResHome: 2, ResAway: 1}   // base 4
	outcome := ScoreInput{PredHome: 1, PredAway: 0, ResHome: 3, ResAway: 1} // base 1
	miss := ScoreInput{PredHome: 0, PredAway: 1, ResHome: 1, ResAway: 0}    // base 0

	tests := []struct {
		name  string
		stage string
		in    ScoreInput
		want  int
	}{
		{name: "group stage not doubled", stage: "Group", in: exact, want: 4},
		{name: "round of 16 doubled", stage: "Round of 16", in: exact, want: 8},
		{name: "final doubled", stage: "Final", in: exact, want: 8},
		{name: "playoff outcome doubled", stage: "Quarterfinal", in: outcome, want: 2},
		{name: "playoff miss stays zero", stage: "Final", in: miss, want: 0},
		{name: "unknown empty stage not doubled", stage: "", in: exact, want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := awardPoints(tt.stage, tt.in); got != tt.want {
				t.Fatalf("awardPoints(%q) = %d, want %d", tt.stage, got, tt.want)
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
