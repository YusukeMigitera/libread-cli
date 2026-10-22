---

description: "Task list for 今日読める図書館の検索"
---

# Tasks: 今日読める図書館の検索

**Input**: Design documents from `/specs/001-readable-libraries-today/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/cli.md](./contracts/cli.md), [quickstart.md](./quickstart.md)

**Tests**: Constitution 原則 II（テストファースト, NON-NEGOTIABLE）により必須。
各テストタスクは対応する実装タスクより前に書き、`go test` で失敗することを確認してから実装に進む。
外部 API のテストは `testdata/` の記録済みレスポンスと `httptest.Server` を使い、実ネットワークに接続しない。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- パスはリポジトリルートからの相対パス（[plan.md](./plan.md) の Project Structure に従う）

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Go モジュールと品質ゲートの準備

- [X] T001 Create `go.mod` with module path `libread` and `go 1.25`. Create the empty package directories `cmd/libread/`, `internal/domain/`, `internal/calil/testdata/`, `internal/ndl/testdata/`, `internal/holiday/testdata/`, `internal/store/` and `internal/cli/testdata/`.
- [X] T002 Add staticcheck as a tool dependency with `go get -tool honnef.co/go/tools/cmd/staticcheck@latest`, so that `go tool staticcheck ./...` runs. This updates `go.mod` and `go.sum`.
- [X] T003 [P] Create `Makefile` with a `check` target that runs, in order, `test -z "$(gofmt -l .)"`, `go vet ./...`, `go tool staticcheck ./...` and `go test ./...` (the quality gate in quickstart.md step 1).
- [X] T004 [P] Create `.gitignore` that ignores `bin/`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Value types, entities, persistence and the CLI skeleton that every story depends on

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Tests (write first, confirm they fail)

- [X] T005 [P] Write table-driven tests for `ISBN` in `internal/domain/isbn_test.go`:
  - Hyphens are allowed.
  - A 10-digit ISBN converts to 13 digits.
  - An invalid check digit is an error.
  - Wrong length and non-digit characters are errors.
- [X] T006 [P] Write tests for `TimeOfDay`, `Hours` and `Date` parsing in `internal/domain/time_test.go`:
  - `HH:MM` 形式、00:00〜24:00. Out of range is an error.
  - `Hours` requires 開館 < 閉館.
  - `Date` is `YYYY-MM-DD` in Asia/Tokyo.
- [X] T007 [P] Write tests for `LibraryID` in `internal/domain/library_test.go`:
  - Parsing and formatting use `SystemID:LibKey`.
  - An empty `SystemID` or `LibKey` is an error.
  - A `LibKey` may contain `:` (only the first `:` splits).
- [X] T008 [P] Write round-trip tests for data.json in `internal/store/data_test.go`:
  - Save and load with `version: 1` (data-model.md「永続化形式」).
  - A missing file loads as empty data.
  - An unknown `version` is an error.
  - Writes are atomic (write to a temp file in the same directory, then rename).
  - `LIBREAD_DATA_DIR` overrides the default `os.UserConfigDir()/libread`.
- [X] T009 [P] Write tests for `cli.Run` dispatch in `internal/cli/cli_test.go`:
  - An unknown subcommand returns exit code `2` with usage on stderr.
  - Resolving `<本>` by ISBN or 書名の部分一致: no match or multiple matches returns exit code `2`, listing the candidates when there are several.
  - Resolving `<図書館>` by `SystemID:LibKey` or 図書館名の部分一致 follows the same rules.

### Implementation

- [X] T010 [P] Implement `ISBN` (13桁に正規化; ハイフン許容; 10桁は13桁へ変換; チェックディジット不正はエラー) in `internal/domain/isbn.go`
- [X] T011 [P] Implement `TimeOfDay` (minutes since 0:00), `Hours` and `Date` in `internal/domain/time.go`.
  - Load Asia/Tokyo once via `time.LoadLocation`.
  - Add `import _ "time/tzdata"` in this file.
- [X] T012 [P] Implement `LibraryID{SystemID, LibKey}` in `internal/domain/library.go`, with `ParseLibraryID` and `String()`.
- [X] T013 Define the entities in `internal/domain/entities.go`, following data-model.md:
  - `Book{ISBN, Title, Author, Publisher}`. Title is required.
  - `ReadingBook{Book, Note, AddedOn}`.
  - `RegisteredLibrary{ID, Name, Pref, City, Schedule *Schedule, Closures []ClosurePeriod}`.
  - `Schedule{Weekly [7]DayRule, Holiday HolidayRule, UpdatedOn}`. `DayRule` is closed or `Hours`. `HolidayRule` is `SameAsWeekday` / `Closed` / `CustomHours(Hours)`.
  - `ClosurePeriod{Start, End, Reason}` (Start ≤ End, inclusive).
- [X] T014 Implement `internal/store/data.go` (depends on T013):
  - `Load` and `Save` for `data.json`, using JSON structs with explicit tags.
  - `weekly` omitted days mean 休館.
  - `holiday.mode` is one of `same` / `closed` / `hours`.
  - Convert to and from the domain types at this boundary; do not use `any`.
- [X] T015 Implement `internal/cli/cli.go` (depends on T013):
  - `Run(ctx, args, env Env) int`, where `Env` holds stdout/stderr, `Now func() time.Time`, `*http.Client`, data and cache dirs, and the Calil key.
  - Subcommand dispatch using `flag.FlagSet`.
  - Exit codes `0/1/2` as in contracts/cli.md.
  - Helpers that resolve `<本>` and `<図書館>`.
- [X] T016 Implement `cmd/libread/main.go`:
  - Build `cli.Env` from `os.Getenv` (`LIBREAD_CALIL_APPKEY`, `LIBREAD_DATA_DIR`, `LIBREAD_CACHE_DIR`), `time.Now` and an `http.Client` with a 10s timeout.
  - Call `os.Exit(cli.Run(...))`.
- [X] T017 [P] Implement `internal/store/cache.go`, a JSON file cache under `LIBREAD_CACHE_DIR` (default `os.UserCacheDir()/libread`).
  - Get and put typed values, each with a stored-at time.
  - Write atomically (temp file in the same directory, then rename).
  - Add tests in `internal/store/cache_test.go` first.

**Checkpoint**: `go test ./...` passes and `go run ./cmd/libread` prints usage with exit code 2.

---

## Phase 3: User Story 1 - 今日、続きを読める図書館を知る (Priority: P1) 🎯 MVP

**Goal**: `libread` / `libread check [<本>]` shows, grouped by library, where each 読みかけの本 can be read today (FR-001〜008, 012, 017).

**Independent Test**:
- Hand-write a `data.json` fixture with books, libraries, schedules and closures.
- Run `cli.Run` with a fixed `Now` and an `httptest.Server` that serves recorded Calil and holidays-jp responses.
- Compare the output with golden files for spec US1 acceptance scenarios 1–5.

### Tests for User Story 1 (write first, confirm they fail)

- [X] T018 [P] [US1] Write tests in `internal/domain/availability_test.go` for mapping a Calil value to `Availability`, covering every row of research.md R2:
  - 貸出可→AvailableToLend, 館内のみ→InLibraryOnly, 貸出中→OnLoan, 予約中→Reserved (label is Calil's value 予約中 as-is).
  - 蔵書なし, or a library missing from the response while the system status is OK/Cache → NotHeld.
  - 蔵書あり / 準備中 / 休館中 / unknown values → Unknown.
  - System status Error, or Running at the deadline → Unknown.
  - The raw value is preserved.
- [X] T019 [P] [US1] Write tests in `internal/domain/openstatus_test.go` for `OpenStatusOf`, one case per step of data-model.md「OpenStatusOf の判定順」:
  - NoSchedule, ClosedPeriod (inclusive bounds), HolidayUnknown (only when the holiday rule is not `SameAsWeekday`).
  - Holiday rule `Closed` and `CustomHours`.
  - ClosedRegular.
  - NotYetOpen / Open / FinishedToday at the boundaries (exactly open time, exactly close time).
  - The 閉館まで30分未満 flag.
- [X] T020 [P] [US1] Write tests in `internal/domain/result_test.go` for `Readable` and the result aggregation:
  - Readable only when OpenStatus ∈ {Open, NotYetOpen} and Availability ∈ {AvailableToLend, InLibraryOnly} and the snapshot is not stale (>30分 is stale).
  - Libraries with ≥1 readable book are in `Readable`, sorted by 読める冊数の多い順 → 閉館時刻の遅い順 (FR-008).
  - Other libraries are in `NotReadable`, with their reasons.
- [ ] T021 [P] [US1] After the holidays-jp row of research.md R10 is filled in, record a real holidays-jp response to `internal/holiday/testdata/2026.json`. Then write tests in `internal/holiday/holiday_test.go`:
  - **進捗 (2026-10-22)**: テストは記述済み。実装環境から holidays-jp に接続できなかったため、実レスポンスの記録（`testdata/2026.json`）は未実施。テストは同じ形式の応答をテスト内で組み立てて使っている（`holiday_test.go` の TODO(T021)）。記録したら置き換えて `[X]` にする。
  - Holiday lookup by `Date`.
  - The year cache is reused within 30 days and refreshed after.
  - On fetch failure with no cache, the result is `known=false`.
  - The request includes `User-Agent: libread/<version>`.
- [ ] T022 [P] [US1] Write tests in `internal/calil/check_test.go` for the check client:
  - **進捗 (2026-10-22)**: 手順 2・3 は完了。実装環境からカーリルに接続できず、アプリケーションキーもなかったため、手順 1（実レスポンスの記録）は未実施。テストは同じ形式の応答をテスト内で組み立てて使っている（`check_test.go` の TODO(T022)）。記録したら置き換えて `[X]` にする。
  1. After the Calil row of research.md R10 is filled in, record real responses: an initial reply with `continue: 1`, a poll reply, and a final reply (`continue: 0`) whose system statuses include OK, Cache and Error. Save them to `internal/calil/testdata/check_*.json`, removing the `appkey` value from every saved URL and body.
  2. Add `TestFixturesHaveNoAppKey` in `internal/calil/fixtures_test.go`. It fails if any file under `internal/calil/testdata/` contains `appkey=` followed by a non-empty value (Constitution 技術的制約: no secrets in the repository).
  3. With `httptest.Server`, assert:
     - One request carries comma-separated `isbn` and `systemid`, and `appkey` is sent.
     - Polls use `session` and wait ≥2秒 between requests (use an injected sleep function).
     - Polling stops at the 8-second overall deadline, leaving still-Running systems marked as timed out.
     - The JSON is decoded into typed structs.
- [X] T023 [P] [US1] Write tests in `internal/cli/check_test.go` for the `check` command:
  - Golden-file tests in `internal/cli/testdata/check_*.golden` for spec US1 acceptance scenarios 1–5.
  - Edge cases from spec.md: 開館前 (counted as readable, shown as `開館前 HH:MM〜HH:MM`), 本日終了, 閉館まで30分未満, 臨時休館, 開館情報未入力, 不明(古い情報), 取得タイムアウト, 予約中 (shown with Calil's value as-is).
  - With no libraries or no books, exit code 2 and the guidance message.
  - A missing `LIBREAD_CALIL_APPKEY` gives exit code 1 with a message explaining how to set it.
  - A fresh cache (≤30分) is reused without an HTTP request.
  - A stale cache plus a failed refetch shows `不明（古い情報: HH:MM 取得）`.
  - The Calil links `https://calil.jp/book/<ISBN>` are printed.

### Implementation for User Story 1

- [X] T024 [P] [US1] Implement `Availability` and `AvailabilityOf` in `internal/domain/availability.go`:
  - Values: `AvailableToLend`, `InLibraryOnly`, `OnLoan`, `Reserved`, `NotHeld`, `Unknown`.
  - Japanese labels: 在架（貸出可）, 在架（館内閲覧のみ）, 貸出中, 予約中 (Calil's value as-is), 所蔵なし, 不明.
  - `HoldingSnapshot{ISBN, SystemID, SystemStatus, ByLibKey, FetchedAt}`.
- [ ] T025 [P] [US1] Implement `OpenStatus` in `internal/domain/openstatus.go`:
  - Values: `Open`, `NotYetOpen`, `FinishedToday`, `ClosedRegular`, `ClosedPeriod`, `NoSchedule`, `HolidayUnknown`.
  - `OpenStatusOf(lib, now, isHoliday func(Date) (bool, bool))`, following the order in data-model.md.
- [ ] T026 [US1] Implement `Readable`, `Result{CheckedAt, Readable, NotReadable}` and the FR-008 sort in `internal/domain/result.go`. Depends on T024 and T025.
- [ ] T027 [P] [US1] Implement the holidays-jp client in `internal/holiday/holiday.go`:
  - Endpoint `https://holidays-jp.github.io/api/v1/{year}/date.json`.
  - Uses `store` cache files named `holidays-YYYY.json`, with a 30-day refresh.
  - Returns `func(Date) (isHoliday, known bool)`.
  - Base URL is injectable for tests.
- [ ] T028 [P] [US1] Implement the Calil check client in `internal/calil/check.go`:
  - `Check(ctx, isbns []ISBN, systemIDs []string) ([]HoldingSnapshot, error)` against `https://api.calil.jp/check` with `format=json&callback=no`.
  - Poll with `session` at ≥2秒 intervals, under an 8-second overall deadline.
  - Measure the deadline with the injected clock (the start time from `Now` plus 8 seconds), not with `context.WithTimeout` or a real timer, so that T022 and T041 can test it with fake time. Still honour cancellation of the caller's `ctx`.
  - Send `User-Agent: libread/<version>`.
  - Base URL, sleep and clock are injectable.
- [ ] T029 [US1] Implement the `check` command in `internal/cli/check.go`. Depends on T026–T028 and T017.
  1. Load data and select the target books (all by default, or the resolved `<本>`).
  2. Collect the distinct system IDs.
  3. Use cached holdings `holdings.json`, keyed `ISBN|SystemID`, when ≤30分 old; otherwise refetch.
  4. On refetch failure, keep the stale snapshot marked stale.
  5. Compute `OpenStatusOf` per library and build `Result`.
- [ ] T030 [US1] Implement the result formatter in `internal/cli/format.go`, exactly matching the output example in contracts/cli.md:
  - Header `YYYY-MM-DD(曜) HH:MM 時点`.
  - The two sections, with the `今日読める図書館はありません` fallback.
  - Library status: `開館中 〜HH:MM`, `開館前 HH:MM〜HH:MM` for libraries not yet open (FR-004), and the `（閉館まで30分未満）` note.
  - Per-book `（HH:MM 取得）` when fetch times differ.
  - The footer with Calil links, `開館情報: 利用者入力（最終更新 ...）` and `※ 臨時休館は入力済みの情報のみ反映しています`.
- [ ] T031 [US1] Make `libread` with no arguments run `check` (contracts/cli.md), wiring it in `internal/cli/cli.go`.

**Checkpoint**: The US1 golden tests pass. With a hand-written `data.json`, the real `libread` shows today's readable libraries.

---

## Phase 4: User Story 2 - 読みかけの本を登録する (Priority: P2)

**Goal**: Find books by 書名 or ISBN and manage 読みかけの本 and their notes (FR-009, FR-010).

**Independent Test**:
- Against an `httptest.Server` that serves a recorded NDL response, run `book find` → `book add` → `book note` → `book list` → `book rm`.
- Check `data.json` and the output after each step (spec US2 acceptance scenarios 1–3).

### Tests for User Story 2 (write first, confirm they fail)

- [ ] T032 [P] [US2] After the NDL row of research.md R10 is filled in, record real NDL OpenSearch responses (a title search and an ISBN search) to `internal/ndl/testdata/*.xml`. Then write tests in `internal/ndl/ndl_test.go`:
  - Parse ISBN, title, author and publisher (the elements are confirmed from the recording, research.md R3).
  - Skip entries without an ISBN.
  - Return at most 10 results.
  - Requests use the `title` or `isbn` parameter, plus `cnt` and `dpid=iss-ndl-opac` (research.md R3), and send `User-Agent`.
- [ ] T033 [P] [US2] Write tests in `internal/cli/book_test.go`:
  - `book add` rejects a duplicate ISBN (同じ ISBN は1冊しか登録できない) with exit code 2.
  - `book add` looks up the bibliographic record by ISBN and stores `AddedOn` = today.
  - `book note` overwrites the note; an empty string clears it.
  - `book rm` resolves `<本>` and removes the book.
  - The `book list` output format is tested.
  - Notes are never sent in any HTTP request.
  - `book find` and `book add` end with the NDL attribution line from contracts/cli.md, and so does `libread help` (research.md R10).

### Implementation for User Story 2

- [ ] T034 [P] [US2] Implement the NDL client in `internal/ndl/ndl.go`:
  - `Search(ctx, query) ([]domain.Book, error)`. A query that parses as an ISBN uses the `isbn` parameter; otherwise it uses `title`.
  - Endpoint `https://ndlsearch.ndl.go.jp/api/opensearch`, always with `dpid=iss-ndl-opac`.
  - Decode with `encoding/xml` into typed structs.
  - Base URL is injectable.
- [ ] T035 [US2] Implement the `book find`, `book add <ISBN> [--note]`, `book list`, `book note <本> <テキスト>` and `book rm <本>` subcommands in `internal/cli/book.go`, and register them in `internal/cli/cli.go`. Depends on T034.
  - Print the NDL attribution line from contracts/cli.md at the end of `book find` and `book add`, and add it to the end of `writeUsage` in `internal/cli/cli.go` (research.md R10).

**Checkpoint**: Books can be registered from the CLI, and `libread` (US1) uses them without editing `data.json` by hand.

---

## Phase 5: User Story 3 - 利用する図書館を登録する (Priority: P3)

**Goal**: Register 利用図書館 from all of Japan and enter 開館スケジュール and 臨時休館期間 (FR-011, FR-016).

**Independent Test**:
- Against an `httptest.Server` that serves a recorded Calil library response, run `lib find` → `lib add` → `lib hours` → `lib closure add` → `lib list` → `lib rm`.
- Check that US1's `check` target changes accordingly (spec US3 acceptance scenarios 1–3).

### Tests for User Story 3 (write first, confirm they fail)

- [ ] T036 [P] [US3] Record a real Calil library search response (e.g. `pref=東京都&city=世田谷区`) to `internal/calil/testdata/library_*.json`, removing the `appkey` value. These files are covered by `TestFixturesHaveNoAppKey` in `internal/calil/fixtures_test.go`; if T022 is not done yet, create that test first as T022 step 2 describes. Then write tests in `internal/calil/library_test.go`:
  - Parse `systemid`, `libkey`, `formal`, `pref`, `city` and `address`.
  - `pref` is required.
  - `appkey` and `User-Agent` are sent.
- [ ] T037 [P] [US3] Write tests in `internal/cli/lib_test.go`:
  - `lib find` requires `--pref` (exit code 2 without it) and filters by `--name` 部分一致 locally.
  - `lib add` rejects duplicates and unknown IDs.
  - `lib hours`: `HH:MM-HH:MM` or `closed` per day; days not given keep their current value, or become 休館 when no schedule exists yet; `--holiday same|closed|HH:MM-HH:MM` defaults to `same`; `UpdatedOn` = today.
  - `lib closure add`: Start ≤ End, otherwise exit code 2.
  - `lib closure list` numbering, and `lib closure rm <番号>`.
  - `lib rm` removes the library.

### Implementation for User Story 3

- [ ] T038 [P] [US3] Implement the Calil library search in `internal/calil/library.go`:
  - `Libraries(ctx, pref, city string) ([]LibraryInfo, error)` against `https://api.calil.jp/library` with `format=json&callback=`.
  - Decode into typed structs.
- [ ] T039 [US3] Implement the `lib find`, `lib add`, `lib list`, `lib rm` and `lib hours` subcommands in `internal/cli/lib.go`, and register them in `internal/cli/cli.go`. Depends on T038.
- [ ] T040 [US3] Implement the `lib closure add|list|rm` subcommands in `internal/cli/closure.go`, and register them in `internal/cli/cli.go`. Depends on T039.

**Checkpoint**: Every operation works from the CLI without editing `data.json` by hand.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Cross-story checks

- [ ] T041 [P] Add a test in `internal/cli/check_perf_test.go` for SC-002:
  - 10 libraries across 10 systems and 5 books.
  - A fake Calil server that never completes (stays Running).
  - Assert `check` returns within 10 seconds of injected time and marks the libraries 不明（取得タイムアウト）.
- [ ] T042 [P] Add a test in `internal/cli/outbound_test.go` that records every outgoing HTTP request across all commands and asserts (Constitution 技術的制約):
  - No request contains a book note.
  - Per command, requests stay within the limits in research.md R9: `check` sends one Calil session (initial request plus polls ≥2秒 apart) and at most one holidays-jp request per year, and sends no Calil request when the holdings cache is ≤30分 old; `lib find` sends one Calil library request; `book find` and `book add` send one NDL request.
  - At most one request to each service is in flight at a time.
  - Every request carries `User-Agent: libread/<version>`.
- [ ] T043 Run the `check` target in `Makefile` (gofmt, go vet, staticcheck, go test) and fix every warning.
- [ ] T044 Run quickstart.md steps 2 and 3 against the real services with a temporary `LIBREAD_DATA_DIR`, and record any differences in `specs/001-readable-libraries-today/quickstart.md`.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies.
- **Foundational (Phase 2)**: Depends on Setup. Blocks all user stories.
- **US1 (Phase 3)**: Depends on Foundational.
- **US2 (Phase 4)**: Depends on Foundational. Independent of US1; `book find` and `book add` work alone.
- **US3 (Phase 5)**: Depends on Foundational. Independent of US1 and US2, except that T036 relies on `TestFixturesHaveNoAppKey` from T022. If US3 is built before US1, create that test first.
- **Polish (Phase 6)**: Depends on US1–US3.

### Within Each Phase

- Tests are written first and must fail before the matching implementation (T005–T009 → T010–T016, T018–T023 → T024–T031, and so on).
- domain → external clients → cli.
- `internal/cli/cli.go` is edited by T015, T031, T035, T039 and T040. Do these in order, never in parallel.

### Parallel Opportunities

- Phase 1: T003 and T004.
- Phase 2: the tests T005–T009; then T010, T011, T012 and T017.
- US1: the tests T018–T023; then T024, T025, T027 and T028.
- US2: T032 and T033; then T034.
- US3: T036 and T037; then T038.
- After Phase 2, US1, US2 and US3 can proceed in parallel, keeping `cli.go` edits sequential.

---

## Parallel Example: User Story 1

```bash
# Tests first (all different files):
Task: "Availability mapping tests in internal/domain/availability_test.go"
Task: "OpenStatusOf tests in internal/domain/openstatus_test.go"
Task: "Readable/aggregation tests in internal/domain/result_test.go"
Task: "holidays-jp client tests in internal/holiday/holiday_test.go"
Task: "Calil check client tests in internal/calil/check_test.go"

# Then independent implementations:
Task: "Availability in internal/domain/availability.go"
Task: "OpenStatus in internal/domain/openstatus.go"
Task: "holidays-jp client in internal/holiday/holiday.go"
Task: "Calil check client in internal/calil/check.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 and Phase 2.
2. Phase 3 (US1).
3. **STOP and VALIDATE**: hand-write `data.json` with real ISBNs, Calil `SystemID:LibKey` values and hours, then run `libread` against the real services.

### Incremental Delivery

1. Setup + Foundational.
2. US1: the search works from a hand-written `data.json` (MVP).
3. US2: books are registered from the CLI.
4. US3: libraries, hours and closures are registered from the CLI. No manual data editing is needed after this.
5. Polish.

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Verify tests fail before implementing (Constitution II)
- Record external responses once with a real API key; never commit the key (`appkey` must be stripped from recorded URLs or fixtures)
- Stop at any checkpoint to validate story independently
