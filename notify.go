package main

import (
	"context"
	"database/sql"
	"log"
	"strconv"
	"strings"
	"time"
)

const (
	// notifyInterval is how often the background pass runs. Triggers are daily
	// or coarser, and the weekly cap is far coarser than an hour, so hourly is
	// plenty without risking spam.
	notifyInterval = time.Hour
	// notifyCooldown is the minimum gap between any two emails to one user.
	notifyCooldown = 3 * 24 * time.Hour
	// notifySendTimeout bounds a single message delivery.
	notifySendTimeout = 30 * time.Second
)

type notifyKind string

const (
	kindMissingPrediction notifyKind = "missing_prediction"
	kindStageOpen         notifyKind = "stage_open"
	kindPlayoffStart      notifyKind = "playoff_start"
	kindLeaderChange      notifyKind = "leader_change"
)

// recipient is a user eligible to receive notification emails.
type recipient struct {
	ID        int64
	Email     string
	Lang      string
	Token     string
	CreatedAt string
	LastSent  sql.NullString
}

// runNotifier ticks until the context is cancelled, running one notification
// pass per tick. A single goroutine means passes can never overlap, so no
// locking is needed; shutdown is graceful via ctx cancellation.
func (a *app) runNotifier(ctx context.Context) {
	ticker := time.NewTicker(notifyInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.runNotifications(ctx); err != nil {
				log.Printf("notifications: %v", err)
			}
		}
	}
}

// runNotifications evaluates every trigger for every eligible recipient. Most
// triggers are checked in priority order and capped at one email per user per
// pass plus the cooldown. The stage-open announcement is the exception: a stage
// is only open for a short window, so it is sent the moment a stage opens,
// bypassing both the priority order and the cooldown (deduped once per stage).
func (a *app) runNotifications(ctx context.Context) error {
	now := a.now()

	playoffStarted, err := a.playoffsStarted(ctx, now)
	if err != nil {
		return err
	}
	changedLeaderID, changedLeaderName, leaderEventAt, err := a.leaderToAnnounce(ctx)
	if err != nil {
		return err
	}

	recipients, err := a.notifyRecipients(ctx)
	if err != nil {
		return err
	}

	today := now.UTC().Format("2006-01-02")
	for _, r := range recipients {
		// Stage-open announcements jump the queue: send immediately, ignoring the
		// cooldown, so they can't arrive after the stage has already been played.
		// Gated on an explicit LOCK_STAGES so an unconfigured prod (every stage
		// "open") doesn't blast one email per stage.
		if a.announceStages {
			stage, err := a.openStageForUser(ctx, r.ID, now)
			if err != nil {
				return err
			}
			if stage != "" {
				if err := a.sendNotification(ctx, r, kindStageOpen, "stage_open:"+stage, stage); err != nil {
					log.Printf("notify %s -> %s: %v", kindStageOpen, r.Email, err)
				}
				// One email per user per pass; cooldown-gated triggers wait for a later pass.
				continue
			}
		}

		if withinCooldown(r.LastSent, now) {
			continue
		}
		kind, dedupeKey, detail, err := a.pickTrigger(ctx, r, now, today, playoffStarted, changedLeaderID, changedLeaderName, leaderEventAt)
		if err != nil {
			return err
		}
		if kind == "" {
			continue
		}
		if err := a.sendNotification(ctx, r, kind, dedupeKey, detail); err != nil {
			log.Printf("notify %s -> %s: %v", kind, r.Email, err)
		}
	}
	return nil
}

// pickTrigger returns the first applicable, not-yet-sent trigger for a
// recipient, in priority order: missing prediction today, playoff start, then
// leader change. The returned detail string is the trigger-specific payload for
// the message (the leader name). Stage-open announcements are handled separately
// in runNotifications because they bypass the cooldown and priority order.
func (a *app) pickTrigger(ctx context.Context, r recipient, now time.Time, today string, playoffStarted bool, changedLeaderID int64, changedLeaderName, leaderEventAt string) (notifyKind, string, string, error) {
	missing, err := a.hasMissingPredictionToday(ctx, r.ID, now)
	if err != nil {
		return "", "", "", err
	}
	if missing {
		key := "missing:" + today
		sent, err := a.alreadySent(ctx, r.ID, key)
		if err != nil {
			return "", "", "", err
		}
		if !sent {
			return kindMissingPrediction, key, "", nil
		}
	}

	if playoffStarted {
		key := "playoff"
		sent, err := a.alreadySent(ctx, r.ID, key)
		if err != nil {
			return "", "", "", err
		}
		if !sent {
			return kindPlayoffStart, key, "", nil
		}
	}

	// Only announce a leader change to users who already existed when it
	// happened — a newcomer registering afterwards didn't witness any change.
	if changedLeaderID != 0 && (leaderEventAt == "" || r.CreatedAt <= leaderEventAt) {
		key := "leader:" + strconv.FormatInt(changedLeaderID, 10)
		sent, err := a.alreadySent(ctx, r.ID, key)
		if err != nil {
			return "", "", "", err
		}
		if !sent {
			return kindLeaderChange, key, changedLeaderName, nil
		}
	}

	return "", "", "", nil
}

func (a *app) sendNotification(ctx context.Context, r recipient, kind notifyKind, dedupeKey, detail string) error {
	subject, body := a.notifyMessage(r.Lang, kind, detail, r.Token)

	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	if err := a.sendEmail(sendCtx, r.Email, subject, body); err != nil {
		return err
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO notifications(user_id, kind, dedupe_key, sent_at) VALUES (?, ?, ?, ?)`,
		r.ID, string(kind), dedupeKey, a.now().Format(time.RFC3339),
	)
	return err
}

// notifyRecipients returns subscribed users with an email plus the timestamp of
// their most recent notification (for the cooldown check).
func (a *app) notifyRecipients(ctx context.Context) ([]recipient, error) {
	rows, err := a.db.QueryContext(ctx, `
SELECT u.id, u.email, u.lang, u.token, u.created_at, MAX(n.sent_at)
FROM users u
LEFT JOIN notifications n ON n.user_id = u.id
WHERE u.email IS NOT NULL AND u.email <> '' AND u.unsubscribed = 0
GROUP BY u.id, u.email, u.lang, u.token, u.created_at
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []recipient
	for rows.Next() {
		var r recipient
		if err := rows.Scan(&r.ID, &r.Email, &r.Lang, &r.Token, &r.CreatedAt, &r.LastSent); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func withinCooldown(lastSent sql.NullString, now time.Time) bool {
	if !lastSent.Valid || lastSent.String == "" {
		return false
	}
	last, err := time.Parse(time.RFC3339, lastSent.String)
	if err != nil {
		return false
	}
	return now.Sub(last) < notifyCooldown
}

func (a *app) alreadySent(ctx context.Context, userID int64, dedupeKey string) (bool, error) {
	var exists int
	err := a.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM notifications WHERE user_id = ? AND dedupe_key = ?)`,
		userID, dedupeKey,
	).Scan(&exists)
	return exists == 1, err
}

// hasMissingPredictionToday reports whether the user has a match kicking off
// later today (UTC) that they can still fill in but haven't. Matches already
// kicked off or locked behind the playoff lock are excluded — nagging about
// them would be pointless since the prediction can no longer be saved.
func (a *app) hasMissingPredictionToday(ctx context.Context, userID int64, now time.Time) (bool, error) {
	nowStr := now.UTC().Format(time.RFC3339)
	args := []any{nowStr, nowStr}

	var lockedClause strings.Builder
	if len(a.lockedStages) > 0 {
		lockedClause.WriteString("\tAND m.stage NOT IN (")
		first := true
		for s := range a.lockedStages {
			if !first {
				lockedClause.WriteString(", ")
			}
			lockedClause.WriteString("?")
			args = append(args, s)
			first = false
		}
		lockedClause.WriteString(")\n")
	}
	args = append(args, userID)

	var exists int
	err := a.db.QueryRowContext(ctx, `
SELECT EXISTS(
	SELECT 1 FROM matches m
	WHERE date(m.kickoff_utc) = date(?)
	AND m.kickoff_utc > ?
`+lockedClause.String()+`	AND NOT EXISTS (
		SELECT 1 FROM predictions p
		WHERE p.user_id = ? AND p.match_id = m.id
		AND p.home IS NOT NULL AND p.away IS NOT NULL
	)
)`, args...).Scan(&exists)
	return exists == 1, err
}

// openStageForUser returns the first knockout stage (in play order) that is open
// for predictions for this user, or "" if none. A stage qualifies when it is not
// locked, still has at least one match before kickoff, and the user has not yet
// been sent its "stage_open:<stage>" announcement. Evaluating the dedupe per
// user inside the scan is what lets a newly unlocked later stage be announced
// even while an earlier, already-announced stage still has upcoming matches.
func (a *app) openStageForUser(ctx context.Context, userID int64, now time.Time) (string, error) {
	nowStr := now.UTC().Format(time.RFC3339)
	for _, stage := range knockoutStages {
		if a.stageLocked(stage) {
			continue
		}
		var hasUpcoming int
		err := a.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM matches WHERE stage = ? AND kickoff_utc > ?)`,
			stage, nowStr,
		).Scan(&hasUpcoming)
		if err != nil {
			return "", err
		}
		if hasUpcoming == 0 {
			continue
		}
		sent, err := a.alreadySent(ctx, userID, "stage_open:"+stage)
		if err != nil {
			return "", err
		}
		if !sent {
			return stage, nil
		}
	}
	return "", nil
}

func (a *app) playoffsStarted(ctx context.Context, now time.Time) (bool, error) {
	var exists int
	err := a.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM matches WHERE stage <> 'Group' AND kickoff_utc <= ?)`,
		now.UTC().Format(time.RFC3339),
	).Scan(&exists)
	return exists == 1, err
}

// leaderToAnnounce returns the leader that should currently be announced, or
// (0, "") if there is nothing to announce.
//
// Two pieces of state are tracked: "leader" is the latest observed top (for
// change detection), and "announce_leader" is the leader a change email is owed
// for. The announcement target persists across passes until it is superseded by
// a newer change — it is NOT cleared when emails are sent. Per-user delivery is
// gated by the notifications dedupe key, so transient SMTP failures and
// per-user cooldowns are retried on later passes instead of being lost.
//
// The very first observed leader is recorded silently (not a "change").
//
// It also returns the timestamp of the change, so callers can avoid notifying
// users who registered after the leader changed.
func (a *app) leaderToAnnounce(ctx context.Context) (int64, string, string, error) {
	board, err := a.leaderboard(ctx)
	if err != nil {
		return 0, "", "", err
	}
	var currentID int64
	var currentName string
	if len(board) > 0 && board[0].Points > 0 {
		currentID = board[0].UserID
		currentName = board[0].Name
	}
	if currentID == 0 {
		return 0, "", "", nil
	}
	current := strconv.FormatInt(currentID, 10)

	prev, err := a.getState(ctx, "leader")
	if err != nil {
		return 0, "", "", err
	}
	if prev != current {
		if prev == "" {
			// First observed leader: record it silently, no announcement.
			if err := a.setState(ctx, "leader", current); err != nil {
				return 0, "", "", err
			}
		} else {
			// Genuine change: record the latest leader, the announcement target
			// and the moment it happened in one transaction, so a crash can't
			// leave the state half-updated (losing the change, or losing the
			// timestamp the later-signup filter relies on).
			if err := a.recordLeaderChange(ctx, current, a.now().Format(time.RFC3339)); err != nil {
				return 0, "", "", err
			}
		}
	}

	announce, err := a.getState(ctx, "announce_leader")
	if err != nil {
		return 0, "", "", err
	}
	if announce == current {
		eventAt, err := a.getState(ctx, "announce_leader_at")
		if err != nil {
			return 0, "", "", err
		}
		return currentID, currentName, eventAt, nil
	}
	return 0, "", "", nil
}

func (a *app) getState(ctx context.Context, key string) (string, error) {
	var value string
	err := a.db.QueryRowContext(ctx, `SELECT value FROM app_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (a *app) setState(ctx context.Context, key, value string) error {
	_, err := a.db.ExecContext(ctx, stateUpsert, key, value)
	return err
}

const stateUpsert = `INSERT INTO app_state(key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`

// recordLeaderChange atomically advances the tracked leader and the
// announcement target plus its timestamp, so a partial failure can never leave
// the change lost or the timestamp missing.
func (a *app) recordLeaderChange(ctx context.Context, leaderID, eventAt string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, stateUpsert, "leader", leaderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, stateUpsert, "announce_leader", leaderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, stateUpsert, "announce_leader_at", eventAt); err != nil {
		return err
	}
	return tx.Commit()
}
