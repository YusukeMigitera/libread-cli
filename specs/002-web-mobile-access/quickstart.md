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

Chrome か Firefox で `http://localhost:8080` を開き、次を確かめる。
Safari は `http://localhost` で `Secure` の Cookie を保存しないため、ローカルではログインできない（research W10）。
Safari でのログインは、HTTPS になる本番で確かめる（手順 5-1）。

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
| 2. 取り込み | — | 省略。001 の CLI の `data.json` は残っておらず、移したいデータもないため（T058 も該当なし）。取り込みの機能は手順 1 の自動テストで確かめている |
| 3. 図書館 | 2026-11-06 | 期待どおり。手順との違いはなかった |
| 4. 本 | 2026-11-06 | 期待どおり。手順との違いはなかった |
| 5. 今日 | 2026-11-06 | 期待どおり。手順との違いはなかった |
| 6. すべての端末からログアウト | 2026-11-06 | 期待どおり（Chrome の通常のウィンドウとシークレットウィンドウで確認） |

手順との違い:

- **Safari ではログインできなかった**: Google でアカウントを選んだあと、ログインできない。
  原因は、Safari（WebKit）が `http://localhost` を安全な接続として扱わず、`Secure` の Cookie（`libread_login`・`libread_session`）を保存しないことと考えられる。
  `libread_login` がないと、Google から戻ったときに state を照合できず、ログインを拒否する。
  Chrome・Firefox は localhost の `Secure` の Cookie を保存するので、この問題は起きない。
  本番は HTTPS なので起きないはずで、`Secure` は外さない。手順 3 の冒頭に、ローカルでは Chrome か Firefox を使うことを追記した。
  本番（HTTPS）では PC の Safari でログインできた（本番での確認結果）。

## 4. Cloud Run へのデプロイ

初回だけ、バケットと秘密情報を用意する（`<...>` は自分の値に置き換える）。

```sh
gcloud config set project <プロジェクト ID>
gcloud storage buckets create gs://<バケット名> --location=asia-northeast1 --uniform-bucket-level-access
printf '%s' '<カーリルのキー>'           | gcloud secrets create libread-calil-appkey --data-file=-
printf '%s' '<クライアントシークレット>' | gcloud secrets create libread-google-client-secret --data-file=-
openssl rand -base64 32 | tr -d '\n'     | gcloud secrets create libread-session-key --data-file=-
```

Cloud Run で動かすための専用のサービスアカウント `libread-run` を作り、バケットの `roles/storage.objectUser` と、3つの秘密情報の `roles/secretmanager.secretAccessor` だけを付ける。

```sh
gcloud iam service-accounts create libread-run --display-name="libread (Cloud Run)"
gcloud storage buckets add-iam-policy-binding gs://<バケット名> \
  --member=serviceAccount:libread-run@<プロジェクト ID>.iam.gserviceaccount.com --role=roles/storage.objectUser
for s in libread-calil-appkey libread-google-client-secret libread-session-key; do
  gcloud secrets add-iam-policy-binding "$s" \
    --member=serviceAccount:libread-run@<プロジェクト ID>.iam.gserviceaccount.com --role=roles/secretmanager.secretAccessor
done
```

- `--service-account` を指定しないと、Compute Engine の既定のサービスアカウントで動く。プロジェクトの編集者ロールが付いていることがあり、ほかのサービスとも共用なので使わない。
- バケット名は全世界で一意なので、`<プロジェクト ID>-libread` のようにプロジェクト ID を含める。個人情報は入れない。
- カーリルのアプリケーションキーに、呼び出し元の制限はない（2026-11-06 に確認、research W9）。キーを作り直したときも、ダッシュボードで制限の項目が増えていないかだけ確かめる。

デプロイ（2回目以降はこれだけ）:

```sh
gcloud run deploy libread --source . --region asia-northeast1 \
  --service-account libread-run@<プロジェクト ID>.iam.gserviceaccount.com \
  --min-instances 0 --max-instances 1 --allow-unauthenticated \
  --add-volume name=store,type=cloud-storage,bucket=<バケット名> \
  --add-volume-mount volume=store,mount-path=/mnt/store \
  --set-env-vars LIBREAD_DATA_DIR=/mnt/store/data,LIBREAD_CACHE_DIR=/mnt/store/cache,LIBREAD_STATIC_DIR=/app/static \
  --set-env-vars LIBREAD_OWNER_EMAIL=<本人の Google アカウント>,LIBREAD_GOOGLE_CLIENT_ID=<クライアント ID>,LIBREAD_PUBLIC_ORIGIN=https://<Cloud Run の URL> \
  --set-secrets LIBREAD_CALIL_APPKEY=libread-calil-appkey:latest,LIBREAD_GOOGLE_CLIENT_SECRET=libread-google-client-secret:latest,LIBREAD_SESSION_KEY=libread-session-key:latest
```

初回は Cloud Run の URL がまだ決まっていないので、次の順に進める。

1. `LIBREAD_PUBLIC_ORIGIN` を仮の値にしてデプロイする。終わると URL（`https://libread-…run.app`）が表示される。
2. 環境変数だけを正しい URL に更新する（ビルドし直さない。ほかの環境変数・秘密情報・ボリュームはそのまま残る）。
   控えているデプロイのコマンドの `LIBREAD_PUBLIC_ORIGIN` も、この URL に書き換えておく。

   ```sh
   gcloud run services update libread --region asia-northeast1 --update-env-vars LIBREAD_PUBLIC_ORIGIN=https://<Cloud Run の URL>
   ```

3. Google の OAuth クライアントの「承認済みのリダイレクト URI」に `https://<Cloud Run の URL>/auth/callback` を加える。

- 初回の `--source` でのデプロイでは、ビルドしたイメージを置く Artifact Registry のリポジトリ `cloud-run-source-deploy`（`asia-northeast1`）を作るかを尋ねられる。作ってよい。
  イメージはデプロイのたびにたまり、保存量が 0.5 GB を超えた分は料金がかかるので、古いイメージを消すクリーンアップポリシーを設定する。
- `--allow-unauthenticated` は、Cloud Run の前段の認証を使わないという意味。本人確認はアプリが行う（research W2）。
- `Dockerfile` は、Node で `web/` をビルドし、Go でサーバーをビルドして、両方を1つのイメージに入れる。
- デプロイの最中は、画面から保存しない。新旧のリビジョンが短い時間同時に動き、変更が失われることがある（research W3「デプロイ中の例外」）。

### 自動デプロイ（GitHub Actions）

手作業でのデプロイが動いたあと、`main` への push（PR のマージ）で自動でデプロイする（research W13）。
ワークフローは [`.github/workflows/deploy.yml`](../../.github/workflows/deploy.yml)。`make check` が通ったときだけ、上と同じ `gcloud run deploy` を実行する。
GitHub から Google Cloud へは Workload Identity 連携で認証し、サービスアカウントの鍵は作らない。

初回だけ、次を用意する。`<リポジトリの ID>` は GitHub のリポジトリの数値の ID
（`curl -s https://api.github.com/repos/YusukeMigitera/libread-cli | grep -m1 '"id"'`）。
名前ではなく ID で絞るので、リポジトリ名を変えても、同じ名前の別のリポジトリからは入れない。

```sh
# Workload Identity プールと、このリポジトリの main からのトークンだけを受け入れるプロバイダ
gcloud services enable iamcredentials.googleapis.com sts.googleapis.com
gcloud iam workload-identity-pools create github --location=global --display-name="GitHub Actions"
gcloud iam workload-identity-pools providers create-oidc libread-cli \
  --location=global --workload-identity-pool=github --display-name="libread-cli" \
  --issuer-uri=https://token.actions.githubusercontent.com \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository_id=assertion.repository_id,attribute.ref=assertion.ref" \
  --attribute-condition="assertion.repository_id=='<リポジトリの ID>' && assertion.ref=='refs/heads/main'"

# デプロイ用のサービスアカウントと、GitHub Actions からの利用
gcloud iam service-accounts create libread-deploy --display-name="libread (GitHub Actions deploy)"
gcloud iam service-accounts add-iam-policy-binding libread-deploy@<プロジェクト ID>.iam.gserviceaccount.com \
  --role=roles/iam.workloadIdentityUser \
  --member="principalSet://iam.googleapis.com/projects/<プロジェクト番号>/locations/global/workloadIdentityPools/github/attribute.repository_id/<リポジトリの ID>"
```

デプロイ用のサービスアカウントには、`gcloud run deploy --source` に要る権限だけを、できるだけ対象のリソースに限って付ける。
`<ソースのバケット>` は `--source` のソースを置くバケット（`gcloud storage ls` で `run-sources-<プロジェクト ID>-asia-northeast1`）、
`<ビルドのサービスアカウント>` は `gcloud builds get-default-service-account` の値。

```sh
DEPLOY=serviceAccount:libread-deploy@<プロジェクト ID>.iam.gserviceaccount.com
gcloud run services add-iam-policy-binding libread --region asia-northeast1 --member="$DEPLOY" --role=roles/run.admin
gcloud projects add-iam-policy-binding <プロジェクト ID> --member="$DEPLOY" --role=roles/cloudbuild.builds.editor
gcloud projects add-iam-policy-binding <プロジェクト ID> --member="$DEPLOY" --role=roles/serviceusage.serviceUsageConsumer
gcloud storage buckets add-iam-policy-binding gs://<ソースのバケット> --member="$DEPLOY" --role=roles/storage.objectAdmin
gcloud storage buckets add-iam-policy-binding gs://<ソースのバケット> --member="$DEPLOY" --role=roles/storage.legacyBucketReader
gcloud artifacts repositories add-iam-policy-binding cloud-run-source-deploy --location asia-northeast1 --member="$DEPLOY" --role=roles/artifactregistry.reader
for sa in libread-run@<プロジェクト ID>.iam.gserviceaccount.com <ビルドのサービスアカウント>; do
  gcloud iam service-accounts add-iam-policy-binding "$sa" --member="$DEPLOY" --role=roles/iam.serviceAccountUser
done
```

- Artifact Registry へイメージを push するのはビルドのサービスアカウントなので、デプロイ用には読み取り（`artifactregistry.reader`）だけを付ける。
- `run.admin` は、`--allow-unauthenticated` で公開の設定（IAM ポリシー）を書くのに要る。

GitHub のリポジトリ変数（Settings → Secrets and variables → Actions → Variables）に、次を登録する。秘密情報は GitHub に置かない（Secret Manager にある）。

| 変数 | 値 |
|---|---|
| `GCP_PROJECT_ID` | プロジェクト ID |
| `GCP_REGION` | `asia-northeast1` |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | `projects/<プロジェクト番号>/locations/global/workloadIdentityPools/github/providers/libread-cli` |
| `GCP_DEPLOY_SERVICE_ACCOUNT` | `libread-deploy@<プロジェクト ID>.iam.gserviceaccount.com` |
| `GCP_RUN_SERVICE_ACCOUNT` | `libread-run@<プロジェクト ID>.iam.gserviceaccount.com` |
| `LIBREAD_BUCKET` | バケット名 |
| `LIBREAD_OWNER_EMAIL` | 本人の Google アカウント |
| `LIBREAD_GOOGLE_CLIENT_ID` | OAuth クライアントの ID |
| `LIBREAD_PUBLIC_ORIGIN` | `https://<Cloud Run の URL>` |

- PR をマージすると、Actions の deploy が走る。終わるまで（数分）画面から保存しない（research W3「デプロイ中の例外」）。
- Actions の画面の「Run workflow」からも手で実行できる（`workflow_dispatch`）。

## 5. スマートフォンでの確認

1. スマートフォンのブラウザで `https://<Cloud Run の URL>` を開き、ログインする。ホーム画面に追加しておく。iPhone では Safari でログインできることを確かめる（ローカルの `http://localhost` では Safari でログインできないため、ここが最初の確認になる）。
2. 縦向きで、今日・本・図書館の3画面とも横スクロールが出ない（FR-115）。
3. 本人確認済みの端末で、ホーム画面から開いて結果が出るまで20秒以内（SC-101）。
4. 15分以上使わずに置いてから（インスタンスが停止した状態で）開き、30秒以内に結果が出る（SC-102）。
5. `http://<Cloud Run の URL>` を開くと `https://` に切り替わる（FR-119）。
6. 館内で、メモの書き換えが30秒以内に終わる（SC-106）。
7. 新しいリビジョンをデプロイしたあとも、データが残っている（SC-107）。

### 本番での確認結果（T057）

| 項目 | 実施日 | 結果 |
|---|---|---|
| デプロイ（手順 4） | 2026-11-06 | 期待どおり。`--set-env-vars` を2回指定しても両方が設定され、環境変数6つと秘密情報3つがそろっていた（`gcloud run services describe libread --region asia-northeast1 --format='yaml(spec.template.spec.containers[0].env)'` で確認） |
| 1. ログイン・ホーム画面への追加 | 2026-11-06 | 期待どおり（Android の Brave）。iPhone は手元にないため、iOS Safari では確かめていない |
| 2. 横スクロールが出ない（FR-115） | 2026-11-06 | 期待どおり（3画面とも） |
| 3. 結果が出るまで20秒以内（SC-101） | 2026-11-06 | 期待どおり（20秒以内） |
| 4. 停止状態から30秒以内（SC-102） | 2026-11-07 | 期待どおり（30秒以内） |
| 5. `http://` から `https://` へ（FR-119） | 2026-11-07 | 期待どおり |
| 6. 館内でのメモの書き換え（SC-106） | 2026-11-07 | 期待どおり（30秒以内） |
| 7. デプロイ後もデータが残る（SC-107） | 2026-11-07 | 期待どおり。同じ `gcloud run deploy` で新しいリビジョンをデプロイしたあとも、本・図書館・メモが残っていた |
| PC のブラウザの 360px 幅（FR-115） | 2026-11-07 | 期待どおり。表示は崩れなかった |
| PC の Safari でのログイン（HTTPS） | 2026-11-07 | 期待どおり。iOS Safari の代わりに、同じ WebKit の PC の Safari で本番にログインできた。ローカルで Safari がログインできなかった原因は、推定どおり `http://localhost` での `Secure` の Cookie と考えてよい |

## 6. 001 の CLI の廃止（FR-117）

手順 2〜5 が済み、手順 3-2 の取り込みを本番で済ませたあとに行う（取り込むデータがなかったため、取り込みは省略した。ローカルでの確認結果を参照）。

- `cmd/libread` と `internal/cli` を削除し、README を Web 版の使い方に書き換える。
- 削除後も `make check` が通る。
