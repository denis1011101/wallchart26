package main

import (
	"testing"
	"time"
)

func TestNextMatchID(t *testing.T) {
	at := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return ts
	}
	now := at("2026-06-20T12:00:00Z")
	matches := []matchRow{
		{ID: 1, Kickoff: at("2026-06-18T18:00:00Z")}, // past
		{ID: 2, Kickoff: at("2026-06-20T11:00:00Z")}, // past (earlier today)
		{ID: 3, Kickoff: at("2026-06-20T18:00:00Z")}, // next upcoming
		{ID: 4, Kickoff: at("2026-06-21T18:00:00Z")}, // later
	}

	if got := nextMatchID(matches, now); got != 3 {
		t.Fatalf("nextMatchID = %d, want 3", got)
	}

	allPast := []matchRow{{ID: 1, Kickoff: at("2026-06-01T18:00:00Z")}}
	if got := nextMatchID(allPast, now); got != 0 {
		t.Fatalf("nextMatchID(all past) = %d, want 0", got)
	}

	if got := nextMatchID(nil, now); got != 0 {
		t.Fatalf("nextMatchID(nil) = %d, want 0", got)
	}
}
