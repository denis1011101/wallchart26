package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type fixtureFile struct {
	Groups    []fixtureGroup               `json:"groups"`
	TeamNames map[string]map[string]string `json:"team_names"`
}

type fixtureGroup struct {
	Name      string   `json:"name"`
	Teams     []string `json:"teams"`
	Matchdays []string `json:"matchdays"`
}

type fixtureMatch struct {
	ID        int
	Stage     string
	GroupName string
	Home      string
	Away      string
	Kickoff   time.Time
}

func normalizeDBPath(path string) string {
	if strings.HasPrefix(path, "file:") || filepath.Dir(path) != "." {
		return path
	}
	return "." + string(filepath.Separator) + path
}

func migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	email TEXT,
	email_norm TEXT,
	token TEXT NOT NULL UNIQUE,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS matches (
	id INTEGER PRIMARY KEY,
	stage TEXT NOT NULL,
	group_name TEXT NOT NULL DEFAULT '',
	home TEXT NOT NULL,
	away TEXT NOT NULL,
	kickoff_utc TEXT NOT NULL,
	home_score INTEGER,
	away_score INTEGER
);

CREATE TABLE IF NOT EXISTS predictions (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	match_id INTEGER NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
	home INTEGER,
	away INTEGER,
	home_team TEXT NOT NULL DEFAULT '',
	away_team TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(user_id, match_id)
);

CREATE TABLE IF NOT EXISTS login_codes (
	email_norm TEXT PRIMARY KEY,
	email TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	code_hash TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	token TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notifications (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind TEXT NOT NULL,
	dedupe_key TEXT NOT NULL,
	sent_at TEXT NOT NULL,
	UNIQUE(user_id, dedupe_key)
);

CREATE TABLE IF NOT EXISTS app_state (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`)
	if err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "users", "email", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "users", "email_norm", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "users", "lang", "TEXT NOT NULL DEFAULT 'ru'"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "users", "unsubscribed", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS users_email_norm_idx ON users(email_norm) WHERE email_norm IS NOT NULL`); err != nil {
		return err
	}
	return ensurePredictionsNullable(ctx, db)
}

func ensureColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func ensurePredictionsNullable(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(predictions)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	rebuild := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if (name == "home" || name == "away") && notnull == 1 {
			rebuild = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !rebuild {
		return nil
	}

	_, err = db.ExecContext(ctx, `
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE predictions_new (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	match_id INTEGER NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
	home INTEGER,
	away INTEGER,
	home_team TEXT NOT NULL DEFAULT '',
	away_team TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(user_id, match_id)
);
INSERT INTO predictions_new(user_id, match_id, home, away, home_team, away_team, created_at, updated_at)
SELECT user_id, match_id, home, away, home_team, away_team, created_at, updated_at FROM predictions;
DROP TABLE predictions;
ALTER TABLE predictions_new RENAME TO predictions;
COMMIT;
PRAGMA foreign_keys = ON;
`)
	return err
}

func seedFixtures(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matches`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	raw, err := embedded.ReadFile("fixtures.json")
	if err != nil {
		return err
	}
	var file fixtureFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return err
	}
	fixtures, err := buildFixtures(file)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO matches(id, stage, group_name, home, away, kickoff_utc)
VALUES (?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range fixtures {
		if _, err := stmt.ExecContext(ctx, m.ID, m.Stage, m.GroupName, m.Home, m.Away, m.Kickoff.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func buildFixtures(file fixtureFile) ([]fixtureMatch, error) {
	var fixtures []fixtureMatch
	pairs := [][2]int{{0, 1}, {2, 3}, {0, 2}, {3, 1}, {3, 0}, {1, 2}}
	id := 1

	for _, g := range file.Groups {
		if len(g.Teams) != 4 || len(g.Matchdays) != 3 {
			return nil, fmt.Errorf("group %s must have 4 teams and 3 matchdays", g.Name)
		}
		for day := 0; day < 3; day++ {
			for slot := 0; slot < 2; slot++ {
				date := g.Matchdays[day]
				hour := 18 + slot*3
				kickoff, err := time.Parse(time.RFC3339, fmt.Sprintf("%sT%02d:00:00Z", date, hour))
				if err != nil {
					return nil, err
				}
				pair := pairs[day*2+slot]
				fixtures = append(fixtures, fixtureMatch{
					ID: id, Stage: "Group", GroupName: g.Name,
					Home: g.Teams[pair[0]], Away: g.Teams[pair[1]], Kickoff: kickoff,
				})
				id++
			}
		}
	}

	knockouts := []struct {
		stage string
		count int
		dates []string
	}{
		{"Round of 32", 16, []string{"2026-06-28", "2026-06-29", "2026-06-30", "2026-07-01", "2026-07-02", "2026-07-03"}},
		{"Round of 16", 8, []string{"2026-07-04", "2026-07-05", "2026-07-06", "2026-07-07"}},
		{"Quarter-final", 4, []string{"2026-07-09", "2026-07-10", "2026-07-11"}},
		{"Semi-final", 2, []string{"2026-07-14", "2026-07-15"}},
		{"Third place", 1, []string{"2026-07-18"}},
		{"Final", 1, []string{"2026-07-19"}},
	}
	for _, round := range knockouts {
		for i := 0; i < round.count; i++ {
			date := round.dates[i%len(round.dates)]
			hour := 18 + (i%2)*3
			kickoff, err := time.Parse(time.RFC3339, fmt.Sprintf("%sT%02d:00:00Z", date, hour))
			if err != nil {
				return nil, err
			}
			label := fmt.Sprintf("%s %d", round.stage, i+1)
			if round.count == 1 {
				label = round.stage
			}
			fixtures = append(fixtures, fixtureMatch{
				ID: id, Stage: round.stage, Home: "TBD", Away: "TBD", Kickoff: kickoff,
				GroupName: "",
			})
			fixtures[len(fixtures)-1].Home = homePlaceholder(label)
			fixtures[len(fixtures)-1].Away = awayPlaceholder(label)
			id++
		}
	}
	if len(fixtures) != 104 {
		return nil, fmt.Errorf("built %d fixtures, want 104", len(fixtures))
	}
	return fixtures, nil
}

func homePlaceholder(label string) string {
	if label == "Final" || label == "Third place" {
		return "TBD"
	}
	return "TBD " + label + " home"
}

func awayPlaceholder(label string) string {
	if label == "Final" || label == "Third place" {
		return "TBD"
	}
	return "TBD " + label + " away"
}

func (a *app) userByID(ctx context.Context, id int64) (*user, error) {
	var u user
	err := a.db.QueryRowContext(ctx, `SELECT id, name, COALESCE(email, ''), COALESCE(email_norm, '') FROM users WHERE id = ?`, id).Scan(&u.ID, &u.Name, &u.Email, &u.EmailNorm)
	return &u, err
}

func (a *app) matchesForUser(ctx context.Context, userID int64, admin bool) ([]matchRow, error) {
	query := `
SELECT m.id, m.stage, m.group_name, m.home, m.away, m.kickoff_utc, m.home_score, m.away_score,
       p.home, p.away, p.home_team, p.away_team
FROM matches m
LEFT JOIN predictions p ON p.match_id = m.id AND p.user_id = ?
ORDER BY m.kickoff_utc, m.id
`
	rows, err := a.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []matchRow
	for rows.Next() {
		var m matchRow
		var kickoffRaw string
		if err := rows.Scan(&m.ID, &m.Stage, &m.GroupName, &m.Home, &m.Away, &kickoffRaw, &m.HomeScore, &m.AwayScore, &m.PredHome, &m.PredAway, &m.PredHomeTeam, &m.PredAwayTeam); err != nil {
			return nil, err
		}
		m.Kickoff, err = time.Parse(time.RFC3339, kickoffRaw)
		if err != nil {
			return nil, err
		}
		m.Locked = !admin && ((a.lockPlayoffs && m.Stage != "Group") || !m.Kickoff.After(a.now()))
		matches = append(matches, m)
	}
	return matches, rows.Err()
}

func (a *app) leaderboard(ctx context.Context) ([]leaderboardRow, error) {
	rows, err := a.db.QueryContext(ctx, `
SELECT u.id, u.name, p.user_id, m.stage, m.home_score, m.away_score, p.home, p.away
FROM users u
LEFT JOIN predictions p ON p.user_id = u.id
LEFT JOIN matches m ON m.id = p.match_id AND m.home_score IS NOT NULL AND m.away_score IS NOT NULL
ORDER BY u.created_at, u.id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byUser := map[int64]*leaderboardRow{}
	var order []int64
	for rows.Next() {
		var uid int64
		var name string
		var predUserID sql.NullInt64
		var stage sql.NullString
		var resHome, resAway, predHome, predAway sql.NullInt64
		if err := rows.Scan(&uid, &name, &predUserID, &stage, &resHome, &resAway, &predHome, &predAway); err != nil {
			return nil, err
		}
		row, ok := byUser[uid]
		if !ok {
			row = &leaderboardRow{UserID: uid, Name: name}
			byUser[uid] = row
			order = append(order, uid)
		}
		if predUserID.Valid {
			row.Predictions++
		}
		if predHome.Valid && predAway.Valid && resHome.Valid && resAway.Valid {
			got := awardPoints(stage.String, ScoreInput{
				PredHome: int(predHome.Int64), PredAway: int(predAway.Int64),
				ResHome: int(resHome.Int64), ResAway: int(resAway.Int64),
			})
			row.Points += got
			if exactScore(int(predHome.Int64), int(predAway.Int64), int(resHome.Int64), int(resAway.Int64)) {
				row.Exact++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	board := make([]leaderboardRow, 0, len(order))
	for _, id := range order {
		board = append(board, *byUser[id])
	}
	for i := 0; i < len(board); i++ {
		for j := i + 1; j < len(board); j++ {
			if board[j].Points > board[i].Points || (board[j].Points == board[i].Points && board[j].Exact > board[i].Exact) {
				board[i], board[j] = board[j], board[i]
			}
		}
	}
	return board, nil
}
