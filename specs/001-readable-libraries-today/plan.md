# Implementation Plan: 今日読める図書館の検索

**Branch**: `001-readable-libraries-today` | **Date**: 2026-10-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-readable-libraries-today/spec.md`

## Summary

libread は、登録した読みかけの本すべて（または指定した1冊）について、利用図書館のうち
「今日開館していて館内で手に取れる館」を館ごとにまとめてターミナルに表示する、Go 製の個人用 CLI。

- 所蔵状況はカーリル 図書館API で全国の公共図書館から取得し、30分キャッシュする。
- 書誌検索は国立国会図書館サーチ、祝日判定は holidays-jp を使う。
- 開館スケジュールと臨時休館期間は利用者が入力し、ローカルの JSON ファイルに保存する。
- 判定ロジックは外部に依存しない純粋関数にまとめ、現在時刻を引数で受け取ってテストする。

## Technical Context

**Language/Version**: Go 1.25

**Primary Dependencies**:
- 実行時は標準ライブラリのみ: `net/http`、`encoding/json`、`encoding/xml`、`flag`、`time/tzdata`
- 開発用: `honnef.co/go/tools/cmd/staticcheck`（`go.mod` の `tool` ディレクティブ）

**Storage**: ローカルの JSON ファイル
- 利用者データ: `os.UserConfigDir()/libread/data.json`
- キャッシュ: `os.UserCacheDir()/libread/`

**Testing**: `go test`
- 外部 API は `httptest.Server` と、`testdata/` に記録した実レスポンスで差し替える。
- 現在時刻は関数として注入する。

**Target Platform**: macOS / Linux のターミナル

**Project Type**: CLI（単一バイナリ）

**Performance Goals**: 利用図書館10館・本5冊の検索が10秒以内（SC-002）
- カーリルへの問い合わせ全体に8秒の上限を設ける。

**Constraints**:
- カーリル: 1,000ブックリクエスト/時、1セッション100ブックリクエストまで、ポーリング間隔2秒以上、カーリルへのリンク表示が必須。
- アプリケーションキーは環境変数から読む。
- 利用者のメモは外部へ送らない。

**Scale/Scope**: 利用者1人。読みかけの本・利用図書館とも数十件まで。サブコマンドは14個。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原則 / 制約 | 判定 | 根拠 |
|---|---|---|
| I. シンプルさ優先 | PASS | 実行時の依存は標準ライブラリのみ。DB・CLI フレームワーク・汎用リポジトリ層は導入しない。パッケージは外部境界ごとに1つ |
| II. テストファースト | PASS | 判定ロジックは純粋関数。外部 API は記録済みレスポンスでテストし、実ネットワークに依存しない。時刻は注入する |
| III. 型安全 | PASS | `ISBN`、`LibraryID`、`TimeOfDay`、`Availability`、`OpenStatus` を専用型にする。カーリルの未知の値は `Unknown` に変換する。外部 JSON は境界で型付きの構造体へ変換し、`any` は使わない |
| IV. 情報の正確性と鮮度 | PASS | 取得元・取得日時・入力日を併記する。30分超・取得失敗・タイムアウト・祝日判定不可の場合は「読める」に含めない |
| 技術的制約（規約・レート制限・秘密情報） | PASS | カーリル・NDL サーチ・holidays-jp の利用規約を確認し、すべて利用可と research R10 に記入済み（2026-10-22、2026-11-04 に確認し直し）。カーリルは公開されたアプリケーションでの利用に限るので、リポジトリを公開した（2026-11-04）。カーリルはリンク表示、NDL は出典表示が必須で、README と CLI に表示する。NDL の CC BY のクレジット表示など、R10「残っている対応」の項目は未対応。レート制限はコマンドごとの上限とキャッシュで守り（research R9）、テストで確かめる。キーは環境変数から読み、記録済みレスポンスからは除去する |
| 品質ゲート | PASS | `gofmt` / `go vet` / `go tool staticcheck` / `go test` を quickstart 手順 1 に定義 |

**Post-design re-check (Phase 1 後)**: PASS。
data-model・contracts で新たな依存や抽象化は増えていない。
`LIBREAD_DATA_DIR` と `LIBREAD_CACHE_DIR` は、テストと検証で実データを汚さないために必要な最小限の設定項目。

## Project Structure

### Documentation (this feature)

```text
specs/001-readable-libraries-today/
├── plan.md              # This file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── cli.md           # Phase 1: CLI のコマンド・出力契約
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
go.mod
cmd/libread/
└── main.go              # 環境変数・時刻・HTTP クライアントを組み立てて cli.Run を呼ぶ
internal/
├── domain/              # 値型・列挙型・判定ロジック（OpenStatusOf, AvailabilityOf, Readable）・結果の並べ替え
├── calil/               # カーリル API クライアント（図書館検索・所蔵チェック・ポーリング）と値の変換
│   └── testdata/
├── ndl/                 # 国立国会図書館サーチ OpenSearch クライアント（書名・ISBN 検索）
│   └── testdata/
├── holiday/             # holidays-jp クライアント（年単位のキャッシュ）
│   └── testdata/
├── store/               # data.json とキャッシュの読み書き（アトミックな書き込み）
└── cli/                 # サブコマンドの解析、check の組み立て、出力の整形
    └── testdata/        # check の出力のゴールデンファイル
```

テストは Go の慣習に従い、各パッケージ内の `_test.go` に置く。

**Structure Decision**: 単一の Go モジュールで作る CLI。

- `internal/` は外部境界（calil・ndl・holiday・store）とドメイン（domain）、利用者との接点（cli）に分ける。
- domain は他の internal パッケージに依存しない。cli だけが全体を組み立てる。
- 判定ロジックは domain に集め、外部境界から切り離してテストできるようにする。

## Complexity Tracking

憲章違反はないため、正当化が必要な項目はない。
