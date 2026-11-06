# Quickstart: スマートフォンのブラウザから使える libread（Web 化）

**Feature**: [spec.md](./spec.md) | **API**: [contracts/http-api.md](./contracts/http-api.md) | **Screens**: [contracts/screens.md](./contracts/screens.md)

この機能が端から端まで動くことを確かめる手順。

## 前提

- Go 1.25 以上、Node.js 22 LTS
- カーリルのアプリケーションキー（001 と同じ）
- Google Cloud のプロジェクトと `gcloud` CLI（手順 4 だけ）
- Google の OAuth クライアント（種類「ウェブ アプリケーション」）。承認済みのリダイレクト URI に次の3つを登録する。
  承認済みの JavaScript 生成元は空でよい（認可コードの交換はサーバーが行うため）。
  - `http://localhost:8080/auth/callback`（手順 3。ビルドした画面を Go のサーバーで配信するとき）
  - `http://localhost:5173/auth/callback`（手順 3。Vite の開発サーバーで画面を開くとき）
  - `https://libread-<プロジェクト番号>.asia-northeast1.run.app/auth/callback`（手順 4。Cloud Run の URL は「サービス名-プロジェクト番号.リージョン.run.app」の形になる。
    最初のデプロイで `gcloud run deploy` が表示する URL と一致することを確かめる）
- OAuth 同意画面: User Type「外部」、公開ステータス「テスト」のままでよい。「テストユーザー」に本人の Google アカウントを追加する
  （追加しないとログインできない）。スコープは `openid` と `email` だけ。

## 1. 品質ゲート（ネットワーク不要）

```sh
make check
```

`make check` は次をまとめて実行する（Constitution「開発ワークフロー」）。

| 対象 | 内容 |
|---|---|
| サーバー | `gofmt -l`（Git 管理下の Go のファイル、出力なし）、`go vet`・`go tool staticcheck`・`go test`（いずれも `./cmd/... ./internal/...`。`./...` だと `web/node_modules` にある Go のパッケージまで対象になるため） |
| 画面（`web/`） | `npm ci`、`npm run format:check`（Prettier）、`npm run typecheck`（`tsc --noEmit`、strict）、`npm run lint`（ESLint）、`npm test`（Vitest） |

**期待結果**: すべて警告なしで成功する。テストは実ネットワークに接続しない。

## 2. 001 の受け入れシナリオとの一致（SC-103）

```sh
go test ./internal/server/ -run TestCheckMatchesCLIScenarios
```

**期待結果**: 001 の受け入れシナリオ（US1 シナリオ1〜5、Edge Cases）と同じデータ・同じ時刻で、
`GET /api/check` の `readable` の館の集合・順序・各 `label` が、001 の CLI の出力と一致する。

## 3. ローカルでの確認

```sh
export LIBREAD_CALIL_APPKEY=<カーリルのキー>
export LIBREAD_GOOGLE_CLIENT_ID=<クライアント ID>
export LIBREAD_GOOGLE_CLIENT_SECRET=<クライアントシークレット>
export LIBREAD_SESSION_KEY="$(openssl rand -base64 32)"
export LIBREAD_OWNER_EMAIL=<本人の Google アカウント>
export LIBREAD_PUBLIC_ORIGIN=http://localhost:8080
export LIBREAD_DATA_DIR="$(mktemp -d)"
export LIBREAD_CACHE_DIR="$(mktemp -d)"

(cd web && npm ci && npm run build)          # web/dist に画面を出力する
LIBREAD_STATIC_DIR=web/dist go run ./cmd/libread-server
```

画面を変えながら確かめるときは、サーバーを `LIBREAD_PUBLIC_ORIGIN=http://localhost:5173` で起動し、
別の端末で `cd web && npm run dev` を実行して `http://localhost:5173` を開く
（Vite の開発サーバーが `/api` と `/auth` を `localhost:8080` へ中継する）。
`LIBREAD_PUBLIC_ORIGIN` を 8080 番のままにすると、Google からの戻り先が 8080 番になり、書き込みの要求も `Origin` が一致せず `403` になる。

ブラウザで `http://localhost:8080` を開き、次を確かめる。

1. **本人確認**（FR-110・FR-111）
   - 本人の Google アカウントでログインできる。
   - 本人以外のアカウントでは「ログインできませんでした」と表示され、データは表示されない。
   - ログインせずに `curl -i http://localhost:8080/api/books` → `401`。`/api/books/9784003101018` や `/api/nonexistent` も同じ `401` と同じ本文（SC-104）。
2. **取り込み**（FR-118・SC-108）: 本の画面で、001 の CLI の `data.json`（Linux は `~/.config/libread/data.json`）を取り込む。
   件数が 001 の `libread book list`・`libread lib list` と一致する。もう一度取り込もうとすると、取り込みの欄が表示されない。
3. **図書館**（US3）: 図書館を探して追加し、開館時間と今日を含む臨時休館期間を入力する → 今日の画面でその館が「休館」になる。期間を削除すると戻る。
4. **本**（US2）: 本を探して登録し、メモを書き換え、外す。外すときに確認が出る。
5. **今日**（US1）: 「所蔵状況を取得しています…」のあと、結果がまとめて表示される。基準時刻と出典が表示される。
   「更新」で基準時刻が変わる。
6. **すべての端末からログアウト**: 2つのブラウザでログインしておき、片方で実行すると、もう片方も次の操作でログイン画面になる。

### ローカルでの確認結果（T056）

手元の PC で、実際の Google・カーリル・NDL サーチに対して手順 3 を進めた結果。手順と食い違う点があれば、ここに記録する。

| 項目 | 実施日 | 結果 |
|---|---|---|
| 1. 本人確認 | 2026-11-06 | 期待どおり。手順との違いはなかった |
| 2. 取り込み | — | 未実施 |
| 3. 図書館 | — | 未実施 |
| 4. 本 | — | 未実施 |
| 5. 今日 | — | 未実施 |
| 6. すべての端末からログアウト | — | 未実施 |

## 4. Cloud Run へのデプロイ

初回だけ、バケットと秘密情報を用意する（`<...>` は自分の値に置き換える）。

```sh
gcloud config set project <プロジェクト ID>
gcloud storage buckets create gs://<バケット名> --location=asia-northeast1 --uniform-bucket-level-access
printf '%s' '<カーリルのキー>'           | gcloud secrets create libread-calil-appkey --data-file=-
printf '%s' '<クライアントシークレット>' | gcloud secrets create libread-google-client-secret --data-file=-
openssl rand -base64 32 | tr -d '\n'     | gcloud secrets create libread-session-key --data-file=-
```

- Cloud Run のサービスアカウントに、バケットの `roles/storage.objectUser` と、3つの秘密情報の `roles/secretmanager.secretAccessor` を付ける。
- カーリルの開発者ダッシュボードで、アプリケーションキーに呼び出し元の制限がないかを確かめる（research W9）。

デプロイ（2回目以降はこれだけ）:

```sh
gcloud run deploy libread --source . --region asia-northeast1 \
  --min-instances 0 --max-instances 1 --allow-unauthenticated \
  --add-volume name=store,type=cloud-storage,bucket=<バケット名> \
  --add-volume-mount volume=store,mount-path=/mnt/store \
  --set-env-vars LIBREAD_DATA_DIR=/mnt/store/data,LIBREAD_CACHE_DIR=/mnt/store/cache,LIBREAD_STATIC_DIR=/app/static \
  --set-env-vars LIBREAD_OWNER_EMAIL=<本人の Google アカウント>,LIBREAD_GOOGLE_CLIENT_ID=<クライアント ID>,LIBREAD_PUBLIC_ORIGIN=https://<Cloud Run の URL> \
  --set-secrets LIBREAD_CALIL_APPKEY=libread-calil-appkey:latest,LIBREAD_GOOGLE_CLIENT_SECRET=libread-google-client-secret:latest,LIBREAD_SESSION_KEY=libread-session-key:latest
```

- `--allow-unauthenticated` は、Cloud Run の前段の認証を使わないという意味。本人確認はアプリが行う（research W2）。
- `Dockerfile` は、Node で `web/` をビルドし、Go でサーバーをビルドして、両方を1つのイメージに入れる。
- デプロイの最中は、画面から保存しない。新旧のリビジョンが短い時間同時に動き、変更が失われることがある（research W3「デプロイ中の例外」）。

## 5. スマートフォンでの確認

1. スマートフォンのブラウザで `https://<Cloud Run の URL>` を開き、ログインする。ホーム画面に追加しておく。
2. 縦向きで、今日・本・図書館の3画面とも横スクロールが出ない（FR-115）。
3. 本人確認済みの端末で、ホーム画面から開いて結果が出るまで20秒以内（SC-101）。
4. 15分以上使わずに置いてから（インスタンスが停止した状態で）開き、30秒以内に結果が出る（SC-102）。
5. `http://<Cloud Run の URL>` を開くと `https://` に切り替わる（FR-119）。
6. 館内で、メモの書き換えが30秒以内に終わる（SC-106）。
7. 新しいリビジョンをデプロイしたあとも、データが残っている（SC-107）。

## 6. 001 の CLI の廃止（FR-117）

手順 2〜5 が済み、手順 3-2 の取り込みを本番で済ませたあとに行う。

- `cmd/libread` と `internal/cli` を削除し、README を Web 版の使い方に書き換える。
- 削除後も `make check` が通る。
