# Data Model: スマートフォンのブラウザから使える libread（Web 化）

**Feature**: [spec.md](./spec.md) | **Research**: [research.md](./research.md) | **API**: [contracts/http-api.md](./contracts/http-api.md)

## 001 から引き継ぐもの

値型（`ISBN`・`LibraryID`・`TimeOfDay`・`Hours`・`Date`）、列挙型（`Availability`・`OpenStatus`）、
エンティティ（Book・ReadingBook・RegisteredLibrary・Schedule・ClosurePeriod・HoldingSnapshot）、
判定ロジック（`OpenStatusOf`・`AvailabilityOf`・`Readable`）、検索結果の集約（`Result`）、
`data.json` の永続化形式は、[001 の data-model.md](../001-readable-libraries-today/data-model.md) をそのまま使う。
変更はない。

- `data.json` の形式（`version: 1`）を変えないので、001 の CLI のデータをそのまま取り込める（FR-118）。
- 判定ロジックは Go の `internal/domain` だけに置く（Constitution 原則 IV）。

## 本機能で追加するもの

### OwnerIdentity（本人。設定値）

| フィールド | 型 | 備考 |
|---|---|---|
| Email | string | 本人の Google アカウントのメールアドレス。環境変数 `LIBREAD_OWNER_EMAIL` で設定する |

- 比較は大文字・小文字を区別しない。ID トークンの `email_verified` が `true` のときだけ一致とみなす（research W2）。
- 本人は1人だけ（FR-110）。複数のアドレスは設定できない。

### AuthState（本人確認の状態。`auth.json` に保存）

| フィールド | 型 | 備考 |
|---|---|---|
| Generation | `SessionGeneration`（正の整数） | セッション世代番号。全端末の確認の取り消し（FR-111）で1つ進める |

```json
{ "version": 1, "generation": 1 }
```

- ファイルがなければ `generation: 1` として扱う。
- 利用者データ（`data.json`）とは別のファイルにする。取り込み（FR-118）と本人確認の状態が混ざらないようにするため。

### Session（確認済み端末。spec の「確認済み端末」。サーバーには保存せず、署名付き Cookie に入れる）

| フィールド | 型 | 備考 |
|---|---|---|
| IssuedAt | time.Time | 本人を確認した日時 |
| ExpiresAt | time.Time | IssuedAt + 30日（FR-111） |
| Generation | `SessionGeneration` | 発行したときの AuthState.Generation |

- Cookie の値: `base64url(JSON) + "." + base64url(HMAC-SHA256(署名鍵, JSON))`。
- 有効と判定する条件（すべて満たすこと）:
  1. 署名が正しい（`hmac.Equal` で比較する）。
  2. 現在時刻 < ExpiresAt。
  3. Generation = AuthState.Generation。
- 状態遷移:

```text
（なし） --本人の Google アカウントでログイン--> 有効
有効 --30日経過--> 無効（期限切れ）
有効 --どこかの端末で「すべての端末の確認を取り消す」--> 無効（世代が古い）
有効 --この端末でログアウト--> （なし）（Cookie を消す）
```

### LoginAttempt（ログインの途中。短時間の Cookie）

| フィールド | 型 | 備考 |
|---|---|---|
| State | string | `crypto/rand` の32バイトを base64url にしたもの。Google から戻ったときに照合する |
| ReturnTo | `ReturnPath` | ログイン後に戻る画面（`/`・`/books`・`/libraries` のどれか。それ以外は `/`）。戻るときは `/#<ReturnTo>` へリダイレクトする |

- Cookie 名 `libread_login`、有効期限10分、`HttpOnly`・`Secure`・`SameSite=Lax`。コールバックで必ず消す。
- `ReturnTo` は決まった値の中からしか選べない。任意の URL へのリダイレクトには使えない。

### CheckView（検索結果の表示。spec の「検索結果の表示」。API の応答だけで、保存しない）

001 の `Result` を、画面がそのまま並べられる形にしたもの。
各館・各本の表示文言（`label`）はサーバーで作る（research W5）。形は [contracts/http-api.md](./contracts/http-api.md#get-apicheck) を参照。

| フィールド | 内容 |
|---|---|
| checkedAt | 判定の基準時刻（RFC 3339、`+09:00`）。画面はこれを常に表示し、30分を超えたら「古い情報」とする（FR-105） |
| readable[] / notReadable[] | 館ごとの結果。並び順は 001 FR-008 のとおりで、サーバーが決める |
| sources | 出典表示（カーリルの取得日時・本ごとのリンク、開館情報の最終更新日、注意書き） |

## 画面側の型（TypeScript）

API の応答の型は、contracts/http-api.md の各応答と1対1に対応させる。

- 状態を表す値は文字列リテラルの合併型にする。例: `type OpenStatus = "open" | "notYetOpen" | ...`。
  知らない値が来たら、検証関数で `"unknown"` に変換する（原則 III）。
- ID は区別できる型にする。例: `type ISBN = string & { readonly brand: "ISBN" }`。
  検証関数だけが作れるようにする。
- `any` は使わない。応答は `unknown` として受け、検証関数（`decodeCheckView(u: unknown): CheckView` など）で変換する。
  形が合わなければ例外を投げ、画面には「通信に失敗しました」と表示する（FR-116）。

## 保存先の配置（research W3）

```text
<マウント先>/                 # Cloud Storage のバケット（Cloud Run のボリューム）
├── data/
│   ├── data.json            # 利用者データ（001 と同じ形式）
│   └── auth.json            # AuthState
└── cache/
    ├── holdings.json        # 所蔵状況（001 と同じ）
    └── holidays-YYYY.json   # 祝日（001 と同じ）
```

- 場所は 001 と同じ環境変数 `LIBREAD_DATA_DIR`・`LIBREAD_CACHE_DIR` で指定する。
