---

description: "Task list for スマートフォンのブラウザから使える libread（Web 化）"
---

# Tasks: スマートフォンのブラウザから使える libread（Web 化）

**Input**: Design documents from `/specs/002-web-mobile-access/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/http-api.md](./contracts/http-api.md), [contracts/screens.md](./contracts/screens.md), [quickstart.md](./quickstart.md)

**Tests**: Constitution 原則 II（テストファースト, NON-NEGOTIABLE）により、サーバーと画面の両方で必須。
各テストタスクは対応する実装タスクより前に書き、失敗することを確認してから実装に進む。

- サーバー: `go test`。API は `httptest`。外部サービスは 001 の `testdata/` の記録済みレスポンス、Google のトークンエンドポイントは `httptest.Server` で差し替える。時刻は注入する。
- 画面: `npm test`（Vitest ＋ Testing Library、jsdom）。`fetch` を差し替えて contracts/http-api.md の例と同じ形の応答を返す。時刻は `vi.useFakeTimers()` で固定する。見た目の細部はテストしない。
- どちらも実ネットワークに接続しない。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- パスはリポジトリルートからの相対パス（[plan.md](./plan.md) の Project Structure に従う）

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 画面のプロジェクトと、サーバー・画面をまとめた品質ゲートの準備

- [X] T001 Create the frontend project in `web/` (research W6):
  - `web/package.json` with `"private": true`, `"type": "module"` and scripts `dev` (`vite`), `build` (`tsc --noEmit && vite build`), `typecheck` (`tsc --noEmit`), `lint` (`eslint .`), `format:check` (`prettier --check .`), `format` (`prettier --write .`), `test` (`vitest run`).
  - Runtime dependencies are only `react` and `react-dom` (19). Dev dependencies are only `vite`, `@vitejs/plugin-react`, `typescript`, `@types/react`, `@types/react-dom`, `eslint`, `@eslint/js`, `typescript-eslint`, `eslint-plugin-react-hooks`, `prettier`, `vitest`, `jsdom`, `@testing-library/react`, `@testing-library/user-event`, `@testing-library/jest-dom`. Pin exact versions (no `^`) and commit `web/package-lock.json`.
  - Add `"engines": {"node": ">=22"}`.
- [X] T002 [P] Create the TypeScript and tool configs in `web/`:
  - `web/tsconfig.json`: `"strict": true`, `"noUncheckedIndexedAccess": true`, `"noImplicitOverride": true`, `"jsx": "react-jsx"`, `"moduleResolution": "bundler"`, `"resolveJsonModule": true` (for the shared contract examples in T017), `"types": ["vitest/globals", "@testing-library/jest-dom"]`.
  - `web/eslint.config.js`: `@eslint/js` recommended + `typescript-eslint` `strictTypeChecked` + `eslint-plugin-react-hooks`; `@typescript-eslint/no-explicit-any: "error"` (Constitution 原則 III). Ignore `dist/`.
  - `web/.prettierrc.json` (`{}` is fine) and `web/.prettierignore` (`dist/`, `node_modules/`, `package-lock.json`).
  - `web/vite.config.ts`: React plugin; `server.proxy` sends `/api` and `/auth` to `http://localhost:8080`; `test: { environment: "jsdom", globals: true, setupFiles: ["src/test/setup.ts"] }`.
  - `web/src/test/setup.ts`: import `@testing-library/jest-dom/vitest`; after each test, restore `fetch`, clear `sessionStorage`, and call `vi.useRealTimers()`.
- [X] T003 [P] Create `web/index.html` (lang `ja`, `<meta name="viewport" content="width=device-width, initial-scale=1">`, title `libread`) and `web/src/main.tsx` that renders a placeholder `<App />` from `web/src/App.tsx` into `#root`. Confirm `npm run build`, `npm run typecheck`, `npm run lint`, `npm run format:check` and `npm test` (with one trivial passing test in `web/src/App.test.tsx`) all succeed.
- [X] T004 [P] Add `web/node_modules/` and `web/dist/` to `.gitignore`.
- [X] T005 Extend the `check` target in `Makefile` (quickstart.md step 1): keep the existing Go gates (`gofmt -l .` must be empty, `go vet ./...`, `go tool staticcheck ./...`, `go test ./...`), then run `cd web && npm ci && npm run format:check && npm run typecheck && npm run lint && npm test`. Make `gofmt` ignore `web/node_modules` (use `gofmt -l $$(git ls-files '*.go')`). After the web gates, run `cd web && npm run build` and fail if `web/dist` contains any of the strings `LIBREAD_`, `client_secret`, `appkey` (`! grep -rE 'LIBREAD_|client_secret|appkey' web/dist`) (Constitution 技術的制約「秘密情報」, FR-113). This checks names only; secret values exist only in the local environment and are intentionally not matched.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 001 のコードの共有化、本人確認、HTTP サーバーの骨組み、画面の骨組み。すべてのストーリーがこれに依存する。

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Tests (write first, confirm they fail)

- [X] T006 [P] Write tests in `internal/store/data_test.go` for a new `ParseData(raw []byte) (Data, error)` (research W8):
  - It accepts the same content `Load` accepts (reuse the existing fixtures, e.g. `internal/cli/testdata/check_scenario1.json`) and returns the same `Data`.
  - Invalid JSON, `version` other than 1, and any invalid field (bad ISBN, `open` ≥ `close`, closure `start` > `end`) are errors.
  - `Load` keeps its current behaviour (missing file → empty `Data`).
- [X] T007 [P] Write tests in `internal/store/auth_test.go` for `LoadAuthState(dir) (AuthState, error)` and `SaveAuthState(dir, AuthState) error` on `dir/auth.json` (data-model.md AuthState):
  - Missing file → `Generation` 1.
  - Round trip of `{"version": 1, "generation": N}`.
  - `generation` < 1 or unknown `version` is an error.
- [X] T008 [P] Write tests in `internal/present/present_test.go` for the label functions moved from `internal/cli/format.go` (research W5): `OpeningLabel(domain.Opening) string`, `AvailabilityLabel(domain.BookHolding) string`, `HoldingsSourceLabel(domain.Result) string`, `OpeningInfoSourceLabel([]domain.RegisteredLibrary) string`, and the constants for the closure note (`※ 臨時休館は入力済みの情報のみ反映しています`), the NDL credit and the Calil credit. Expected strings are exactly those in `internal/cli/testdata/check_*.golden` and specs/001-readable-libraries-today/contracts/cli.md (e.g. `開館中 〜19:00`, `開館前 15:00〜20:00`, `開館中 〜14:30（閉館まで30分未満）`, `在架（貸出可）`, `不明（古い情報: HH:MM 取得）`).
- [X] T009 [P] Write tests in `internal/checkrun/checkrun_test.go` for `Run(ctx, Deps, data store.Data, books []domain.ReadingBook, now time.Time) (domain.Result, error)` moved from `internal/cli/check.go` (`loadHoldings`, the holdings cache, the holiday lookup and the per-library loop). Port the holdings-cache cases from `internal/cli/check_test.go`: all fresh → no Calil request; stale or missing → one Calil session and the cache is saved; Calil failure with an old cache → old snapshots are used and marked stale; Calil failure with no cache → error. Warnings go to a `Deps.Warn func(string)` instead of stderr.
- [X] T010 [P] Write tests in `internal/auth/session_test.go` for the session cookie (data-model.md Session, research W2):
  - `Issue(key, generation, now)` → value `base64url(JSON) + "." + base64url(HMAC-SHA256(key, JSON))`; `ExpiresAt` = `IssuedAt` + 30日.
  - `Verify(key, value, currentGeneration, now)` accepts a fresh cookie and rejects: a changed payload, a changed signature, a different key, `now` ≥ `ExpiresAt`, `Generation` ≠ current generation, malformed values (no `.`, bad base64, bad JSON).
  - `SessionGeneration` is a named type; values < 1 are rejected.
- [X] T011 [P] Write tests in `internal/auth/oidc_test.go` for the Google login with a fake token endpoint (`httptest.Server`) (research W2):
  - `LoginURL(state)` contains `response_type=code`, `client_id`, `redirect_uri=<origin>/auth/callback`, `scope=openid email`, `state`, `prompt=select_account`.
  - `Exchange(ctx, code)` POSTs `code`, `client_id`, `client_secret`, `redirect_uri`, `grant_type=authorization_code` and returns the owner check result.
  - Accepted only when `iss` is `https://accounts.google.com` or `accounts.google.com`, `aud` equals the client ID, `exp` is in the future, `email_verified` is `true`, and `email` equals `LIBREAD_OWNER_EMAIL` ignoring case. Each violation is rejected. A token-endpoint error status is rejected.
  - `ReturnPath` accepts only `/`, `/books`, `/libraries`; anything else becomes `/`.
- [X] T012 [P] Write tests in `internal/server/middleware_test.go` (contracts/http-api.md「共通」):
  - Without a valid `libread_session`, every `/api/...` path and method (including unknown paths such as `/api/nonexistent` and `/api/books/9784003101018`) returns exactly the same `401` status and body `{"error":{"code":"unauthenticated","message":"ログインしてください"}}`, whether or not user data exists (FR-110, SC-104).
  - With a valid session, `POST`/`PUT`/`PATCH`/`DELETE` whose `Origin` differs from `LIBREAD_PUBLIC_ORIGIN` (or is missing) returns `403` `forbidden`; `GET` does not need `Origin`.
  - Every response has `Strict-Transport-Security: max-age=31536000`; `/api/` and `/auth/` responses have `Cache-Control: no-store`.
  - The request log line contains time, method, path (without the query string), status and duration, and never contains the query string, cookies, notes or the owner email (research W11).
- [X] T013 [P] Write tests in `internal/server/auth_handlers_test.go` (contracts/http-api.md「本人確認」):
  - `GET /auth/login?return=/books` sets `libread_login` (`HttpOnly`, `Secure`, `SameSite=Lax`, 10分) and redirects to Google with the same `state`.
  - `GET /auth/callback` with matching `state` and an owner token sets `libread_session` (`HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, 30日), clears `libread_login`, and redirects to `/#/books`.
  - Non-owner token, `state` mismatch, missing `libread_login` and `error=` from Google all redirect to `/#/login?error=denied` without `libread_session`, and clear `libread_login`.
  - `GET /api/session` returns `{"expiresAt": ...}` and no email.
  - `POST /api/session/logout` clears the cookie (`204`).
  - `POST /api/session/revoke-all` increments the generation in `auth.json`, clears the cookie (`204`), and afterwards a cookie issued before it gets `401` (FR-111).
  - Each success response, and the `401` body, passes `assertMatchesExample` with the matching example file (T017).
- [X] T014 [P] Write tests in `internal/server/static_test.go`: `/` returns `index.html` with `Cache-Control: no-cache` without a session; `/assets/x.js` returns the file with `Cache-Control: public, max-age=31536000, immutable`; a missing asset is `404`. Use `fstest.MapFS`.
- [X] T015 [P] Write tests in `web/src/api/http.test.ts` for the fetch wrapper:
  - Requests use `credentials: "same-origin"`, and send `Content-Type: application/json` with a JSON body for writes.
  - A `401` response throws `UnauthenticatedError`.
  - Other error statuses throw `ApiError` with `error.message` from the body, or 「通信に失敗しました」 when the body is not the contract shape.
  - A network failure throws `ApiError` with 「通信に失敗しました」.
  - `decodeSession(u: unknown)` accepts `{"expiresAt": "<RFC 3339>"}` and throws on anything else.
  - `decodeSession` accepts the `session.json` example from `web/src/test/examples.ts` (T017).
- [X] T016 [P] Write tests in `web/src/App.test.tsx` (contracts/screens.md「共通」「ログイン画面」):
  - When `GET /api/session` returns `401`, the login screen is shown with a 「Google でログイン」 link to `/auth/login?return=/` and no user data.
  - `#/login?error=denied` shows 「ログインできませんでした。libread に登録した Google アカウントでログインしてください」.
  - With a session, the bottom tabs 今日・本・図書館 switch the hash between `#/`, `#/books` and `#/libraries`.
  - When any later API call throws `UnauthenticatedError`, the login screen is shown.

- [X] T017 [P] Create the shared contract examples in `specs/002-web-mobile-access/contracts/examples/` (contracts/http-api.md「共通」, Constitution 原則 III): `session.json`, `check.json` (the example in contracts/http-api.md), `check-setup-libraries.json`, `check-setup-books.json`, `books.json`, `book-search.json`, `libraries.json`, `library-search.json`, `import.json`, `error-unauthenticated.json`.
  - Add a Go helper `assertMatchesExample(t, name, body)` in `internal/server/contract_test.go` that checks the response has exactly the same set of keys at every level as the example file (values may differ).
  - Add a web helper `web/src/test/examples.ts` that loads the same files with `import ... from "../../../specs/002-web-mobile-access/contracts/examples/<name>.json"`.

### Implementation

- [X] T018 Implement `ParseData` in `internal/store/data.go` by extracting the parsing part of `Load`; make `Load` call it (T006).
- [X] T019 [P] Implement `AuthState`, `LoadAuthState` and `SaveAuthState` in `internal/store/auth.go` using `writeFileAtomic` (T007). `SessionGeneration` lives in `internal/auth`; store keeps a plain validated `int` field to avoid an import cycle, and `internal/auth` converts it.
- [X] T020 [P] Create `internal/present/present.go` by moving the label-building code out of `internal/cli/format.go` (T008), and change `internal/cli/format.go` to call it. The existing CLI golden tests in `internal/cli` must still pass unchanged.
- [X] T021 [P] Create `internal/checkrun/checkrun.go` by moving `loadHoldings`, `readHoldingsCache`, `saveHoldingsCache`, `systemIDsOf`, `holdingKey` and the per-library loop out of `internal/cli/check.go` (T009). `Deps` holds the Calil client, the holiday client, `store.Cache`, and `Warn`. Change `internal/cli/check.go` to call `checkrun.Run`; the existing CLI tests must still pass unchanged.
- [X] T022 [P] Implement `internal/auth/session.go` (T010): `SessionGeneration`, `Session`, `Issue`, `Verify`. Compare signatures with `hmac.Equal`. Use only the standard library.
- [X] T023 [P] Implement `internal/auth/oidc.go` (T011): `Config{ClientID, ClientSecret, PublicOrigin, OwnerEmail, TokenURL, AuthURL, HTTP}`, `LoginURL`, `Exchange`, `ReturnPath`, and `NewState()` (32 bytes from `crypto/rand`, base64url). Decode the ID token payload without signature verification, because it comes directly from the token endpoint over TLS (OpenID Connect Core 1.0 §3.1.3.7; research W2). Use only the standard library.
- [X] T024 Implement `internal/server/server.go` (T012–T014):
  - `Config` (data dir, cache dir, static `fs.FS`, auth config, session key, Calil app key, `Now func() time.Time`, `HTTP *http.Client`, log `io.Writer`) and `New(Config) http.Handler`.
  - Routing with `http.ServeMux` method patterns (Go 1.22+). Order: security headers → request log → for `/api/`: session check (before routing, so unknown paths also get `401`) → `Origin` check for writes → handler.
  - A `sync.Mutex` that serializes every read-modify-write of `data.json` (research W3): each write handler loads the latest data, applies one change, and saves, all under the lock.
  - One `sync.Mutex` per external service (Calil, NDL, holidays-jp), held only while that service is being called (research W9). The Calil mutex is shared by `/api/check`, `/api/library-search` and `POST /api/libraries`; the NDL mutex by `/api/book-search` and `POST /api/books`.
  - JSON helpers that write the error shape in contracts/http-api.md, and never include the Calil app key in messages.
  - Static files from the `fs.FS` with the cache headers in T014.
- [X] T025 Implement `internal/server/auth_handlers.go`: `/auth/login`, `/auth/callback`, `GET /api/session`, `POST /api/session/logout`, `POST /api/session/revoke-all` (T013). Revoke-all runs under the same mutex as data writes.
- [X] T026 Implement `cmd/libread-server/main.go`: read `PORT` (default `8080`), `LIBREAD_DATA_DIR`, `LIBREAD_CACHE_DIR`, `LIBREAD_STATIC_DIR`, `LIBREAD_PUBLIC_ORIGIN`, `LIBREAD_OWNER_EMAIL`, `LIBREAD_GOOGLE_CLIENT_ID`, `LIBREAD_GOOGLE_CLIENT_SECRET`, `LIBREAD_SESSION_KEY` (base64, at least 32 bytes) and `LIBREAD_CALIL_APPKEY`. Exit with a clear message if any required value except `LIBREAD_CALIL_APPKEY` is missing (a missing Calil key gives `503 notConfigured` on the endpoints that need it). Import `time/tzdata`. Set `ReadHeaderTimeout` on the `http.Server` and shut down gracefully on SIGTERM (Cloud Run).
- [X] T027 [P] Implement `web/src/api/http.ts` (T015): `getJSON`, `sendJSON`, `UnauthenticatedError`, `ApiError`, `decodeSession`, and small decoding helpers (`asObject`, `asString`, `asNumber`, `asBoolean`, `asArray`, `asOptional`) that take `unknown` and throw on mismatch. No `any`.
- [X] T028 Implement `web/src/App.tsx`, `web/src/screens/Login.tsx`, `web/src/components/ErrorRetry.tsx` and `web/src/styles.css` (T016): session check on start, hash routing for `#/`, `#/books`, `#/libraries`, `#/login`, fixed bottom tabs, a shared handler that switches to the login screen on `UnauthenticatedError`. The three screens are placeholders for now. `styles.css` uses a single column, `max-width: 100%`, `overflow-wrap: anywhere` and no fixed widths over 360px (FR-115). The web code must not read build-time environment variables (`import.meta.env` is not used except `import.meta.env.DEV`); add an ESLint `no-restricted-syntax` rule in `web/eslint.config.js` that rejects any other `import.meta.env` member (FR-113).
- [X] T029 Create `Dockerfile` (quickstart.md step 4): stage 1 `node:22` runs `npm ci && npm run build` in `web/`; stage 2 `golang:1.25` builds `./cmd/libread-server` with `CGO_ENABLED=0`; final stage `gcr.io/distroless/static-debian12:nonroot` copies the binary to `/app/libread-server` and `web/dist` to `/app/static`, and sets `ENTRYPOINT`. Add `.dockerignore` (`web/node_modules`, `web/dist`, `bin/`, `.git`, `.env*`).

**Checkpoint**: `make check` passes. Login works locally against Google (quickstart.md step 3-1), and the three tabs show placeholders.

---

## Phase 3: User Story 1 - スマートフォンで今日読める図書館を確認する (Priority: P1) 🎯 MVP

**Goal**: スマートフォンのブラウザで、001 と同じ基準・同じ表示内容で「今日読める図書館」を確認できる。001 のデータを取り込んで使い始められる。

**Independent Test**: 001 の `data.json` を取り込み、スマートフォン幅の画面で今日の画面を開いて、001 の CLI と同じ館・同じ順・同じ理由・同じ出典が表示されることを確認する（quickstart.md 手順 2・3-2・3-5）。

### Tests for User Story 1 (write first, confirm they fail)

- [X] T030 [P] [US1] Write tests in `internal/server/check_test.go` for `GET /api/check` (contracts/http-api.md「検索」):
  - Response shape and field names exactly as in the contract example, `checkedAt` in `+09:00`.
  - `opening.open`/`close` only for `open`・`notYetOpen`・`finishedToday`; `closingSoon` only for `open`; `notReadable` libraries that are not `open`/`notYetOpen` have empty `books`.
  - A `stale: true` book always has `readable: false`; `fetchedAt` is omitted when nothing was fetched.
  - `{"setupRequired": "libraries"}` with no libraries (also when there are no books either), `{"setupRequired": "books"}` with libraries but no books.
  - `?isbn=` limits the result to that book; an unregistered ISBN is `404`; an invalid ISBN is `400`.
  - Missing Calil key → `503` `notConfigured`; Calil unreachable with no cache → `502` `upstream`.
  - Two concurrent requests send only one Calil session (the second waits for the first and uses the 30分 cache; research W9).
  - A concurrent `/api/check` and `/api/library-search` never have two Calil requests in flight at once (research W9).
  - With `time.Local` set to UTC in the test (`t.Cleanup` restores it), at injected time `2026-10-21T00:05:00+09:00` (= `2026-10-20T15:05Z`), the result's `checkedAt` is `2026-10-21T00:05:00+09:00` and a library whose schedule is open on 水曜 only and closed on 火曜 is judged by 水曜 (FR-103).
  - The normal and `setupRequired` responses pass `assertMatchesExample` with `check.json`, `check-setup-libraries.json` and `check-setup-books.json` (T017).
- [X] T031 [P] [US1] Write `TestCheckMatchesCLIScenarios` in `internal/server/check_scenarios_test.go` (SC-103, quickstart.md step 2). Copy these fixtures from `internal/cli/testdata/` to `internal/server/testdata/`, so the test survives the CLI's removal, and port each case from `internal/cli/check_test.go` with the same injected time and the same fake Calil/holidays/cache setup:

  | CLI test | data | golden |
  |---|---|---|
  | `TestCheckContractExample` | `check_example.json` | `check_example.golden` |
  | `TestCheckScenario1SingleBook` | `check_scenario1.json` | `check_scenario1.golden` |
  | `TestCheckScenario5NoneReadable` | `check_scenario1.json` | `check_none.golden` |
  | `TestCheckEdgeCases` | `check_edge.json` | `check_edge.golden` |
  | `TestCheckStaleCacheAndFailedRefetch` | `check_single.json` | `check_stale.golden` |
  | `TestCheckFallsBackToCachePerSystem` | `check_single.json` | `check_mixed_times.golden` |

  For each case, assert that the library names and order in `readable` and `notReadable`, every `opening.label`, every book `label`, and the `sources` labels equal the corresponding lines of the golden file. Parse the golden file (lines `N. <館名>  <label>`, `- <館名>  <label>`, `   - 『<書名>』 <label>`) instead of re-running the CLI.
- [X] T032 [P] [US1] Write tests in `internal/server/import_test.go` for `POST /api/import` (FR-118, research W8):
  - With no books and no libraries, a valid 001 `data.json` is saved and the response is `{"books": N, "libraries": M, "closures": K}`; loading the saved `data.json` with `store.Load` gives the same `Data` as `store.ParseData` of the input (SC-108).
  - With any existing book or library → `409`, nothing changed.
  - Invalid content → `400`, nothing saved. Bodies over 1 MiB → `400`.
- [X] T033 [P] [US1] Write tests in `web/src/api/check.test.ts` for `decodeCheckView(u: unknown)`: accepts the `check.json`, `check-setup-libraries.json` and `check-setup-books.json` examples from `web/src/test/examples.ts` (T017); unknown `opening.status` or `availability` values become `"unknown"` (Constitution 原則 III); missing required fields throw.
- [X] T034 [P] [US1] Write tests in `web/src/screens/Today.test.tsx` (contracts/screens.md「今日」):
  - While `GET /api/check` is pending, only 「所蔵状況を取得しています…」 is shown — no partial result (FR-104).
  - On success: the checked-at time (e.g. 「10月21日(水) 14:05 時点」), 「今日読める図書館」 with libraries numbered in API order, labels as given, 「今日は読めない図書館」, sources and Calil links. Empty `readable` shows 「今日読める図書館はありません」. The screen never reorders libraries.
  - 「更新」 hides the old result, shows the loading text, and then shows the new checked-at time (FR-105).
  - With fake timers, 31 minutes after `checkedAt` the screen shows 「古い情報」 and 「更新してください」 without a new request; at 30 minutes it does not.
  - On failure: the error message and 「再試行」; a previous result stays visible with its checked-at time (FR-116).
  - `setupRequired` shows the right message and a link to `#/libraries` or `#/books`.
  - With 2+ books, choosing a book requests `/api/check?isbn=<ISBN>` (US1 scenario 5).
- [X] T035 [P] [US1] Write tests in `web/src/screens/Import.test.tsx`: the import section (file input) is shown only when the books and libraries lists are both empty; choosing a file POSTs its text to `/api/import` and shows the imported counts; a `400`/`409` shows the error message.

### Implementation for User Story 1

- [X] T036 [US1] Implement `GET /api/check` in `internal/server/check.go` using `checkrun.Run` and `present` (T030, T031). Use the Calil mutex from T024 while calling Calil. Convert `domain.Result` to the contract JSON with typed response structs (no `any`).
- [X] T037 [US1] Implement `POST /api/import` in `internal/server/import.go` using `store.ParseData` under the data mutex (T032).
- [X] T038 [P] [US1] Implement `web/src/api/check.ts` (`CheckView` types with string-literal unions and an `ISBN` brand type, `decodeCheckView`, `fetchCheck(isbn?)`) (T033).
- [X] T039 [US1] Implement `web/src/screens/Today.tsx` (T034). Re-evaluate staleness once a minute with `setInterval`, clearing it on unmount. Fetch on mount and when the screen becomes active again, not on tab visibility changes.
- [X] T040 [US1] Implement `web/src/screens/Import.tsx` and show it from the placeholder books screen `web/src/screens/Books.tsx` (T035). Read the file with `File.text()`.

**Checkpoint**: 001 のデータを取り込むと、今日の画面に 001 の CLI と同じ結果が表示される（MVP）。

---

## Phase 4: User Story 2 - スマートフォンで読みかけの本とメモを管理する (Priority: P2)

**Goal**: スマートフォンから本を探して登録し、メモを書き換え、読み終えた本を外せる。

**Independent Test**: スマートフォン幅の画面で本を探して登録し、メモを編集・消去し、本を外す。そのたびに一覧と今日の画面の検索対象が変わることを確認する（quickstart.md 手順 3-4）。

### Tests for User Story 2 (write first, confirm they fail)

- [X] T041 [P] [US2] Write tests in `internal/server/books_test.go` (contracts/http-api.md「読みかけの本」):
  - `GET /api/books` in registration order with `isbn`, `title`, `author`, `publisher`, `note`, `addedOn`.
  - `GET /api/book-search` uses the recorded NDL responses in `internal/ndl/testdata/`; `q` is required (`400`); `registered` is true for registered ISBNs; `hint` appears only under the 001 conditions; `credit` has the NDL label, URL and CC BY 4.0 license link.
  - `POST /api/books` checks the ISBN with NDL, returns `201` and the book, `addedOn` is today (Asia/Tokyo); an already registered ISBN is `409` without an NDL request; an invalid ISBN is `400`; NDL unreachable is `502`.
  - `PATCH /api/books/{isbn}` replaces the note, `""` clears it; unregistered is `404`.
  - `DELETE /api/books/{isbn}` → `204`; unregistered is `404`.
  - Two concurrent `PATCH` requests on different books both persist (spec Edge Cases, research W3).
  - The note never appears in any outgoing request (FR-114).
  - Success responses pass `assertMatchesExample` with `books.json` and `book-search.json` (T017).
- [X] T042 [P] [US2] Write tests in `web/src/api/books.test.ts` for `decodeBooks`, `decodeBookSearch` and the request helpers (`addBook`, `updateNote`, `removeBook`, `searchBooks`). The decoders accept the `books.json` and `book-search.json` examples from `web/src/test/examples.ts` (T017).
- [X] T043 [P] [US2] Write tests in `web/src/screens/Books.test.tsx` (contracts/screens.md「本」):
  - The list shows title, author, publisher, note and added date.
  - 「メモを編集」 → input, 「保存」 sends `PATCH`, is disabled while saving, and the list updates; 「取消」 restores; an empty note clears it.
  - When `PATCH` throws `UnauthenticatedError`, the draft is saved to `sessionStorage`, the login screen is shown, and after re-login on `#/books` the draft is back in the input (research W12) and removed from `sessionStorage`.
  - 「外す」 calls `window.confirm` with 「『<書名>』を外しますか？」; cancel sends nothing, OK sends `DELETE` (FR-108).
  - Search: 「書名または ISBN」 is required; results show title/author/publisher/ISBN and 「登録」 or a disabled 「登録済み」; `hint` and the NDL credit are shown; zero results show 「見つかりませんでした」 with the given conditions; 「登録」 asks for an optional note, then sends `POST`.

### Implementation for User Story 2

- [X] T044 [US2] Implement the book endpoints in `internal/server/books.go` using `internal/ndl` and the data mutex (T041). Reuse the NDL candidate ranking and `hint` conditions from `internal/ndl` and `internal/cli/book.go` (move shared text into `internal/present` if both need it). Use the NDL mutex from T024 (research W9).
- [X] T045 [P] [US2] Implement `web/src/api/books.ts` (T042).
- [X] T046 [US2] Implement `web/src/screens/Books.tsx` (list, note editing, removal, search and registration), keeping the import section from T040 (T043).

**Checkpoint**: 本の登録・メモの更新・削除がスマートフォンからでき、今日の画面の対象が変わる。

---

## Phase 5: User Story 3 - スマートフォンで利用図書館と開館情報を管理する (Priority: P3)

**Goal**: スマートフォンから利用図書館の追加・削除、開館時間と祝日の扱い、臨時休館期間の入力・削除ができる。全端末からのログアウトもここから行う。

**Independent Test**: 図書館を探して追加し、開館スケジュールと今日を含む臨時休館期間を入力・削除して、今日の画面の判定が変わることを確認する（quickstart.md 手順 3-3・3-6）。

### Tests for User Story 3 (write first, confirm they fail)

- [X] T047 [P] [US3] Write tests in `internal/server/libraries_test.go` (contracts/http-api.md「利用図書館」):
  - `GET /api/libraries`: registration order; `schedule` is `null` when not entered; `weekly` has all 7 keys with `null` for closed days; `holiday.mode` is `same`/`closed`/`hours`; `closures` sorted by start date.
  - `GET /api/library-search` uses `internal/calil/testdata/library_setagaya.json`; `pref` is required (`400`); `name` filters by substring; `registered` flags; `credit` is the Calil link.
  - `POST /api/libraries` checks existence with `library_system_setagaya.json` and returns `201`; unknown `SystemID:LibKey` is `404`; already registered is `409` without a Calil request; missing Calil key is `503`.
  - `DELETE /api/libraries/{id}` with a URL-encoded `SystemID:LibKey` → `204`; unknown is `404`.
  - `PUT /api/libraries/{id}/schedule` requires all 7 weekday keys, each `null` or `{"open","close"}` in `HH:MM` with 開館 < 閉館; `holiday` mode `hours` needs valid `open`/`close`; violations are `400`; `updatedOn` becomes today; returns the updated library.
  - `POST /api/libraries/{id}/closures`: `start` > `end` is `400`; same start/end/reason is `409`; overlapping periods are allowed; returns the library with closures re-sorted.
  - `DELETE /api/libraries/{id}/closures/{index}`: out-of-range or non-numeric index is `404`; returns the updated library.
  - After `PUT` schedule and `POST` closure including today, `GET /api/check` reports that library as `closedPeriod`; after deleting it, the weekly schedule applies (US3 scenarios 3–4).
  - Success responses pass `assertMatchesExample` with `libraries.json` and `library-search.json` (T017).
- [X] T048 [P] [US3] Write tests in `web/src/api/libraries.test.ts` for `decodeLibraries`, `decodeLibrarySearch` and the request helpers. The decoders accept the `libraries.json` and `library-search.json` examples from `web/src/test/examples.ts` (T017).
- [X] T049 [P] [US3] Write tests in `web/src/screens/Libraries.test.tsx` (contracts/screens.md「図書館」「設定」):
  - The list shows name, city, the schedule summary per weekday, holiday rule and last updated date, or 「開館情報未入力」, and the closure periods.
  - Schedule editor: first-time editing starts with every weekday 休館 and holiday 曜日どおり; toggling a day and setting times then 「保存」 sends `PUT` with all 7 keys.
  - Closure form: `start` after `end` shows 「開始日は終了日以前にしてください」 and sends nothing; valid input sends `POST`.
  - 「外す」 and closure 「削除」 call `window.confirm` first (FR-108).
  - Search: prefecture select with all 47 prefectures is required; results show name/city/address and 「登録」 or disabled 「登録済み」; the Calil credit link is shown.
  - 「ログアウト」 sends `POST /api/session/logout` and shows the login screen; 「すべての端末からログアウト」 confirms, sends `POST /api/session/revoke-all`, and shows the login screen (FR-111).

### Implementation for User Story 3

- [X] T050 [US3] Implement the library endpoints in `internal/server/libraries.go` using `internal/calil` and the data mutex (T047). Use the Calil mutex from T024 (research W9).
- [X] T051 [P] [US3] Implement `web/src/api/libraries.ts` (T048).
- [X] T052 [US3] Implement `web/src/screens/Libraries.tsx` with the schedule editor (`<input type="time">`), closure form (`<input type="date">`), library search, and the settings section (T049).

**Checkpoint**: 図書館・開館時間・臨時休館をスマートフォンから管理でき、今日の画面の判定に反映される。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: ストーリーをまたぐ確認、デプロイ、CLI の廃止

- [X] T053 [P] Add `internal/server/outbound_test.go`, porting `internal/cli/outbound_test.go` to the API (research W9, Constitution 技術的制約): record every outgoing request across all endpoints and assert no request contains a note, the per-endpoint limits from 001 research R9 hold (`/api/check` one Calil session and no Calil request within 30分 of a fresh cache; library search/add one Calil request; book search/add one NDL request), at most one request per service is in flight, and every request has `User-Agent: libread/<version>`.
- [X] T054 [P] Add `internal/server/check_perf_test.go` for SC-102, porting `internal/cli/check_perf_test.go`: 10 libraries in 10 systems and 5 books with a Calil fake that stays Running; `/api/check` returns within 10 seconds of injected time and marks them 不明（取得タイムアウト）.
- [ ] T055 Run `make check` and fix every warning (also run `go test -race ./...`).
- [ ] T056 Run quickstart.md step 3 locally against the real services and Google, and record any differences in `specs/002-web-mobile-access/quickstart.md`.
- [ ] T057 Deploy to Cloud Run following quickstart.md step 4 (bucket, secrets, IAM, Calil key restriction check, `gcloud run deploy`), then run quickstart.md step 5 on the phone. Record the results (including the cold-start time for SC-102 and the 360px check for FR-115) in `specs/002-web-mobile-access/quickstart.md`.
- [ ] T058 Import the real 001 `data.json` in production (quickstart.md step 3-2) and confirm the counts (SC-108).
- [ ] T059 Remove the 001 CLI (FR-117, quickstart.md step 6): delete `cmd/libread/` and `internal/cli/` (`internal/server/testdata/` keeps the six data/golden pairs listed in T031). Move any helper still used only by the server out of `internal/cli` first. `make check` must pass afterwards.
- [ ] T060 Rewrite `README.md` for the Web version: what it is, how to deploy (link to quickstart.md step 4), how to use it on the phone, where data lives (Cloud Storage bucket), the environment variables, development (`make check`, `npm run dev`), and the external-service credits (keep the 001 credits section). Link to `specs/002-web-mobile-access/`.
- [ ] T061 Change **Status** in `specs/002-web-mobile-access/spec.md` from `Draft` to `Implemented` with the completion date, and note in `specs/001-readable-libraries-today/spec.md` that FR-015（ターミナルからの操作）was superseded by spec 002 FR-117.
- [ ] T062 Add automatic deployment (research W13) after the manual deployment in T057 has worked:
  - Set up Workload Identity Federation for this GitHub repository and a deploy service account with only the roles needed for `gcloud run deploy --source` (Cloud Run admin on the service, Cloud Build, Artifact Registry writer, and `iam.serviceAccountUser` on the runtime service account). Do not create a service account key. Record the `gcloud` commands in `specs/002-web-mobile-access/quickstart.md` step 4.
  - Create `.github/workflows/deploy.yml`: on push to `main`, set up Go 1.25 and Node 22, run `make check`, authenticate with `google-github-actions/auth` (Workload Identity), and run the same `gcloud run deploy` command as quickstart.md step 4. Keep non-secret values (project, region, bucket, owner email, client ID, public origin) in GitHub repository variables, never in the workflow file.
  - Revisit research W3「デプロイ中の例外」: deployments now run without the user noticing. Either keep the decision and say why in research.md, or add a task for generation-checked writes.
  - Mention the automatic deployment in `README.md`.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies.
- **Foundational (Phase 2)**: Depends on Setup. Blocks all user stories.
- **US1 (Phase 3)**: Depends on Foundational.
- **US2 (Phase 4)**: Depends on Foundational. T046 extends `web/src/screens/Books.tsx` created in T040; if US2 is built before US1, create that file in T046 and add the import section later in T040.
- **US3 (Phase 5)**: Depends on Foundational. The last case in T047 uses `GET /api/check` (T036); if US3 is built before US1, skip that case until T036 is done.
- **Polish (Phase 6)**: Depends on US1–US3. T057 → T058 → T059 in this order (the CLI is removed only after the production import). T062 needs T057 (a working manual deployment).

### Within Each Phase

- Tests are written first and must fail before the matching implementation (T006–T017 → T018–T029, T030–T035 → T036–T040, T041–T043 → T044–T046, T047–T049 → T050–T052).
- Go: `store`/`present`/`checkrun`/`auth` → `server` → `cmd/libread-server`.
- Web: `api/*` → `screens/*` → `App.tsx`.
- `internal/server/server.go` (routing) is edited by T024, T036, T037, T044 and T050. Do these in order, never in parallel.
- `internal/cli/format.go` and `internal/cli/check.go` are edited by T020 and T021; the CLI tests must stay green after each.

### Parallel Opportunities

- Phase 1: T002, T003, T004.
- Phase 2: the tests T006–T017; then T019, T020, T021, T022, T023 and T027.
- US1: the tests T030–T035; then T038 alongside T036/T037.
- US2: T041–T043; then T045 alongside T044.
- US3: T047–T049; then T051 alongside T050.
- After Phase 2, the Go side and the web side of each story can be built in parallel by different people.

---

## Parallel Example: User Story 1

```bash
# Tests first (all different files):
Task: "GET /api/check tests in internal/server/check_test.go"
Task: "SC-103 scenario tests in internal/server/check_scenarios_test.go"
Task: "POST /api/import tests in internal/server/import_test.go"
Task: "decodeCheckView tests in web/src/api/check.test.ts"
Task: "Today screen tests in web/src/screens/Today.test.tsx"
Task: "Import section tests in web/src/screens/Import.test.tsx"

# Then independent implementations:
Task: "GET /api/check in internal/server/check.go"
Task: "CheckView decoding in web/src/api/check.ts"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 and Phase 2.
2. Phase 3 (US1).
3. **STOP and VALIDATE**: import the 001 `data.json` locally and confirm the today screen matches the CLI (quickstart.md steps 2, 3-2, 3-5). Optionally deploy (T057) to use it on the phone already. Until US2 and US3 are done, books and libraries cannot be changed after the import, so wait to import in production (T058) until then if they change often.

### Incremental Delivery

1. Setup + Foundational: login works.
2. US1: today's result on the phone from imported data (MVP).
3. US2: books and notes from the phone.
4. US3: libraries, hours and closures from the phone. The CLI is no longer needed.
5. Polish: deploy, production import, remove the CLI.

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Verify tests fail before implementing (Constitution II)
- Never commit secrets: the Calil key, the Google client secret and the session key come only from environment variables / Secret Manager
- Stop at any checkpoint to validate story independently
