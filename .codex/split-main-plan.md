# Wallchart '26 — split main.go for Codex

`main.go` is ~1560 lines. Split it into cohesive files. **This is a pure mechanical move — zero
behavior change.** Everything is already `package main`, so no import cycles are possible and no
call sites change; only the file each declaration lives in changes, plus per-file import blocks.

## Ground rules
- **No logic edits.** Move declarations verbatim. Do not rename, reorder fields, or "improve"
  anything. If you're tempted to refactor, stop — that's a separate task.
- Keep `package main` in every file. Each new file gets its **own import block**; run
  `goimports -w .` (or `gofmt` + fix imports by hand) so each file imports exactly what it uses.
- `//go:embed all:templates fixtures.json` and `var embedded embed.FS` **stay in main.go** — other
  files reference `embedded` freely (same package).
- Routing (`mux.HandleFunc(...)` in `run()`) **stays in main.go**.
- Type-placement rule: a type moves to a file **only if used solely in that file**; types shared
  across files stay in main.go (see table). When unsure, leave it in main.go.
- Do **not** touch `score.go`, `i18n.go` logic, or the `*_test.go` files. Tests reference functions
  by name within the package, so moving a function between files does not break them.

## Target layout (3 new files + slimmed main.go + helpers into i18n.go)

### main.go — bootstrap + shared vocabulary (~280 lines)
Keep:
- `embed` var + build tag, `const (...)` block (line 38), `app` struct (57).
- Shared domain types used across files: `user` (86), `matchRow` (93), `leaderboardRow` (109),
  `pageData` (117).
- `main` (152), `run` (158) incl. the mux/route wiring, `loadDotEnv` (1445), `getenv` (1489).

### handlers.go — HTTP layer (~450 lines)
All `(a *app)` request handlers + response plumbing:
- `home` (547), `setLang` (561), `login` (579), `authRequest` (590), `authVerify` (641),
  `logout` (687), `me` (695), `predict` (715), `admin` (799), `adminResult` (823),
  `userPage` (891)
- `currentUser` (928), `render` (1322), `serverError` (1335), `setCookie` (1306),
  `clearCookie` (1318)

### auth.go — login codes, rate limiting, email, crypto (~400 lines)
- Types `smtpConfig` (68), `loginRequestLimiter` (76) — used only by auth code.
- `canIssueLoginCode` (1048), `newLoginRequestLimiter` (1062), `(*loginRequestLimiter).Allow` (1072),
  `pruneWindow` (1099), `clientIP` (1110), `parseIPHeader` (1131), `verifyLoginCode` (1139),
  `findOrCreateUser` (1161), `isAdmin` (1202)
- `sendLoginCodeAsync` (1210), `sendLoginCode` (1220), `sendMail` (1244)
- `randomToken` (1340), `randomDigits` (1422), `hashLoginCode` (1434), `constantTimeEqual` (1439)
- `normalizeEmail` (1388), `parseEmailSet` (1400), `loadSMTPConfig` (1411)

### store.go — schema, fixtures, queries (~430 lines)
- Fixture types `fixtureFile` (132), `fixtureGroup` (137), `fixtureMatch` (143) — used only here +
  i18n.go reads `fixtureFile.TeamNames` (still fine, same package).
- `normalizeDBPath` (267), `migrate` (274), `ensureColumn` (343), `ensurePredictionsNullable` (370),
  `seedFixtures` (422), `buildFixtures` (467), `homePlaceholder` (533), `awayPlaceholder` (540)
- Read queries: `userByID` (946), `matchesForUser` (952), `leaderboard` (983)

### i18n.go — gains the template-func / view helpers
Move these (they're registered in the funcmap and are presentation concerns, so they belong next to
`t`/`plural`/`teamName`/`stageLabel`-style code already in i18n.go):
- `scoreText` (1496), `formatKickoff` (1500), `stageLabel` (1504), `predictionText` (1530),
  `resultText` (1537), `isKnockout` (1544), `teamGuessHome` (1548), `teamGuessAway` (1555)
- Input cleaners used by handlers — your call whether these go to i18n.go or handlers.go; they are
  small and parse-y, so **handlers.go** is the more natural home: `parseScore` (1348),
  `parseOptionalScore` (1356), `cleanTeamGuess` (1365), `cleanDisplayName` (1373),
  `collapseSpace` (1485).

## Order of work
1. Create the three new files with `package main` + empty import blocks.
2. Cut/paste declarations per the table above, one file at a time.
3. After each file: `goimports -w .` then `make build`. Fix import lists until it compiles.
4. main.go should now be ~280 lines and contain only bootstrap + shared types + routing.

## Verification (must all pass, unchanged behavior)
- `make test` and `make build` green; `go vet ./...` clean.
- `git diff --stat` should show main.go shrinking by ~1280 lines and the new files gaining them,
  with **no net change in total non-blank lines** beyond added `package main`/import lines.
- Sanity: `git diff -M` (rename/move detection) — the moved blocks should show as moves, not
  rewrites. Spot-check that no function body was altered.
- Run the binary once (`COOKIE_SECURE=false go run .`) and load `/`, `/me`, `/login` to confirm it
  still boots and renders.

## Out of scope
- Any renaming, signature changes, or behavior tweaks.
- Splitting `score.go` or `i18n.go` further, or introducing sub-packages (keep it flat `main`).
