package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
)

const defaultTimezone = "Europe/Moscow"

var messages = map[string]map[string]string{
	"en": {
		"brand":                   "Wallchart '26",
		"footer.author":           "Author",
		"meta.description":        "Predict every score of the FIFA World Cup 2026. The office score-prediction sheet, online: 104 matches, one leaderboard, no ads, no money. Sign in by email and fill in your cells.",
		"nav.leaderboard":         "Leaderboard",
		"nav.mychart":             "My chart",
		"nav.login":               "Log in",
		"nav.logout":              "Logout",
		"hero.eyebrow":            "FIFA World Cup 2026 · 104 matches · one winner",
		"hero.lede":               "The office sheet of paper for score predictions, online.",
		"hero.open":               "Open my chart",
		"auth.email":              "Email",
		"auth.name":               "Display name",
		"auth.sendcode":           "Email code",
		"auth.verify":             "Verify",
		"auth.code":               "6-digit code",
		"auth.check":              "Check your email for a code if that address can sign in.",
		"auth.invalid":            "Invalid or expired code.",
		"login.title":             "Log in",
		"verify.title":            "Enter code",
		"leaderboard.players":     "players",
		"leaderboard.player":      "player",
		"leaderboard.players_few": "players",
		"table.rank":              "#",
		"table.name":              "Name",
		"table.points":            "Points",
		"table.exact":             "Exact",
		"table.predictions":       "Predictions",
		"table.empty":             "No players yet.",
		"scoring.title":           "How points work",
		"scoring.exact":           "Exact score",
		"scoring.diff":            "Correct goal difference",
		"scoring.outcome":         "Correct result (win / draw / loss)",
		"scoring.note":            "Only the best matching tier counts.",
		"me.title":                "My wallchart",
		"me.autosave":             "Autosaves on blur",
		"me.saved":                "Saved",
		"user.unlock":             "Player's predictions",
		"match.vs":                "vs",
		"match.hidden":            "No prediction",
		"match.result":            "Result",
		"match.locked":            "Locked",
		"match.open":              "Open",
		"match.hometeam":          "Home team",
		"match.awayteam":          "Away team",
		"admin.title":             "Results",
		"admin.manual":            "Autosaves on blur",
		"admin.save":              "Save",
		"admin.saved":             "Saved",
		"intro.title":             "Before kickoff",
		"intro.p1":                "Every big tournament, my dad's office had the same ritual: someone printed out the full match schedule, with little empty cells next to every game, and everyone filled in their scores with a pen.",
		"intro.p2":                "This is that sheet of paper, online. No ads, no money, no passwords. Sign in by email, fill in your cells, and may the best guesser win.",
		"intro.start":             "Start guessing",
		"tz.label":                "Time zone",
		"tz.auto":                 "Auto",
		"tz.moscow":               "Moscow",
		"tz.kaliningrad":          "Kaliningrad",
		"tz.samara":               "Samara",
		"tz.yekaterinburg":        "Yekaterinburg",
		"tz.novosibirsk":          "Novosibirsk",
		"tz.vladivostok":          "Vladivostok",
		"tz.utc":                  "UTC",
		"team.tbd":                "TBD",
		"email.subject":           "Wallchart '26 login code",
		"email.body":              "Your Wallchart '26 code is %s. It expires in 10 minutes.",
	},
	"ru": {
		"brand":                   "Wallchart '26",
		"footer.author":           "Автор",
		"meta.description":        "Прогнозируйте счёт каждого матча чемпионата мира 2026. Офисный бланк прогнозов, только онлайн: 104 матча, одна таблица лидеров, без рекламы и денег. Вход по почте — заполняйте клетки.",
		"nav.leaderboard":         "Таблица",
		"nav.mychart":             "Мой бланк",
		"nav.login":               "Войти",
		"nav.logout":              "Выйти",
		"hero.eyebrow":            "Чемпионат мира 2026 · 104 матча · один победитель",
		"hero.lede":               "Офисный бланк прогнозов, только онлайн.",
		"hero.open":               "Открыть мой бланк",
		"auth.email":              "Эл. почта",
		"auth.name":               "Имя",
		"auth.sendcode":           "Прислать код",
		"auth.verify":             "Подтвердить",
		"auth.code":               "Код из 6 цифр",
		"auth.check":              "Проверьте почту: если адрес может войти, туда придёт код.",
		"auth.invalid":            "Код недействителен или истёк.",
		"login.title":             "Вход",
		"verify.title":            "Введите код",
		"leaderboard.players":     "игроков",
		"leaderboard.player":      "игрок",
		"leaderboard.players_few": "игрока",
		"table.rank":              "#",
		"table.name":              "Имя",
		"table.points":            "Очки",
		"table.exact":             "Точных",
		"table.predictions":       "Прогнозов",
		"table.empty":             "Пока нет игроков.",
		"scoring.title":           "Как считаются очки",
		"scoring.exact":           "Точный счёт",
		"scoring.diff":            "Угаданная разница мячей",
		"scoring.outcome":         "Угаданный исход (победа / ничья / поражение)",
		"scoring.note":            "Засчитывается только лучший из уровней.",
		"me.title":                "Мой бланк",
		"me.autosave":             "Сохраняется автоматически",
		"me.saved":                "Сохранено",
		"user.unlock":             "Прогнозы игрока",
		"match.vs":                "—",
		"match.hidden":            "Нет прогноза",
		"match.result":            "Результат",
		"match.locked":            "Закрыто",
		"match.open":              "Открыто",
		"match.hometeam":          "Хозяева",
		"match.awayteam":          "Гости",
		"admin.title":             "Результаты",
		"admin.manual":            "Сохраняется автоматически",
		"admin.save":              "Сохранить",
		"admin.saved":             "Сохранено",
		"intro.title":             "Перед стартом",
		"intro.p1":                "Каждый крупный турнир в офисе моего отца был один и тот же ритуал: кто-то распечатывал полное расписание матчей с пустыми клетками у каждой игры, и все вписывали свои счёты ручкой.",
		"intro.p2":                "Это тот самый лист бумаги, только онлайн. Без рекламы, без денег, без паролей. Войдите по почте, заполните клетки, и пусть победит лучший прогнозист.",
		"intro.start":             "Начать угадывать",
		"tz.label":                "Часовой пояс",
		"tz.auto":                 "Авто",
		"tz.moscow":               "Москва",
		"tz.kaliningrad":          "Калининград",
		"tz.samara":               "Самара",
		"tz.yekaterinburg":        "Екатеринбург",
		"tz.novosibirsk":          "Новосибирск",
		"tz.vladivostok":          "Владивосток",
		"tz.utc":                  "UTC",
		"team.tbd":                "не определено",
		"email.subject":           "Код входа Wallchart '26",
		"email.body":              "Ваш код Wallchart '26: %s. Он действует 10 минут.",
	},
}

type teamCatalog struct {
	once    sync.Once
	options []string
	names   map[string]map[string]string
	reverse map[string]string
}

var embeddedTeams teamCatalog

func t(lang, key string) string {
	lang = normalizeLang(lang)
	if msg, ok := messages[lang][key]; ok {
		return msg
	}
	if msg, ok := messages["en"][key]; ok {
		return msg
	}
	return key
}

func plural(lang string, n int, one, few, many string) string {
	if normalizeLang(lang) != "ru" {
		if n == 1 {
			return one
		}
		return few
	}
	mod100 := n % 100
	mod10 := n % 10
	if mod100 >= 11 && mod100 <= 14 {
		return many
	}
	switch mod10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

func normalizeLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "ru":
		return "ru"
	case "en":
		return "en"
	default:
		return "ru"
	}
}

func localeFromRequest(r *http.Request) string {
	if c, err := r.Cookie(langCookieName); err == nil {
		return normalizeLang(c.Value)
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if strings.HasPrefix(part, "ru") {
			return "ru"
		}
		if strings.HasPrefix(part, "en") {
			return "en"
		}
	}
	return "ru"
}

func timezoneFromRequest(r *http.Request) string {
	if c, err := r.Cookie(tzCookieName); err == nil && validTimezoneCookie(c.Value) {
		return c.Value
	}
	return defaultTimezone
}

func validTimezoneCookie(tz string) bool {
	if tz == "auto" {
		return true
	}
	for _, option := range timezoneOptions() {
		if tz == option.Value {
			return true
		}
	}
	return false
}

type timezoneOption struct {
	Value    string
	LabelKey string
}

func timezoneOptions() []timezoneOption {
	return []timezoneOption{
		{Value: defaultTimezone, LabelKey: "tz.moscow"},
		{Value: "Europe/Kaliningrad", LabelKey: "tz.kaliningrad"},
		{Value: "Europe/Samara", LabelKey: "tz.samara"},
		{Value: "Asia/Yekaterinburg", LabelKey: "tz.yekaterinburg"},
		{Value: "Asia/Novosibirsk", LabelKey: "tz.novosibirsk"},
		{Value: "Asia/Vladivostok", LabelKey: "tz.vladivostok"},
		{Value: "UTC", LabelKey: "tz.utc"},
	}
}

func teamName(key, lang string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "TBD") {
		return t(lang, "team.tbd")
	}
	if normalizeLang(lang) == "ru" {
		if names := embeddedTeamNames(); names != nil {
			if byLang, ok := names[key]; ok {
				if ru := strings.TrimSpace(byLang["ru"]); ru != "" {
					return ru
				}
			}
		}
	}
	return key
}

func resolveTeamName(raw string) string {
	cleaned := cleanTeamGuess(raw)
	if cleaned == "" || strings.HasPrefix(cleaned, "TBD") {
		return cleaned
	}
	if key, ok := embeddedTeamReverse()[normalizeText(cleaned)]; ok {
		return key
	}
	return cleaned
}

func localizedTeamOptions(lang string) []string {
	options := embeddedTeamOptions()
	out := make([]string, 0, len(options))
	for _, key := range options {
		out = append(out, teamName(key, lang))
	}
	return out
}

func embeddedTeamOptions() []string {
	embeddedTeams.load()
	return append([]string(nil), embeddedTeams.options...)
}

func embeddedTeamNames() map[string]map[string]string {
	embeddedTeams.load()
	return embeddedTeams.names
}

func embeddedTeamReverse() map[string]string {
	embeddedTeams.load()
	return embeddedTeams.reverse
}

func (c *teamCatalog) load() {
	c.once.Do(func() {
		raw, err := embedded.ReadFile("fixtures.json")
		if err != nil {
			c.names = map[string]map[string]string{}
			c.reverse = map[string]string{}
			return
		}
		var file fixtureFile
		if err := json.Unmarshal(raw, &file); err != nil {
			c.names = map[string]map[string]string{}
			c.reverse = map[string]string{}
			return
		}
		seen := map[string]struct{}{}
		for _, group := range file.Groups {
			for _, team := range group.Teams {
				if _, ok := seen[team]; ok {
					continue
				}
				seen[team] = struct{}{}
				c.options = append(c.options, team)
			}
		}
		c.names = file.TeamNames
		if c.names == nil {
			c.names = map[string]map[string]string{}
		}
		c.reverse = map[string]string{}
		for _, key := range c.options {
			c.reverse[normalizeText(key)] = key
			for _, name := range c.names[key] {
				if name = strings.TrimSpace(name); name != "" {
					c.reverse[normalizeText(name)] = key
				}
			}
		}
	})
}

func isoTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func emailSubject(lang string) string {
	return t(lang, "email.subject")
}

func emailBody(lang, code string) string {
	return fmt.Sprintf(t(lang, "email.body"), code)
}

func formatKickoff(t time.Time) string {
	return t.UTC().Format("Jan 02 15:04 UTC")
}

func stageLabel(m matchRow, lang string) string {
	if m.Stage == "Group" {
		if normalizeLang(lang) == "ru" {
			return "Группа " + m.GroupName
		}
		return "Group " + m.GroupName
	}
	if normalizeLang(lang) == "ru" {
		switch m.Stage {
		case "Round of 32":
			return "1/16 финала"
		case "Round of 16":
			return "1/8 финала"
		case "Quarter-final":
			return "1/4 финала"
		case "Semi-final":
			return "1/2 финала"
		case "Third place":
			return "Матч за 3-е место"
		case "Final":
			return "Финал"
		}
	}
	return m.Stage
}

func predictionText(m matchRow) string {
	if !m.PredHome.Valid || !m.PredAway.Valid {
		return ""
	}
	return fmt.Sprintf("%d-%d", m.PredHome.Int64, m.PredAway.Int64)
}

// publicPredictionText renders another player's prediction for the read-only
// page: the score and, for knockout ties, the picked teams (which can exist
// without a score).
func publicPredictionText(m matchRow, lang string) string {
	score := predictionText(m)
	if isKnockout(m) {
		home := teamName(teamGuessHome(m), lang)
		away := teamName(teamGuessAway(m), lang)
		var pick string
		switch {
		case home != "" && away != "":
			pick = home + " – " + away
		case home != "":
			pick = home
		case away != "":
			pick = away
		}
		if pick != "" {
			if score != "" {
				return pick + " · " + score
			}
			return pick
		}
	}
	return score
}

func resultText(m matchRow) string {
	if !m.HomeScore.Valid || !m.AwayScore.Valid {
		return ""
	}
	return fmt.Sprintf("%d-%d", m.HomeScore.Int64, m.AwayScore.Int64)
}

func isKnockout(m matchRow) bool {
	return m.Stage != "Group"
}

func teamGuessHome(m matchRow) string {
	if m.PredHomeTeam.Valid {
		return m.PredHomeTeam.String
	}
	return ""
}

func teamGuessAway(m matchRow) string {
	if m.PredAwayTeam.Valid {
		return m.PredAwayTeam.String
	}
	return ""
}

func int64Text(v int64) string {
	return strconv.FormatInt(v, 10)
}

func intText(v int) string {
	return strconv.Itoa(v)
}

func userURL(id int64) templ.SafeURL {
	return templ.SafeURL("/u/" + int64Text(id))
}

func scoreValue(v sql.NullInt64) string {
	if !v.Valid {
		return ""
	}
	return strconv.FormatInt(v.Int64, 10)
}
