package main

import (
	"context"
	"sort"
	"testing"
	"time"
)

func fixedTime(t *testing.T, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return ts
}

func insertEmailUser(t *testing.T, a *app, id int64, name, email, lang string, unsubscribed int) {
	t.Helper()
	insertEmailUserAt(t, a, id, name, email, lang, unsubscribed, "2026-01-01T00:00:00Z")
}

func insertEmailUserAt(t *testing.T, a *app, id int64, name, email, lang string, unsubscribed int, createdAt string) {
	t.Helper()
	_, err := a.db.Exec(
		`INSERT INTO users(id, name, email, email_norm, token, lang, unsubscribed, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, email, email, name+"-token", lang, unsubscribed, createdAt,
	)
	if err != nil {
		t.Fatalf("insert email user %s: %v", name, err)
	}
}

func insertMatchKickoff(t *testing.T, a *app, id int64, stage, kickoff string, home, away *int) {
	t.Helper()
	_, err := a.db.Exec(
		`INSERT INTO matches(id, stage, home, away, kickoff_utc, home_score, away_score)
		 VALUES(?, ?, 'H', 'A', ?, ?, ?)`,
		id, stage, kickoff, home, away,
	)
	if err != nil {
		t.Fatalf("insert match %d: %v", id, err)
	}
}

func sentKinds(t *testing.T, a *app, userID int64) []string {
	t.Helper()
	rows, err := a.db.Query(`SELECT kind FROM notifications WHERE user_id = ? ORDER BY kind`, userID)
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	defer rows.Close()
	var kinds []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("scan kind: %v", err)
		}
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}

func ptr(v int) *int { return &v }

func TestRunNotificationsMissingPrediction(t *testing.T) {
	a := newTestApp(t)
	a.now = func() time.Time { return fixedTime(t, "2026-06-15T12:00:00Z") }

	insertEmailUser(t, a, 1, "Ann", "ann@example.com", "en", 0)
	insertEmailUser(t, a, 2, "Bob", "bob@example.com", "ru", 0)
	// Match today, no result yet.
	insertMatchKickoff(t, a, 100, "Group", "2026-06-15T18:00:00Z", nil, nil)
	// Bob already filled it in; Ann did not.
	insertPrediction(t, a, 2, 100, 1, 0)

	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}

	if got := sentKinds(t, a, 1); len(got) != 1 || got[0] != string(kindMissingPrediction) {
		t.Fatalf("Ann kinds = %v, want [missing_prediction]", got)
	}
	if got := sentKinds(t, a, 2); len(got) != 0 {
		t.Fatalf("Bob kinds = %v, want none", got)
	}
}

func TestRunNotificationsCooldown(t *testing.T) {
	a := newTestApp(t)
	a.now = func() time.Time { return fixedTime(t, "2026-06-15T12:00:00Z") }

	insertEmailUser(t, a, 1, "Ann", "ann@example.com", "en", 0)
	insertMatchKickoff(t, a, 100, "Group", "2026-06-15T18:00:00Z", nil, nil)
	// A prior email two days ago — within the cooldown.
	_, err := a.db.Exec(
		`INSERT INTO notifications(user_id, kind, dedupe_key, sent_at) VALUES (1, 'leader_change', 'leader:9', ?)`,
		"2026-06-13T12:00:00Z",
	)
	if err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}

	// Still only the seeded one; the missing-prediction email is capped.
	if got := sentKinds(t, a, 1); len(got) != 1 || got[0] != "leader_change" {
		t.Fatalf("Ann kinds = %v, want only the seeded leader_change", got)
	}
}

func TestRunNotificationsSkipsUnsubscribed(t *testing.T) {
	a := newTestApp(t)
	a.now = func() time.Time { return fixedTime(t, "2026-06-15T12:00:00Z") }

	insertEmailUser(t, a, 1, "Ann", "ann@example.com", "en", 1) // unsubscribed
	insertMatchKickoff(t, a, 100, "Group", "2026-06-15T18:00:00Z", nil, nil)

	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}

	if got := sentKinds(t, a, 1); len(got) != 0 {
		t.Fatalf("unsubscribed user kinds = %v, want none", got)
	}
}

func TestRunNotificationsPlayoffStart(t *testing.T) {
	a := newTestApp(t)
	a.now = func() time.Time { return fixedTime(t, "2026-07-01T12:00:00Z") }

	insertEmailUser(t, a, 1, "Ann", "ann@example.com", "en", 0)
	// A playoff match that already kicked off; no match today, nothing missing.
	insertMatchKickoff(t, a, 200, "Final", "2026-06-30T18:00:00Z", nil, nil)

	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}

	if got := sentKinds(t, a, 1); len(got) != 1 || got[0] != string(kindPlayoffStart) {
		t.Fatalf("Ann kinds = %v, want [playoff_start]", got)
	}
}

func TestRunNotificationsLeaderChange(t *testing.T) {
	a := newTestApp(t)
	a.now = func() time.Time { return fixedTime(t, "2026-06-20T12:00:00Z") }

	insertEmailUser(t, a, 1, "Ann", "ann@example.com", "en", 0)
	insertEmailUser(t, a, 2, "Bob", "bob@example.com", "ru", 0)
	// Seed a previous leader so a switch is a genuine change.
	if err := a.setState(context.Background(), "leader", "1"); err != nil {
		t.Fatalf("seed leader: %v", err)
	}
	// A finished group match in the past (not today): Bob predicts it exactly and
	// becomes the leader with points.
	insertMatchKickoff(t, a, 300, "Group", "2026-06-10T18:00:00Z", ptr(2), ptr(1))
	insertPrediction(t, a, 2, 300, 2, 1)

	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}

	// Everyone subscribed hears about the new leader.
	for _, uid := range []int64{1, 2} {
		got := sentKinds(t, a, uid)
		if len(got) != 1 || got[0] != string(kindLeaderChange) {
			t.Fatalf("user %d kinds = %v, want [leader_change]", uid, got)
		}
	}
	// app_state now points at the new leader.
	if v, _ := a.getState(context.Background(), "leader"); v != "2" {
		t.Fatalf("stored leader = %q, want 2", v)
	}
}

func TestRunNotificationsLeaderChangeRetriedAfterCooldown(t *testing.T) {
	a := newTestApp(t)
	now := fixedTime(t, "2026-06-20T12:00:00Z")
	a.now = func() time.Time { return now }

	insertEmailUser(t, a, 1, "Ann", "ann@example.com", "en", 0)
	if err := a.setState(context.Background(), "leader", "1"); err != nil {
		t.Fatalf("seed leader: %v", err)
	}
	// Ann just got another email, so she's in cooldown when the leader changes.
	if _, err := a.db.Exec(
		`INSERT INTO notifications(user_id, kind, dedupe_key, sent_at) VALUES (1, 'missing_prediction', 'missing:x', ?)`,
		"2026-06-20T00:00:00Z",
	); err != nil {
		t.Fatalf("seed cooldown notification: %v", err)
	}
	// Bob becomes the leader.
	insertEmailUser(t, a, 2, "Bob", "bob@example.com", "ru", 0)
	insertMatchKickoff(t, a, 300, "Group", "2026-06-10T18:00:00Z", ptr(2), ptr(1))
	insertPrediction(t, a, 2, 300, 2, 1)

	// Pass while Ann is capped: change detected, but Ann gets no leader email.
	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}
	if got := sentKinds(t, a, 1); len(got) != 1 || got[0] != string(kindMissingPrediction) {
		t.Fatalf("Ann (capped) kinds = %v, want only the seeded missing_prediction", got)
	}

	// Cooldown passes, leader unchanged: the announcement must still reach Ann.
	now = fixedTime(t, "2026-06-25T12:00:00Z")
	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}
	kinds := sentKinds(t, a, 1)
	found := false
	for _, k := range kinds {
		if k == string(kindLeaderChange) {
			found = true
		}
	}
	if !found {
		t.Fatalf("Ann kinds = %v, want leader_change retried after cooldown", kinds)
	}
}

func TestRunNotificationsLeaderChangeSkipsLaterSignups(t *testing.T) {
	a := newTestApp(t)
	a.now = func() time.Time { return fixedTime(t, "2026-06-20T12:00:00Z") }

	// Ann existed before the change; Carol registers after it.
	insertEmailUserAt(t, a, 1, "Ann", "ann@example.com", "en", 0, "2026-06-01T00:00:00Z")
	insertEmailUserAt(t, a, 3, "Carol", "carol@example.com", "en", 0, "2026-07-01T00:00:00Z")
	if err := a.setState(context.Background(), "leader", "1"); err != nil {
		t.Fatalf("seed leader: %v", err)
	}
	insertEmailUserAt(t, a, 2, "Bob", "bob@example.com", "ru", 0, "2026-06-02T00:00:00Z")
	insertMatchKickoff(t, a, 300, "Group", "2026-06-10T18:00:00Z", ptr(2), ptr(1))
	insertPrediction(t, a, 2, 300, 2, 1)

	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}

	if got := sentKinds(t, a, 1); len(got) != 1 || got[0] != string(kindLeaderChange) {
		t.Fatalf("Ann (pre-existing) kinds = %v, want [leader_change]", got)
	}
	if got := sentKinds(t, a, 3); len(got) != 0 {
		t.Fatalf("Carol (later signup) kinds = %v, want none", got)
	}
}

func TestRunNotificationsFirstLeaderIsNotChange(t *testing.T) {
	a := newTestApp(t)
	a.now = func() time.Time { return fixedTime(t, "2026-06-20T12:00:00Z") }

	insertEmailUser(t, a, 1, "Ann", "ann@example.com", "en", 0)
	insertMatchKickoff(t, a, 300, "Group", "2026-06-10T18:00:00Z", ptr(2), ptr(1))
	insertPrediction(t, a, 1, 300, 2, 1)

	if err := a.runNotifications(context.Background()); err != nil {
		t.Fatalf("runNotifications: %v", err)
	}

	if got := sentKinds(t, a, 1); len(got) != 0 {
		t.Fatalf("first-ever leader should not notify, got %v", got)
	}
	if v, _ := a.getState(context.Background(), "leader"); v != "1" {
		t.Fatalf("stored leader = %q, want 1 (seeded silently)", v)
	}
}
