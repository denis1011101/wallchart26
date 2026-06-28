package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
)

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	current, _ := a.currentUser(r)
	board, err := a.leaderboard(r.Context())
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, r, pageData{
		Title:       "Wallchart '26",
		CurrentUser: current,
		Leaderboard: board,
	}, HomePage)
}

func (a *app) leaderboardFragment(w http.ResponseWriter, r *http.Request) {
	board, err := a.leaderboard(r.Context())
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, r, pageData{
		Title:       "Wallchart '26",
		Leaderboard: board,
	}, LeaderboardPanel)
}

func (a *app) setLang(w http.ResponseWriter, r *http.Request) {
	lang := normalizeLang(r.URL.Query().Get("to"))
	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     langCookieName,
		Value:    lang,
		Path:     "/",
		HttpOnly: false,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/me", http.StatusSeeOther)
		return
	}
	a.render(w, r, pageData{
		Title:     t(localeFromRequest(r), "login.title"),
		NoIndex:   true,
		AuthEmail: strings.TrimSpace(r.URL.Query().Get("email")),
	}, LoginPage)
}

func (a *app) authRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	email, emailNorm, err := normalizeEmail(r.FormValue("email"))
	if err != nil {
		lang := localeFromRequest(r)
		a.render(w, r, pageData{
			Title:   t(lang, "login.title"),
			NoIndex: true,
			Message: t(lang, "auth.check"),
		}, LoginPage)
		return
	}
	name := cleanDisplayName(r.FormValue("name"), email)

	now := a.now()
	clientIP := a.clientIP(r)
	if a.canIssueLoginCode(r.Context(), emailNorm, now) && a.loginRateLimiter.Allow(clientIP, now) {
		code, err := randomDigits(6)
		if err != nil {
			a.serverError(w, err)
			return
		}
		_, err = a.db.ExecContext(r.Context(), `
INSERT INTO login_codes(email_norm, email, name, code_hash, expires_at, attempts, created_at)
VALUES (?, ?, ?, ?, ?, 0, ?)
ON CONFLICT(email_norm) DO UPDATE SET
	email = excluded.email,
	name = excluded.name,
	code_hash = excluded.code_hash,
	expires_at = excluded.expires_at,
	attempts = 0,
	created_at = excluded.created_at
`, emailNorm, email, name, hashLoginCode(emailNorm, code), now.Add(loginCodeTTL).Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			a.serverError(w, err)
			return
		}
		a.sendLoginCodeAsync(email, code, localeFromRequest(r))
	}

	lang := localeFromRequest(r)
	a.render(w, r, pageData{
		Title:     t(lang, "verify.title"),
		NoIndex:   true,
		Message:   t(lang, "auth.check"),
		AuthEmail: email,
		AuthName:  name,
	}, VerifyPage)
}

func (a *app) authVerify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	email, emailNorm, err := normalizeEmail(r.FormValue("email"))
	if err != nil {
		lang := localeFromRequest(r)
		a.render(w, r, pageData{Title: t(lang, "verify.title"), NoIndex: true, Message: t(lang, "auth.invalid")}, VerifyPage)
		return
	}
	name := cleanDisplayName(r.FormValue("name"), email)
	code := strings.TrimSpace(r.FormValue("code"))
	if !a.verifyLoginCode(r.Context(), emailNorm, code) {
		lang := localeFromRequest(r)
		a.render(w, r, pageData{
			Title:     t(lang, "verify.title"),
			NoIndex:   true,
			Message:   t(lang, "auth.invalid"),
			AuthEmail: email,
			AuthName:  name,
		}, VerifyPage)
		return
	}

	userID, err := a.findOrCreateUser(r.Context(), email, emailNorm, name, localeFromRequest(r))
	if err != nil {
		a.serverError(w, err)
		return
	}
	token, err := randomToken()
	if err != nil {
		a.serverError(w, err)
		return
	}
	now := a.now()
	_, err = a.db.ExecContext(r.Context(), `
INSERT INTO sessions(token, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)
`, token, userID, now.Add(sessionTTL).Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.setCookie(w, sessionCookieName, token, "/", int(sessionTTL.Seconds()))
	http.Redirect(w, r, "/me", http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE token = ?`, cookie.Value)
	}
	a.clearCookie(w, sessionCookieName, "/")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) unsubscribe(w http.ResponseWriter, r *http.Request) {
	lang := localeFromRequest(r)
	token := strings.TrimSpace(r.URL.Query().Get("t"))
	if token == "" {
		token = strings.TrimSpace(r.FormValue("t"))
	}
	data := pageData{Title: t(lang, "unsub.title"), NoIndex: true}

	valid := false
	if token != "" {
		var id int64
		err := a.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE token = ?`, token).Scan(&id)
		if err == nil {
			valid = true
		} else if !errors.Is(err, sql.ErrNoRows) {
			a.serverError(w, err)
			return
		}
	}

	if r.Method == http.MethodPost {
		if !valid {
			data.Message = t(lang, "unsub.invalid")
			a.render(w, r, data, UnsubscribePage)
			return
		}
		if _, err := a.db.ExecContext(r.Context(), `UPDATE users SET unsubscribed = 1 WHERE token = ?`, token); err != nil {
			a.serverError(w, err)
			return
		}
		data.Message = t(lang, "unsub.done")
		a.render(w, r, data, UnsubscribePage)
		return
	}

	if valid {
		data.Token = token
	}
	a.render(w, r, data, UnsubscribePage)
}

// nextMatchID returns the id of the earliest match that hasn't kicked off yet,
// or 0 if none. Matches are expected in chronological order. Used to scroll a
// blank to the nearest upcoming match on open.
func nextMatchID(matches []matchRow, now time.Time) int64 {
	for _, m := range matches {
		if m.Kickoff.After(now) {
			return m.ID
		}
	}
	return 0
}

func (a *app) me(w http.ResponseWriter, r *http.Request) {
	current, ok := a.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	matches, err := a.matchesForUser(r.Context(), current.ID, false)
	if err != nil {
		a.serverError(w, err)
		return
	}
	lang := localeFromRequest(r)
	a.render(w, r, pageData{
		Title:        t(lang, "me.title"),
		NoIndex:      true,
		CurrentUser:  current,
		Matches:      matches,
		TeamOptions:  localizedTeamOptions(lang),
		FocusMatchID: nextMatchID(matches, a.now()),
	}, MePage)
}

func (a *app) predict(w http.ResponseWriter, r *http.Request) {
	current, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Join first", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	matchID, err := strconv.ParseInt(r.FormValue("match_id"), 10, 64)
	if err != nil {
		http.Error(w, "Bad match", http.StatusBadRequest)
		return
	}
	home, homeOK, err := parseOptionalScore(r.FormValue("home"))
	if err != nil {
		http.Error(w, "Bad home score", http.StatusBadRequest)
		return
	}
	away, awayOK, err := parseOptionalScore(r.FormValue("away"))
	if err != nil {
		http.Error(w, "Bad away score", http.StatusBadRequest)
		return
	}

	var kickoffRaw, stage, homeTeam, awayTeam string
	if err := a.db.QueryRowContext(r.Context(), `SELECT kickoff_utc, stage, home, away FROM matches WHERE id = ?`, matchID).Scan(&kickoffRaw, &stage, &homeTeam, &awayTeam); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Match not found", http.StatusNotFound)
			return
		}
		a.serverError(w, err)
		return
	}
	kickoff, err := time.Parse(time.RFC3339, kickoffRaw)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if a.stageLocked(stage) {
		http.Error(w, "Prediction is locked", http.StatusConflict)
		return
	}
	if !kickoff.After(a.now()) {
		http.Error(w, "Prediction is locked", http.StatusConflict)
		return
	}
	if homeOK != awayOK {
		http.Error(w, "Fill both scores or neither", http.StatusBadRequest)
		return
	}
	if !homeOK {
		http.Error(w, "Fill both scores", http.StatusBadRequest)
		return
	}

	var homeValue any = nil
	var awayValue any = nil
	if homeOK {
		homeValue = home
		awayValue = away
	}

	now := a.now().Format(time.RFC3339)
	_, err = a.db.ExecContext(r.Context(), `
INSERT INTO predictions(user_id, match_id, home, away, home_team, away_team, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, match_id) DO UPDATE SET
	home = excluded.home,
	away = excluded.away,
	home_team = excluded.home_team,
	away_team = excluded.away_team,
	updated_at = excluded.updated_at
`, current.ID, matchID, homeValue, awayValue, homeTeam, awayTeam, now, now)
	if err != nil {
		a.serverError(w, err)
		return
	}
	// Keep the user's notification language in sync with their current choice.
	lang := localeFromRequest(r)
	_, _ = a.db.ExecContext(r.Context(), `UPDATE users SET lang = ? WHERE id = ? AND lang <> ?`, lang, current.ID, lang)
	if r.Header.Get("HX-Request") == "true" {
		a.renderComponent(w, r, SavedStatus(matchID, lang))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) admin(w http.ResponseWriter, r *http.Request) {
	current, ok := a.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !a.isAdmin(current) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	matches, err := a.matchesForUser(r.Context(), 0, true)
	if err != nil {
		a.serverError(w, err)
		return
	}
	lang := localeFromRequest(r)
	a.render(w, r, pageData{
		Title:        t(lang, "admin.title"),
		NoIndex:      true,
		CurrentUser:  current,
		Matches:      matches,
		TeamOptions:  localizedTeamOptions(lang),
		FocusMatchID: nextMatchID(matches, a.now()),
	}, AdminPage)
}

func (a *app) adminResult(w http.ResponseWriter, r *http.Request) {
	current, ok := a.currentUser(r)
	if !ok || !a.isAdmin(current) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	matchID, err := strconv.ParseInt(r.FormValue("match_id"), 10, 64)
	if err != nil {
		http.Error(w, "Bad match", http.StatusBadRequest)
		return
	}
	homeTeam := resolveTeamName(r.FormValue("home_team"))
	awayTeam := resolveTeamName(r.FormValue("away_team"))
	if homeTeam == "" || awayTeam == "" {
		http.Error(w, "Teams are required", http.StatusBadRequest)
		return
	}
	var currentHome, currentAway string
	lang := localeFromRequest(r)
	if err := a.db.QueryRowContext(r.Context(), `SELECT home, away FROM matches WHERE id = ?`, matchID).Scan(&currentHome, &currentAway); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Match not found", http.StatusNotFound)
			return
		}
		a.serverError(w, err)
		return
	}
	if strings.HasPrefix(currentHome, "TBD") && normalizeText(r.FormValue("home_team")) == normalizeText(t(lang, "team.tbd")) {
		homeTeam = currentHome
	}
	if strings.HasPrefix(currentAway, "TBD") && normalizeText(r.FormValue("away_team")) == normalizeText(t(lang, "team.tbd")) {
		awayTeam = currentAway
	}

	homeScore, homeOK, err := parseOptionalScore(r.FormValue("home_score"))
	if err != nil {
		http.Error(w, "Bad home score", http.StatusBadRequest)
		return
	}
	awayScore, awayOK, err := parseOptionalScore(r.FormValue("away_score"))
	if err != nil {
		http.Error(w, "Bad away score", http.StatusBadRequest)
		return
	}
	var homeValue any = nil
	var awayValue any = nil
	if homeOK && awayOK {
		homeValue = homeScore
		awayValue = awayScore
	} else if homeOK != awayOK {
		http.Error(w, "Fill both scores or neither", http.StatusBadRequest)
		return
	}

	_, err = a.db.ExecContext(r.Context(), `
UPDATE matches SET home = ?, away = ?, home_score = ?, away_score = ? WHERE id = ?
`, homeTeam, awayTeam, homeValue, awayValue, matchID)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		a.renderComponent(w, r, AdminStatus(matchID, lang, true))
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *app) userPage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	viewed, err := a.userByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		a.serverError(w, err)
		return
	}
	current, _ := a.currentUser(r)
	matches, err := a.matchesForUser(r.Context(), id, false)
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, r, pageData{
		Title:        viewed.Name,
		NoIndex:      true,
		CurrentUser:  current,
		ViewedUser:   viewed,
		Matches:      matches,
		FocusMatchID: nextMatchID(matches, a.now()),
	}, UserPage)
}

func (a *app) currentUser(r *http.Request) (*user, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	var u user
	err = a.db.QueryRowContext(r.Context(), `
SELECT u.id, u.name, COALESCE(u.email, ''), COALESCE(u.email_norm, '')
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token = ? AND s.expires_at > ?
`, cookie.Value, a.now().Format(time.RFC3339)).Scan(&u.ID, &u.Name, &u.Email, &u.EmailNorm)
	if err != nil {
		return nil, false
	}
	return &u, true
}

func (a *app) setCookie(w http.ResponseWriter, name, value, path string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func (a *app) clearCookie(w http.ResponseWriter, name, path string) {
	a.setCookie(w, name, "", path, -1)
}

func (a *app) render(w http.ResponseWriter, r *http.Request, data pageData, page func(pageData) templ.Component) {
	if data.Lang == "" {
		data.Lang = localeFromRequest(r)
	}
	if data.Timezone == "" {
		data.Timezone = timezoneFromRequest(r)
	}
	if data.Path == "" {
		data.Path = r.URL.Path
	}
	if data.Description == "" {
		data.Description = t(data.Lang, "meta.description")
	}
	a.renderComponent(w, r, page(data))
}

func (a *app) renderComponent(w http.ResponseWriter, r *http.Request, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		log.Printf("render component: %v", err)
	}
}

func (a *app) serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	http.Error(w, "Something went wrong", http.StatusInternalServerError)
}

func parseScore(raw string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 || v > 99 {
		return 0, errors.New("score must be 0..99")
	}
	return v, nil
}

func parseOptionalScore(raw string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	v, err := parseScore(raw)
	return v, true, err
}

func cleanTeamGuess(raw string) string {
	raw = collapseSpace(raw)
	if len(raw) > 80 {
		raw = raw[:80]
	}
	return raw
}

func cleanDisplayName(raw, email string) string {
	name := collapseSpace(raw)
	if name == "" {
		if at := strings.IndexByte(email, '@'); at > 0 {
			name = email[:at]
		} else {
			name = "Player"
		}
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
