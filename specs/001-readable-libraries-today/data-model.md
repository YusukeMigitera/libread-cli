# Data Model: 今日読める図書館の検索

**Feature**: [spec.md](./spec.md) | **Research**: [research.md](./research.md)

ドメイン概念はすべて専用の型で表す（原則 III）。以下の型名は Go の `internal/domain` パッケージの型に対応する。

## 値型

| 型 | 内容 | 検証ルール |
|---|---|---|
| `ISBN` | 13桁に正規化した ISBN | 入力はハイフンを許容する。10桁は13桁へ変換する。チェックディジットが不正ならエラー |
| `LibraryID` | `SystemID` + `LibKey`（カーリルの図書館システムIDと館キー） | どちらも空でないこと。表示・指定時の形式は `SystemID:LibKey` |
| `TimeOfDay` | 0:00 からの分数 | `HH:MM` 形式、00:00〜24:00 |
| `Hours` | 開館 `TimeOfDay` と閉館 `TimeOfDay` の組 | 開館 < 閉館 |
| `Date` | 暦日（Asia/Tokyo） | `YYYY-MM-DD` |

## 列挙型

### `Availability`（所蔵状況、FR-003）

`AvailableToLend`（在架・貸出可） / `InLibraryOnly`（在架・館内閲覧のみ） / `OnLoan`（貸出中） /
`Reserved`（予約中。カーリルの値をそのまま表示） / `NotHeld`（所蔵なし） / `Unknown`（不明）

- 「読める」とするのは `AvailableToLend` と `InLibraryOnly` だけ。
- カーリルの値との対応は [research.md R2](./research.md#r2-所蔵状況の分類fr-003-への対応付け) を参照。

### `OpenStatus`（今日の開館状態）

| 値 | 意味 | 表示 |
|---|---|---|
| `Open` | 現在時刻が開館時刻〜閉館時刻のあいだ | 〜HH:MM（閉館まで30分未満なら注記を付ける） |
| `NotYetOpen` | 今日開館するが、まだ開館時刻前 | 開館前 HH:MM〜HH:MM（開館時刻を併記する） |
| `FinishedToday` | 今日の閉館時刻を過ぎた | 本日の開館は終了 |
| `ClosedRegular` | 定休日（曜日、または祝日の扱いが「休館」） | 休館（定休日） |
| `ClosedPeriod` | 臨時休館期間に今日が含まれる | 休館（期間・理由） |
| `NoSchedule` | 開館スケジュールが未入力 | 開館情報未入力 |
| `HolidayUnknown` | 祝日かどうか判定できず、判定によって結果が変わる | 開館情報不明（祝日判定不可） |

- 「読める」とするのは `Open` と `NotYetOpen`（今日まだこれから行ける）だけ（spec FR-004）。

## エンティティ

### Book（本）

| フィールド | 型 | 備考 |
|---|---|---|
| ISBN | `ISBN` | 同一性のキー（spec Edge Case: 版違いは別の本） |
| Title | string | 必須 |
| Author | string | 任意 |
| Publisher | string | 任意 |

### ReadingBook（読みかけの本）

| フィールド | 型 | 備考 |
|---|---|---|
| Book | `Book` | |
| Note | string | 読んだ位置などの自由記述。外部サービスへは送らない |
| AddedOn | `Date` | |

- 同じ ISBN は1冊しか登録できない。
- ライフサイクル: 登録 → メモの編集（何度でも） → 削除（読了）。状態は持たない。

### RegisteredLibrary（利用図書館）

| フィールド | 型 | 備考 |
|---|---|---|
| ID | `LibraryID` | 同一性のキー |
| Name | string | カーリルの正式名（`formal`） |
| Pref, City | string | 表示・検索用 |
| Schedule | `*Schedule` | 未入力なら nil（`NoSchedule`） |
| Closures | `[]ClosurePeriod` | |

### Schedule（開館スケジュール）

| フィールド | 型 | 備考 |
|---|---|---|
| Weekly | `[7]DayRule` | 日曜〜土曜。`DayRule` は「休館」または `Hours` |
| Holiday | `HolidayRule` | `SameAsWeekday` / `Closed` / `CustomHours(Hours)` |
| UpdatedOn | `Date` | FR-006 の「最終更新日」 |

### ClosurePeriod（臨時休館期間）

| フィールド | 型 | 備考 |
|---|---|---|
| Start, End | `Date` | Start ≤ End。両端の日を含む |
| Reason | string | 任意 |

### HoldingSnapshot（所蔵状況。キャッシュに保存する）

| フィールド | 型 | 備考 |
|---|---|---|
| ISBN | `ISBN` | |
| SystemID | string | |
| SystemStatus | string | カーリルのシステム単位の status（`OK` / `Cache` / `Running` / `Error`） |
| ByLibKey | map[LibKey]string | カーリルが返した元の値 |
| FetchedAt | time.Time | FR-006 の「取得日時」 |

- 30分を過ぎたスナップショットは「古い」とみなす（research R7）。

## 判定ロジック（純粋関数。外部に依存しない）

```text
OpenStatusOf(lib RegisteredLibrary, now time.Time, isHoliday func(Date) (bool, known bool)) OpenStatus
AvailabilityOf(snapshot HoldingSnapshot, libKey, now) (Availability, raw string, stale bool)
Readable(open OpenStatus, avail Availability, stale bool) bool
  = open ∈ {Open, NotYetOpen} && avail ∈ {AvailableToLend, InLibraryOnly} && !stale
```

OpenStatusOf の判定順:

1. Schedule が nil なら `NoSchedule`。
2. 今日が Closures のどれかに含まれるなら `ClosedPeriod`。
3. 祝日かどうかを調べる。
   判定できず、祝日の扱いが `SameAsWeekday` 以外なら `HolidayUnknown`。
4. 祝日なら Holiday ルールで、そうでなければ Weekly の今日の曜日で、今日の DayRule を決める。
5. DayRule が「休館」なら `ClosedRegular`。
6. 現在時刻で振り分ける: 開館時刻前なら `NotYetOpen`、開館中なら `Open`、閉館時刻以降なら `FinishedToday`。

## 検索結果（表示用の集約）

```text
Result
├── CheckedAt
├── Readable[]      — 1冊以上読める図書館（FR-008: 読める冊数の多い順 → 閉館時刻の遅い順）
│     └── Library, OpenStatus, Hours, Books[] (Book, Availability, raw, FetchedAt)
└── NotReadable[]   — 読める本がない図書館（理由: OpenStatus、または本ごとの Availability）
```

## 永続化形式

`data.json`（スキーマバージョン付き）:

```json
{
  "version": 1,
  "books": [
    {"isbn": "9784000000000", "title": "...", "author": "...", "publisher": "...",
     "note": "p.120まで", "addedOn": "2026-10-21"}
  ],
  "libraries": [
    {"systemId": "Tokyo_Setagaya", "libKey": "中央", "name": "世田谷区立中央図書館",
     "pref": "東京都", "city": "世田谷区",
     "schedule": {
       "weekly": {"sun": {"open": "09:00", "close": "17:00"}, "mon": {"closed": true}},
       "holiday": {"mode": "same"},
       "updatedOn": "2026-10-21"
     },
     "closures": [{"start": "2026-11-10", "end": "2026-11-14", "reason": "特別整理期間"}]}
  ]
}
```

- `weekly` に書かれていない曜日は「休館」として扱う。
- `holiday.mode` は `same` / `closed` / `hours`。`hours` の場合は `open` と `close` も持つ。

キャッシュ（`os.UserCacheDir()/libread/`）:

- `holdings.json`: キー `ISBN|SystemID` → HoldingSnapshot
- `holidays-YYYY.json`: holidays-jp の応答と取得日時
