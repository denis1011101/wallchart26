package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestApp(t *testing.T) *app {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &app{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func insertUser(t *testing.T, a *app, id int64, name, createdAt string) {
	t.Helper()
	_, err := a.db.Exec(
		`INSERT INTO users(id, name, token, created_at) VALUES(?, ?, ?, ?)`,
		id, name, name+"-token", createdAt,
	)
	if err != nil {
		t.Fatalf("insert user %s: %v", name, err)
	}
}

func insertMatch(t *testing.T, a *app, id int64, stage string, homeScore, awayScore int) {
	t.Helper()
	_, err := a.db.Exec(
		`INSERT INTO matches(id, stage, home, away, kickoff_utc, home_score, away_score)
		 VALUES(?, ?, 'H', 'A', '2026-06-01T18:00:00Z', ?, ?)`,
		id, stage, homeScore, awayScore,
	)
	if err != nil {
		t.Fatalf("insert match %d: %v", id, err)
	}
}

func insertPrediction(t *testing.T, a *app, userID, matchID int64, home, away int) {
	t.Helper()
	_, err := a.db.Exec(
		`INSERT INTO predictions(user_id, match_id, home, away, created_at, updated_at)
		 VALUES(?, ?, ?, ?, '2026-05-01T12:00:00Z', '2026-05-01T12:00:00Z')`,
		userID, matchID, home, away,
	)
	if err != nil {
		t.Fatalf("insert prediction u%d m%d: %v", userID, matchID, err)
	}
}

// TestLeaderboardDoublesPlayoffPoints exercises the full
// m.stage -> awardPoints -> Points/sorting chain through the SQL query, so a
// future regression in the SELECT or Scan would be caught.
func TestLeaderboardDoublesPlayoffPoints(t *testing.T) {
	a := newTestApp(t)

	insertUser(t, a, 1, "Ann", "2026-01-01T00:00:00Z")
	insertUser(t, a, 2, "Bob", "2026-01-02T00:00:00Z")

	insertMatch(t, a, 10, "Group", 2, 1) // group result 2:1
	insertMatch(t, a, 20, "Final", 1, 0) // playoff result 1:0

	// Ann: exact on both -> group 4, playoff 4*2=8 => 12 points, 2 exact.
	insertPrediction(t, a, 1, 10, 2, 1)
	insertPrediction(t, a, 1, 20, 1, 0)
	// Bob: exact group (4), playoff right outcome only 3:1 -> 1*2=2 => 6 points, 1 exact.
	insertPrediction(t, a, 2, 10, 2, 1)
	insertPrediction(t, a, 2, 20, 3, 1)

	board, err := a.leaderboard(context.Background())
	if err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	if len(board) != 2 {
		t.Fatalf("board len = %d, want 2", len(board))
	}

	if board[0].Name != "Ann" || board[0].Points != 12 || board[0].Exact != 2 {
		t.Fatalf("board[0] = %+v, want Ann/12/2", board[0])
	}
	if board[1].Name != "Bob" || board[1].Points != 6 || board[1].Exact != 1 {
		t.Fatalf("board[1] = %+v, want Bob/6/1", board[1])
	}
	if board[0].Predictions != 2 || board[1].Predictions != 2 {
		t.Fatalf("predictions count = %d/%d, want 2/2", board[0].Predictions, board[1].Predictions)
	}
}
