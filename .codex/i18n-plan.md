# Wallchart '26 — localization (en/ru toggle) plan for Codex

Add a **language toggle (en/ru)**, **localized team names**, and a **selectable timezone**
for kickoff display. The app is otherwise complete (email-OTP auth, group + knockout fixtures,
per-match prediction, manual results, scoring). Do the tasks top-down; after each run
`make test` and `make build`.

## Scope guard — DO NOT touch the group-stage flow
Group stage stays exactly as it is: every match is predicted individually and locks at its own
kickoff (`Locked = !admin && !m.Kickoff.After(a.now())`, `main.go:923`; `predict()` returns 409
after kickoff, `main.go:723`). A user who joins late simply fills in whatever matches are still
open. **No changes to locking, scoring rules, or fixture structure.** This plan is presentation +
i18n only.

---

## Architecture decisions (read before coding)

### Locale
- Source of truth: cookie `lang` ∈ {`ru`,`en`}, **default `ru`** (audience is Russian-speaking).
- On a request with no cookie, fall back to `Accept-Language` (prefix match `ru` → ru, else en),
  then default ru.
- Setter: `GET /lang?to=ru&next=/me` sets the cookie (1 year, `SameSite=Lax`, `HttpOnly=false` is
  fine — it's not a secret) and 303-redirects to a **same-origin** `next` (validate it starts with
  `/` and not `//`). A small `<a>` toggle in the header.
- Plumb the resolved locale into every render via `pageData.Lang string`.

### Messages — dependency-free catalog
- New file `i18n.go`: `var messages = map[string]map[string]string{"en": {...}, "ru": {...}}`.
- Template func `t(lang, key string) string` (registered in the funcmap at `main.go:181`); returns
  the key itself if missing (so a missing translation is visible, not blank). Templates call
  `{{t $.Lang "leaderboard"}}` — `$` is the root pageData inside `range`/`if`.
- Russian plural helper for counts: `plural(lang, n, one, few, many string) string`
  (ru: 1 → one «игрок», 2–4 → few «игрока», 5–0/11–14 → many «игроков»; en: n==1 ? one : few).
- **Do not** pull in `golang.org/x/text` — keep it a hand-written map, matching the project's
  zero-extra-dep style.

### Team names — canonical key + per-locale display (this is the important one)
The toggle makes "store Russian in the DB" impossible on its own: English mode still needs English
names, so teams **must** be `key → {en, ru}`. Resolution:
- **Canonical key = the current English name** (e.g. `"Mexico"`). This means **no destructive DB
  migration** — `matches.home/away` already hold these strings and stay as-is.
- `fixtures.json`: keep `teams` as the English keys, and add a top-level
  `"team_names": { "Mexico": {"ru": "Мексика"}, ... }` block (en display = the key itself, so only
  `ru` needs listing). `loadTeamOptions` and a new `teamDisplay(key, lang)` read this.
- **Display**: in templates, wrap every `{{.Home}}` / `{{.Away}}` as `{{teamName .Home $.Lang}}`
  (new funcmap func). Knockout placeholders (`"TBD Round of 32 1 home"`, `main.go:524-536`) are not
  real teams — `teamName` should detect the `TBD` prefix and return a localized placeholder
  (ru: «не определено», en: "TBD"), **not** look them up in the map.
- **Datalist** (`me.html:47`): list localized names for the current locale (`value` = localized
  string the user sees).
- **Submit resolution** (`predict`, `main.go:683` + `cleanTeamGuess`): when a user types a knockout
  team guess, resolve the typed string (in either locale, via a reverse map normalized with the
  existing `normalizeText`) back to the **canonical key** before storing. Then `sameTeam`
  (`score.go:41`) keeps comparing canonical keys → scoring stays locale-independent. Store the key,
  not the localized text.
- **Admin** (`adminResult`, `main.go:785`; `admin.html:26-27`): admin types real teams into the
  knockout TBD slots; resolve the typed name → canonical key the same way before writing
  `matches.home/away`. Admin form fields should show the localized current value but persist the key.

### Timezone — selectable, default Europe/Moscow
- Kickoffs are already stored UTC (`kickoff_utc`) — keep that.
- Render client-side: replace `<time>{{formatKickoff .Kickoff}}</time>` with
  `<time datetime="{{isoTime .Kickoff}}" data-ts>{{formatKickoff .Kickoff}}</time>` (the server text
  is the no-JS UTC fallback). A small script formats every `[data-ts]` via
  `new Intl.DateTimeFormat(lang, {timeZone, dateStyle, timeStyle})` — this gets **both** localized
  month names and the chosen tz in one place, for free.
- tz choices persisted in cookie `tz` (default `Europe/Moscow`). Header `<select>` with a short list:
  `Europe/Moscow`, `Europe/Kaliningrad`, `Europe/Samara`, `Asia/Yekaterinburg`,
  `Asia/Novosibirsk`, `Asia/Vladivostok`, `UTC`, plus an "auto (browser)" option. The script reads
  the cookie; "auto" uses `Intl.DateTimeFormat().resolvedOptions().timeZone`.
- `isoTime(t) string` = `t.UTC().Format(time.RFC3339)` (new funcmap func). `formatKickoff` stays as
  the UTC fallback but its label should not claim a tz the JS will override — keep "Jun 02 15:04
  UTC" / localize the fallback minimally.

---

## Tasks

### 1. Locale plumbing
- Add `Lang string` to `pageData`; set it in `render`/handlers from a `a.locale(r)` helper that
  reads the cookie → `Accept-Language` → `ru`.
- Add `GET /lang` handler (validate `next`, set cookie, 303). Register route near the others.
- Register `t`, `teamName`, `plural`, `isoTime` in the funcmap (`main.go:180-186`).
- `<html lang="en">` → `<html lang="{{.Lang}}">` in all 6 templates.

### 2. i18n.go catalog
Create `i18n.go` with the `messages` map and helpers. Seed keys (extend as you translate):

| key | en | ru |
|---|---|---|
| brand | Wallchart '26 | Wallchart '26 |
| nav.leaderboard | Leaderboard | Таблица |
| nav.mychart | My chart | Мой бланк |
| nav.login | Log in | Войти |
| nav.logout | Logout | Выйти |
| hero.eyebrow | FIFA World Cup 2026 · 104 matches · one winner | Чемпионат мира 2026 · 104 матча · один победитель |
| hero.lede | The office sheet of paper for score predictions, online. | Офисный бланк прогнозов, только онлайн. |
| hero.open | Open my chart | Открыть мой бланк |
| auth.email | Email | Эл. почта |
| auth.name | Display name | Имя |
| auth.sendcode | Email code | Прислать код |
| auth.verify | Verify | Подтвердить |
| auth.code | 6-digit code | Код из 6 цифр |
| login.title | Log in | Вход |
| verify.title | Enter code | Введите код |
| leaderboard.players | players | игроков (plural: игрок/игрока/игроков) |
| table.rank | # | # |
| table.name | Name | Имя |
| table.points | Points | Очки |
| table.exact | Exact | Точных |
| table.predictions | Predictions | Прогнозов |
| table.empty | No players yet. | Пока нет игроков. |
| me.title | My wallchart | Мой бланк |
| me.autosave | Autosaves on blur | Сохраняется автоматически |
| me.saving | Saving... | Сохранение… |
| me.saved | Saved | Сохранено |
| user.unlock | Predictions unlock after kickoff | Прогнозы открываются после стартового свистка |
| match.vs | vs | — |
| match.hidden | Hidden | Скрыто |
| match.result | Result | Результат |
| match.locked | Locked | Закрыто |
| match.open | Open | Открыто |
| match.hometeam | Home team | Хозяева |
| match.awayteam | Away team | Гости |
| admin.title | Results | Результаты |
| admin.manual | Manual entry | Ручной ввод |
| admin.save | Save | Сохранить |
| intro.title | Before kickoff | Перед стартом |
| intro.p1 | (office-ritual paragraph — translate) | Каждый крупный турнир в офисе моего отца был один и тот же ритуал: кто-то распечатывал полное расписание матчей с пустыми клетками у каждой игры, и все вписывали свои счёты ручкой. |
| intro.p2 | (sheet-of-paper paragraph — translate) | Это тот самый лист бумаги, только онлайн. Без рекламы, без денег, без паролей. Войдите по почте, заполните клетки, и пусть победит лучший прогнозист. |
| intro.start | Start guessing | Начать угадывать |

(Adjust wording to taste; this table is the checklist of every hardcoded English string across the
6 templates — grep each template against it so nothing is missed.)

### 3. Stage labels via catalog
Replace `stageLabel` (`main.go:1466`) so it's locale-aware (pass lang). Map:

| Stage (DB value) | en | ru |
|---|---|---|
| Group + name | Group A | Группа A |
| Round of 32 | Round of 32 | 1/16 финала |
| Round of 16 | Round of 16 | 1/8 финала |
| Quarter-final | Quarter-final | 1/4 финала |
| Semi-final | Semi-final | 1/2 финала |
| Third place | Third place | Матч за 3-е место |
| Final | Final | Финал |

`stageLabel` is called as `{{stageLabel .}}` today — change templates to `{{stageLabel . $.Lang}}`.

### 4. Team localization
Implement the canonical-key design above. RU display map for the 48 teams (put in
`fixtures.json` `team_names`):

Mexico→Мексика, South Africa→ЮАР, South Korea→Ю. Корея, Czechia→Чехия, Canada→Канада,
Bosnia and Herzegovina→Босния и Герцеговина, Qatar→Катар, Switzerland→Швейцария, Brazil→Бразилия,
Morocco→Марокко, Haiti→Гаити, Scotland→Шотландия, United States→США, Paraguay→Парагвай,
Australia→Австралия, Turkey→Турция, Germany→Германия, Curacao→Кюрасао, Ivory Coast→Кот-д’Ивуар,
Ecuador→Эквадор, Netherlands→Нидерланды, Japan→Япония, Sweden→Швеция, Tunisia→Тунис,
Belgium→Бельгия, Egypt→Египет, Iran→Иран, New Zealand→Новая Зеландия, Spain→Испания,
Cape Verde→Кабо-Верде, Saudi Arabia→Саудовская Аравия, Uruguay→Уругвай, France→Франция,
Senegal→Сенегал, Iraq→Ирак, Norway→Норвегия, Argentina→Аргентина, Algeria→Алжир, Austria→Австрия,
Jordan→Иордания, Portugal→Португалия, DR Congo→ДР Конго, Uzbekistan→Узбекистан, Colombia→Колумбия,
England→Англия, Croatia→Хорватия, Ghana→Гана, Panama→Панама.

- `teamName(key, lang)` funcmap func; `TBD`-prefix → localized placeholder.
- `loadTeamOptions` → return localized names for the datalist (add a `lang` param or a second func).
- Reverse map (normalized en + ru → key) used in `predict` and `adminResult` submit resolution.
- **Regression check**: existing predictions in the dev DB store knockout guesses as whatever the
  user typed (English today). Since the canonical key = English, those still match. Add a test that
  a Russian-typed guess and an English-typed guess for the same team both resolve to the same key
  and both score the +1 team bonus.

### 5. Timezone selector + client-side time
- Add `isoTime` funcmap func; update all 4 `<time>` usages (me, user, admin, + any other).
- Add the tz `<select>` + the formatting `<script>` (shared — put it in the `partials.html`
  `header` or a new `{{define "timefmt"}}` block included on pages that show matches).
- Cookie `tz` default `Europe/Moscow`; "auto" = browser tz.

### 6. Localize the login-code email
`sendLoginCode` (`main.go:1166`) builds an English subject/body. Localize by the request's `lang`
cookie at send time (pass the locale down from `authRequest`). Keep it plain text; two short
templates in `i18n.go`.

### 7. Tests + build
- Table test for `plural` (ru 1/2/5/11/21/22/25, en 1/2).
- Test `teamName` (known key both locales, unknown key, TBD placeholder).
- Test the reverse-resolution: ru and en input → same canonical key (ties into score +1 bonus).
- `make test` and `make build` green.

---

## Out of scope
- Translating user-entered display names (free text — leave as typed).
- Per-user persisted locale/tz in the DB (cookie is enough; revisit only if users complain across
  devices).
- Right-to-left / additional languages.
