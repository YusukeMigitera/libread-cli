# HTTP API Contract: libread

**Feature**: [spec.md](../spec.md) | **Data Model**: [data-model.md](../data-model.md) | **Screens**: [screens.md](./screens.md)

サーバー（Go）と画面（TypeScript）のあいだの約束。両側のテストで、この形に従っていることを確かめる（Constitution 原則 III）。

## 共通

- 各 API の応答の例は `specs/002-web-mobile-access/contracts/examples/<名前>.json` に置く。この文書に載せた例はそのファイルと同じ内容で、
  サーバーと画面の両方のテストが同じファイルを使う（Constitution 原則 III）。
- 画面と API は同じオリジンで配信する。CORS のヘッダーは付けない。
- 本文は UTF-8 の JSON（`Content-Type: application/json; charset=utf-8`）。日時は RFC 3339 で `+09:00`、日付は `YYYY-MM-DD`、時刻は `HH:MM`。
- 本人確認（research W2）:
  - `/api/` 以下はすべて、有効なセッション Cookie（`libread_session`）が必要。
  - 無効・期限切れ・世代が古い・Cookie なしは、どの URL・どのメソッドでも同じ応答を返す。データの有無で応答を変えない（FR-110）。

    ```http
    HTTP/1.1 401 Unauthorized
    {"error": {"code": "unauthenticated", "message": "ログインしてください"}}
    ```

  - 本人確認の確認は、URL の存在確認より先に行う。確認できない相手には、存在しない URL にも `404` ではなく `401` を返す。
- CSRF 対策: `POST`・`PUT`・`PATCH`・`DELETE` は、`Origin` ヘッダーが自分のオリジンと一致しなければ `403`（`"code": "forbidden"`）。
- エラーの形はすべて共通:

  ```json
  {"error": {"code": "<下の表の値>", "message": "<画面にそのまま出せる日本語>"}}
  ```

  | HTTP | code | 例 |
  |---|---|---|
  | 400 | `invalid` | ISBN のチェックディジットが不正、時刻の形式が不正、開始日が終了日より後 |
  | 401 | `unauthenticated` | 本人確認ができない |
  | 403 | `forbidden` | `Origin` が一致しない |
  | 404 | `notFound` | 登録していない本・図書館・臨時休館期間を指定した |
  | 409 | `conflict` | すでに登録済み、取り込み済み（FR-118） |
  | 502 | `upstream` | 外部サービス（カーリル・NDL サーチ）に接続できない、時間内に応答がない |
  | 503 | `notConfigured` | カーリルのアプリケーションキーが設定されていない |
  | 500 | `internal` | 保存先の読み書きの失敗など |

- 外部サービスのエラーの `message` にアプリケーションキーを含めない（001 と同じ）。
- すべての応答に `Strict-Transport-Security: max-age=31536000` と `Cache-Control: no-store`（`/api/` と `/auth/`）を付ける（FR-119）。
- メモは外部サービスに送らない。外部に送るのは ISBN、図書館システムID、検索語、年だけ（FR-114）。

## 本人確認

### `GET /auth/login?return=<画面>`

- `return` は `/`・`/books`・`/libraries` のどれか（それ以外は `/`）。
- `libread_login` Cookie（state と戻り先。10分）を設定し、Google の認可画面へ `302` でリダイレクトする。
  パラメーター: `response_type=code`、`client_id`、`redirect_uri=<オリジン>/auth/callback`、`scope=openid email`、`state`、`prompt=select_account`。

### `GET /auth/callback?code=...&state=...`

- `libread_login` を必ず消す。
- `state` が一致し、ID トークンが本人のものなら、`libread_session` Cookie（30日）を設定して `302` で `/#<戻り先>` へ。
- 本人以外、`state` の不一致、Google がエラーを返した場合は、Cookie を発行せず `302` で `/#/login?error=denied` へ。
  本人以外か、それ以外の失敗かは区別しない。

### `GET /api/session`

本人確認の状態を返す。画面は起動時にこれを呼び、`401` ならログイン画面を表示する。

```json
{"expiresAt": "2026-12-05T08:00:00+09:00"}
```

- 本人のメールアドレスは返さない（画面に必要ないため）。

### `POST /api/session/logout`

この端末の `libread_session` を消す。`204`。

### `POST /api/session/revoke-all`

セッション世代番号を1つ進め、すべての端末（この端末も含む）の確認を取り消す（FR-111）。この端末の Cookie も消す。`204`。

## 検索（User Story 1）

### `GET /api/check`

### `GET /api/check?isbn=<ISBN>`

- 読みかけの本すべて、または指定した1冊について、今日読める図書館を返す（FR-101・FR-102）。
- すべての本の所蔵状況がそろってから1回で返す（FR-104）。カーリルへの問い合わせは001と同じく8秒で打ち切り、
  打ち切った本は「不明（取得タイムアウト）」とする。
- 呼ぶたびに、その時点の時刻（日本時間）で判定し直す（FR-105）。所蔵状況は30分以内のキャッシュを使う（001 と同じ）。
- 利用図書館が0館、または読みかけの本が0冊のときは `200` で `"setupRequired"` を返す（spec Edge Cases）。

```json
{"setupRequired": "libraries"}
```

  `"libraries"`（利用図書館が0館）、`"books"`（読みかけの本が0冊）。両方0なら `"libraries"`。

- 指定した ISBN が読みかけの本になければ `404`。

通常の応答:

```json
{
  "checkedAt": "2026-10-21T14:05:00+09:00",
  "readable": [
    {
      "library": {"id": "Tokyo_Setagaya:中央図書館", "name": "世田谷区立中央図書館"},
      "opening": {
        "status": "open",
        "label": "開館中 〜19:00",
        "open": "09:00",
        "close": "19:00",
        "closingSoon": false
      },
      "books": [
        {
          "isbn": "9784003101018",
          "title": "吾輩は猫である",
          "availability": "availableToLend",
          "label": "在架（貸出可）",
          "readable": true,
          "stale": false,
          "fetchedAt": "2026-10-21T14:04:50+09:00"
        }
      ]
    }
  ],
  "notReadable": [
    {
      "library": {"id": "Tokyo_Setagaya:経堂図書館", "name": "世田谷区立経堂図書館"},
      "opening": {"status": "noSchedule", "label": "開館情報未入力"},
      "books": []
    }
  ],
  "sources": {
    "holdings": {
      "label": "所蔵情報: カーリル 14:04 取得（30分以内の取得結果を使用）",
      "links": [{"title": "吾輩は猫である", "url": "https://calil.jp/book/9784003101018"}]
    },
    "openingInfo": {
      "label": "開館情報: 利用者入力（最終更新 中央図書館 2026-10-21）",
      "note": "※ 臨時休館は入力済みの情報のみ反映しています"
    },
    "holidays": {"label": "祝日: holidays-jp（https://holidays-jp.github.io/）"}
  }
}
```

- `readable` は 001 FR-008 の順（読める冊数の多い順 → 閉館時刻の遅い順）、`notReadable` は登録順。並べ替えはサーバーが行い、画面は並べ替えない。
- `readable` が空のとき、画面は「今日読める図書館はありません」と表示する。
- `opening.status`: `open` / `notYetOpen` / `finishedToday` / `closedRegular` / `closedPeriod` / `noSchedule` / `holidayUnknown`（001 data-model の `OpenStatus`）。
  `open`・`close` は `open`・`notYetOpen`・`finishedToday` のときだけ、`closingSoon` は `open` のときだけ付ける。
- `books[].availability`: `availableToLend` / `inLibraryOnly` / `onLoan` / `reserved` / `notHeld` / `unknown`（001 data-model の `Availability`）。
- `notReadable` の館で `opening.status` が `open`・`notYetOpen` 以外のときは、`books` を空にする（001 の CLI と同じく、今日行けない館は開館状態だけを理由に示す）。
- `label` の文言は、001 の CLI の出力（[001 contracts/cli.md](../../001-readable-libraries-today/contracts/cli.md)）と同じ（research W5、SC-103）。
- `stale` が `true` の本は `readable` が必ず `false`（Constitution 原則 IV）。`fetchedAt` は、取得できなかった本では付けない。

## 読みかけの本（User Story 2）

### `GET /api/books`

```json
{"books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "author": "夏目漱石", "publisher": "岩波書店", "note": "p.120まで", "addedOn": "2026-10-21"}]}
```

登録順。

### `GET /api/book-search?q=<書名|ISBN>&author=<著者>&publisher=<出版社>`

- NDL サーチで探す（001 の `book find` と同じ。最大10件、並べ替えも同じ）。`q` は必須。
- `registered` は、すでに読みかけの本として登録済みなら `true`。

```json
{
  "books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "author": "夏目漱石", "publisher": "岩波書店", "registered": false}],
  "total": 345,
  "truncated": true,
  "hint": "表示しきれていない本があります。見つからないときは著者や出版社で絞り込むか、ISBN で探してください",
  "credit": {
    "label": "書誌情報: 国立国会図書館サーチ API から取得した「国立国会図書館蔵書」（国立国会図書館が運営）のデータを利用しています",
    "url": "https://ndlsearch.ndl.go.jp/",
    "license": {"label": "クリエイティブ・コモンズ 表示 4.0 国際", "url": "https://creativecommons.org/licenses/by/4.0/legalcode.ja"}
  }
}
```

- `hint` は `truncated` のとき、または一致はあるが ISBN のある本が1件もないときだけ付ける（001 と同じ条件）。

### `POST /api/books`

```json
{"isbn": "9784003101018", "note": "p.120まで"}
```

- NDL サーチで ISBN の書誌を確かめてから登録する（001 の `book add` と同じ）。`note` は省略できる。
- `201` で登録した本（`GET /api/books` の1件と同じ形）を返す。登録済みなら `409`。

### `PATCH /api/books/{isbn}`

```json
{"note": "p.180まで"}
```

- メモを書き換える。空文字ならメモを消す。`200` で更新後の本を返す。

### `DELETE /api/books/{isbn}`

- 読みかけの本から外す。`204`。

## 利用図書館（User Story 3）

### `GET /api/libraries`

```json
{
  "libraries": [
    {
      "id": "Tokyo_Setagaya:中央図書館",
      "name": "世田谷区立中央図書館",
      "pref": "東京都",
      "city": "世田谷区",
      "schedule": {
        "weekly": {"sun": {"open": "09:00", "close": "17:00"}, "mon": null, "tue": {"open": "09:00", "close": "19:00"}, "wed": {"open": "09:00", "close": "19:00"}, "thu": {"open": "09:00", "close": "19:00"}, "fri": {"open": "09:00", "close": "19:00"}, "sat": {"open": "09:00", "close": "17:00"}},
        "holiday": {"mode": "same"},
        "updatedOn": "2026-10-21"
      },
      "closures": [{"start": "2026-11-10", "end": "2026-11-14", "reason": "特別整理期間"}]
    }
  ]
}
```

- 登録順。`schedule` は未入力なら `null`。`weekly` は7曜日すべてのキーを持ち、休館の曜日は `null`。
- `holiday.mode`: `same` / `closed` / `hours`（`hours` なら `open`・`close` も持つ）。
- `closures` は開始日の順。`DELETE` ではこの配列の位置（0始まり）で指定する。

### `GET /api/library-search?pref=<都道府県>&city=<市区町村>&name=<部分一致>`

- カーリルで探す（001 の `lib find` と同じ）。`pref` は必須。
- `registered` は登録済みなら `true`。`credit` はカーリルへのリンク。

```json
{
  "libraries": [{"id": "Tokyo_Setagaya:中央図書館", "name": "世田谷区立中央図書館", "pref": "東京都", "city": "世田谷区", "address": "東京都世田谷区弦巻3-16-8", "registered": false}],
  "credit": {"label": "図書館情報: カーリル", "url": "https://calil.jp/"}
}
```

### `POST /api/libraries`

```json
{"id": "Tokyo_Setagaya:中央図書館"}
```

- カーリルで実在を確かめて登録する（001 の `lib add` と同じ）。`201` で登録した館を返す。登録済みなら `409`。

### `DELETE /api/libraries/{id}`

- `{id}` は `SystemID:LibKey` を URL エンコードしたもの。`204`。

### `PUT /api/libraries/{id}/schedule`

```json
{
  "weekly": {"sun": {"open": "09:00", "close": "17:00"}, "mon": null, "tue": {"open": "09:00", "close": "19:00"}, "wed": {"open": "09:00", "close": "19:00"}, "thu": {"open": "09:00", "close": "19:00"}, "fri": {"open": "09:00", "close": "19:00"}, "sat": {"open": "09:00", "close": "17:00"}},
  "holiday": {"mode": "same"}
}
```

- 開館スケジュールを丸ごと置き換える。7曜日すべてのキーが必要。`updatedOn` はサーバーが今日の日付にする。
- `200` で更新後の館（`GET /api/libraries` の1件と同じ形）を返す。

### `POST /api/libraries/{id}/closures`

```json
{"start": "2026-11-10", "end": "2026-11-14", "reason": "特別整理期間"}
```

- `reason` は省略できる。開始日が終了日より後なら `400`。同じ開始日・終了日・理由の期間があれば `409`。
- `201` で更新後の館を返す。

### `DELETE /api/libraries/{id}/closures/{index}`

- `{index}` は `closures` の位置（0始まり）。`200` で更新後の館を返す。

## 取り込み（FR-118）

### `POST /api/import`

- 本文: 001 の `data.json` の中身そのまま（[001 data-model](../../001-readable-libraries-today/data-model.md#永続化形式)）。大きさの上限は 1 MiB。
- クラウド上の読みかけの本と利用図書館がどちらも0件のときだけ受け付ける。それ以外は `409`。
- 形式が正しくなければ `400`。何も保存しない。
- `200` で取り込んだ件数を返す。

```json
{"books": 3, "libraries": 4, "closures": 2}
```

## 静的ファイル

- `/`・`/assets/*` などは、ビルド済みの画面を返す。本人確認は要らない（利用者データを含まないため）。
- `/` の `index.html` は `Cache-Control: no-cache`、`/assets/*`（ファイル名にハッシュ付き）は `Cache-Control: public, max-age=31536000, immutable`。
