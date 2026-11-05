# Implementation Plan: スマートフォンのブラウザから使える libread（Web 化）

**Branch**: `002-web-mobile-access` | **Date**: 2026-11-05 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-web-mobile-access/spec.md`

## Summary

001 の CLI と同じ判定・同じ情報を、スマートフォンのブラウザから確認・操作できる Web サービスにする。

- Go のサーバーが、001 の `internal/domain`・`calil`・`ndl`・`holiday`・`store` をそのまま使い、JSON の HTTP API を提供する。
  「読める」の判定と表示の文言はサーバーだけで作る（Constitution 原則 IV、research W5）。
- 画面は TypeScript ＋ React ＋ Vite の1ページのアプリ。今日・本・図書館の3画面で、API の結果を並べて表示する。
- Cloud Run（東京、最小0・最大1インスタンス）で、サーバーと画面を1つのコンテナから同一オリジンで配信する。
  データは Cloud Storage のバケットをボリュームとしてマウントし、001 と同じ JSON ファイルで保存する（research W1・W3）。
- 本人確認は、サーバーが Google の OpenID Connect を標準ライブラリで実装し、本人のアカウント1つだけを許可する。
  確認した端末には30日間有効な署名付き Cookie を発行し、世代番号で全端末を一括で取り消せる（research W2）。
- 001 の `data.json` を一度だけ取り込めるようにし、取り込み後に CLI を廃止する（FR-117・FR-118）。

## Technical Context

**Language/Version**:
- サーバー: Go 1.25（001 と同じ）
- 画面: TypeScript 5（strict）、Node.js 22 LTS（ビルドとテストのみ）

**Primary Dependencies**:
- サーバー: 実行時は標準ライブラリのみ（`net/http`、`crypto/hmac`、`crypto/rand`、`encoding/json`、`time/tzdata` など）。開発用は 001 と同じ `staticcheck`。
- 画面（実行時）: `react`、`react-dom`（19）
- 画面（開発時）: `vite`、`@vitejs/plugin-react`、`typescript`、`eslint`、`typescript-eslint`、`eslint-plugin-react-hooks`、`prettier`、
  `vitest`、`jsdom`、`@testing-library/react`、`@testing-library/user-event`（research W6）

**Storage**: 001 と同じ JSON ファイル（`data.json`、キャッシュ）＋ `auth.json`（セッション世代番号）。
本番は Cloud Storage のバケットを Cloud Run のボリュームとしてマウントした場所に置く（research W3）。

**Testing**:
- サーバー: `go test`。API は `httptest` で、外部サービスは 001 の記録済みレスポンスで差し替える。Google のトークンエンドポイントも `httptest.Server` で差し替える。時刻は注入する。
- 画面: Vitest ＋ Testing Library（jsdom）。`fetch` を差し替え、contracts の例と同じ形の応答を返す。時刻は偽のタイマーで固定する（research W7）。

**Target Platform**:
- サーバー: Cloud Run（Linux コンテナ、`asia-northeast1`）
- 画面: スマートフォンの標準ブラウザ（iOS Safari・Android Chrome の最新版）と PC のブラウザ

**Project Type**: Web アプリケーション（Go の API サーバー ＋ 1ページの画面）

**Performance Goals**:
- 利用図書館10館・本5冊の検索が、画面を開いてから10秒以内。停止状態からの起動を含めて30秒以内（SC-102）。
- カーリルへの問い合わせ全体の上限は 001 と同じ8秒（001 R7）。

**Constraints**:
- 001 の外部サービスの制約（カーリルのレート制限・リンク表示、NDL の出典表示、User-Agent）をすべて引き継ぐ。各サービスへの同時リクエストは1本まで（research W9）。
- HTTPS のみ（Cloud Run が提供）。HSTS を付ける（research W10）。
- 秘密情報（カーリルのキー、Google のクライアントシークレット、セッションの署名鍵）は Secret Manager から環境変数で渡す。画面には渡さない。
- 運用費は無料枠〜月数百円（spec Assumptions）。

**Scale/Scope**: 利用者1人。読みかけの本・利用図書館とも数十件まで。API は20本弱、画面は3つ（＋ログイン）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution 1.1.0 に照らして確認する。

| 原則 / 制約 | 判定 | 根拠 |
|---|---|---|
| I. シンプルさ優先 | PASS | サーバーの実行時の依存は標準ライブラリのみ。OAuth・JWT・クラウド SDK のライブラリは使わない（W2・W3）。画面の実行時の依存は React だけで、ルーター・状態管理・スキーマ検証・CSS のライブラリは入れない（W6）。データベースは使わず 001 の保存形式を流用する。逐次送信やジョブの仕組みは作らない（W4） |
| II. テストファースト | PASS | サーバー・画面とも Red-Green-Refactor で進める（tasks で各実装の前にテストを置く）。画面は利用者から見える振る舞いを、contracts/screens.md の項目ごとにテストする。外部サービスと Google は記録済み・定義済みの応答で差し替え、実ネットワークに依存しない。時刻はサーバーは注入、画面は偽のタイマー |
| III. 型安全 | PASS | Go は 001 の専用型を使い続ける。新しい概念（`SessionGeneration`、`ReturnPath`）も専用型にする。TypeScript は strict・`any` 禁止、応答は `unknown` から検証関数で変換し、ID はブランド型、状態は文字列リテラル型（data-model）。API の形は contracts/http-api.md に定義し、Go のテスト（応答の JSON）と画面のテスト（検証関数）の両方で確かめる |
| IV. 情報の正確性と鮮度 | PASS | 判定・並び順・文言はサーバーだけで作り、画面は表示するだけ（W5）。画面は基準時刻を常に表示し、30分を超えたら「古い情報」と示す（screens.md）。取得失敗・30分超は 001 と同じく「読める」に含めない。取得中は途中の結果を出さない |
| 技術的制約: 言語 | PASS | サーバー（判定・外部問い合わせ・保存）は Go、ブラウザの画面だけ TypeScript（React、Vite） |
| 技術的制約: 整形 | PASS | Go は `gofmt`、TypeScript は Prettier（`npm run format:check`） |
| 技術的制約: 外部データソース | PASS | 001 R9・R10 を引き継ぎ、常駐による変化は W9 で対処（サービスごとに同時1本、キャッシュ）。外部への問い合わせはサーバーからだけ行う。カーリルの1セッション100件の上限（issue #7）は本機能でも未対応のまま（W9、想定規模では当たらない） |
| 技術的制約: 秘密情報 | PASS | Secret Manager → 環境変数。画面のビルド成果物には入れない（画面は秘密情報を読む手段を持たない）。リポジトリに置かない |
| 技術的制約: クラウドでの実行 | PASS | `/api/` はすべて本人確認が必要で、確認できない相手には一律の `401` を返し、データの有無が分からない（contracts）。HTTPS のみ・HSTS・`Secure` Cookie |
| 技術的制約: 個人情報 | PASS | メモは外部に送らない（FR-114）。ログにメモ・検索語・Cookie・トークン・メールアドレスを書かない（W11） |
| 品質ゲート | PASS | `make check` でサーバーと画面のゲートをまとめて実行する（quickstart 手順 1） |

**Post-design re-check (Phase 1 後)**: PASS。

- data-model・contracts で増えた抽象化はない。`internal/store` は 001 のファイルの読み書きのまま使い、ストレージの差し替えのためのインターフェースは作らない（本番もローカルもファイルシステム）。
- 001 の `internal/cli` にある check の組み立て（`loadHoldings` など）と文言の関数は、CLI と Web の2か所から使うあいだだけ共有し、CLI の削除後は Web だけが使う。
  共有のために新しいパッケージ `internal/checkrun`（組み立て）と `internal/present`（文言）へ移すが、これは移動であって抽象化の追加ではない。
- 新しく増える設定項目（環境変数）は、本人確認とクラウドでの実行に必要な最小限（`LIBREAD_GOOGLE_CLIENT_ID`・`LIBREAD_GOOGLE_CLIENT_SECRET`・`LIBREAD_SESSION_KEY`・`LIBREAD_OWNER_EMAIL`・`LIBREAD_PUBLIC_ORIGIN`・`LIBREAD_STATIC_DIR`）。

## Project Structure

### Documentation (this feature)

```text
specs/002-web-mobile-access/
├── plan.md              # This file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   ├── http-api.md      # Phase 1: サーバーと画面のあいだの API
│   └── screens.md       # Phase 1: 画面の表示と振る舞い
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
go.mod
Makefile                 # check: サーバーと画面の品質ゲートをまとめて実行する
Dockerfile               # web をビルド → Go をビルド → 1つのイメージにまとめる
cmd/
├── libread-server/
│   └── main.go          # 環境変数・時刻・HTTP クライアントを組み立てて server を起動する
└── libread/             # 001 の CLI。取り込みが済んだら削除する（FR-117）
internal/
├── domain/              # 001 のまま。判定ロジックはここだけ
├── calil/               # 001 のまま
├── ndl/                 # 001 のまま
├── holiday/             # 001 のまま
├── store/               # 001 のまま ＋ data.json をバイト列から読む関数（取り込み用）、auth.json の読み書き
├── checkrun/            # 新規: 所蔵状況の取得（キャッシュ込み）と判定の組み立て。001 の cli/check.go から移す
├── present/             # 新規: 開館状態・所蔵状況・出典の表示文言。001 の cli/format.go から移す
├── auth/                # 新規: Google OIDC のログイン、セッション Cookie の発行と検証、世代番号
├── server/              # 新規: HTTP のルーティング、本人確認・Origin の確認、各 API、静的ファイルの配信、ログ
└── cli/                 # 001 の CLI。取り込みが済んだら削除する（FR-117）
web/
├── package.json         # scripts: dev / build / typecheck / lint / format:check / test
├── package-lock.json
├── tsconfig.json        # strict
├── vite.config.ts       # 開発時に /api・/auth を localhost:8080 へ中継する
├── eslint.config.js
├── index.html
└── src/
    ├── main.tsx
    ├── App.tsx          # 本人確認の状態、ハッシュでの画面の切り替え、下部のタブ
    ├── api/             # fetch のラッパー、応答の型と検証関数（contracts/http-api.md と1対1）
    ├── screens/         # Login / Today / Books / Libraries
    ├── components/      # 確認ダイアログ、エラーと再試行、出典表示など
    ├── styles.css
    └── **/*.test.tsx    # 各ファイルの隣に置く
```

テストは、Go は各パッケージ内の `_test.go`、画面は各ファイルの隣の `*.test.ts(x)` に置く。

**Structure Decision**: 1つのリポジトリに、Go のモジュール（リポジトリ直下）と画面（`web/`）を置く。

- Go の依存の向きは 001 と同じ: `domain` は他に依存しない。`checkrun` は `domain`・`calil`・`holiday`・`store` を使う。
  `present` は `domain` だけを使う。`server` が全体を組み立てる。
- 画面は API だけに依存し、Go のコードには依存しない。ビルドした `web/dist` は、コンテナの中でサーバーが `LIBREAD_STATIC_DIR` から配信する。
  `go:embed` は使わない（`web/dist` がなくても `go test ./...` が通るようにするため）。
- 001 の CLI は、取り込み（FR-118）を本番で済ませるまで残し、そのあと削除する（quickstart 手順 6）。
  001 の check のテストのうち受け入れシナリオに当たるものは、削除の前に `internal/server` の API のテストへ移す（SC-103）。

## Complexity Tracking

憲章違反はないため、正当化が必要な項目はない。

参考として、単純な案を採らなかった判断を記録する（Constitution 原則 I）。

| 判断 | 理由 | 採らなかった単純な案 |
|---|---|---|
| 本人確認をアプリで実装する（W2） | 30日の省略と全端末の取り消し（FR-111）を仕様どおりに制御し、テストで確かめるため | IAP に任せる（アプリの実装は不要だが、FR-111 を満たせず、ローカルで再現できない） |
| 最大インスタンス数 1 に固定する（W1・W3） | プロセス内のミューテックスだけで書き込みの競合を防ぐため。デプロイで新旧のリビジョンが同時に動く短い時間は例外とし、その間は保存しない運用にする（W3） | 複数インスタンスを許して楽観ロックを実装する（コードが増える） |
