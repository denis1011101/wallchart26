package main

import "strings"

type ScoreInput struct {
	PredHome int
	PredAway int
	ResHome  int
	ResAway  int
}

func score(s ScoreInput) int {
	points := 0

	if exactScore(s.PredHome, s.PredAway, s.ResHome, s.ResAway) {
		points = 4
	} else if s.PredHome-s.PredAway == s.ResHome-s.ResAway {
		points = 2
	} else if outcome(s.PredHome, s.PredAway) == outcome(s.ResHome, s.ResAway) {
		points = 1
	}

	return points
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func exactScore(predHome, predAway, resHome, resAway int) bool {
	return predHome == resHome && predAway == resAway
}

func outcome(home, away int) int {
	switch {
	case home > away:
		return 1
	case home < away:
		return -1
	default:
		return 0
	}
}
