# libread

本を借りずに図書館の館内で読む人のための、個人用の Web アプリです。
登録した読みかけの本について、近隣の図書館のうち「今日開館していて、その本を館内で手に取れる館」を、スマートフォンのブラウザで確かめられます。

- 利用者は本人1人だけです。Google アカウントでログインし、登録したアカウント以外はデータを見られません。
- サーバーは Google Cloud の Cloud Run で動かし、データは Cloud Storage のバケットに保存します。

仕様は [specs/002-web-mobile-access/](specs/002-web-mobile-access/) にあります。
以前のコマンドラインツール版（[specs/001-readable-libraries-today/](specs/001-readable-libraries-today/)）は廃止しました（spec 002 FR-117）。

## 使い方（スマートフォン）

`https://<Cloud Run の URL>` を開き、「Google でログイン」からログインします。ホーム画面に追加しておくと、アプリのように開けます。
ログインは30日間保たれます。画面は下の3つです。

| 画面 | できること |
|---|---|
| 今日（`#/`） | 読みかけの本すべてについて、今日読める図書館と読めない図書館（理由つき）を表示する。「更新」で取得し直す。本が2冊以上なら1冊に絞り込める |
| 本（`#/books`） | 書名・ISBN（著者・出版社で絞り込み可）で本を探して登録する。読んだ位置などのメモを書き換える。読みかけの本から外す |
| 図書館（`#/libraries`） | 都道府県・市区町村・館名で図書館を探して登録する。曜日ごとの開館時間と祝日の扱い、臨時休館期間を入力する。下部の設定から、ログアウトや「すべての端末からログアウト」ができる |

はじめて使うときは、図書館の画面で利用する図書館を登録して開館時間を入力し、本の画面で読みかけの本を登録します。

- 開館時間を入力していない館は「開館情報未入力」と表示し、「読める」には含めません。開館情報は利用者が入力したものなので、図書館の公式情報と照らし合わせて、ときどき更新してください。
- 臨時休館や特別整理期間は、図書館のお知らせを見て入力します。入力した期間だけが反映されます。
- 所蔵状況は30分間キャッシュします。30分以内に開き直すとカーリルに問い合わせず、前回の取得結果を表示します。30分を過ぎた結果には「古い情報」と表示します。
- 取得できなかった所蔵状況や、30分より古い所蔵状況は「不明」と表示し、「読める」には含めません。
- 国立国会図書館サーチは応答に時間がかかることがあります。本を探せなかったときは、少し待ってから探し直してください。
- 本と図書館がどちらも0件のときだけ、本の画面に 001 の CLI の `data.json` を取り込む欄が出ます（FR-118）。

## デプロイ

手順は [quickstart.md 手順 4](specs/002-web-mobile-access/quickstart.md#4-cloud-run-へのデプロイ) にあります。概要は次のとおりです。

1. Cloud Storage のバケット、Secret Manager の秘密情報（カーリルのキー、Google のクライアントシークレット、セッションの鍵）、Cloud Run 専用のサービスアカウントを用意する。
2. Google Cloud で OAuth クライアント（種類「ウェブ アプリケーション」）を作り、承認済みのリダイレクト URI に `https://<Cloud Run の URL>/auth/callback` を加える。
3. リポジトリ直下で `gcloud run deploy libread --source . ...` を実行する。`Dockerfile` が画面とサーバーをビルドして1つのイメージにする。

デプロイの最中は画面から保存しないでください。新旧のリビジョンが短い時間同時に動き、変更が失われることがあります（[research.md W3](specs/002-web-mobile-access/research.md)）。

## データの保存場所

| 内容 | 場所 |
|---|---|
| 読みかけの本・メモ・利用図書館（`data.json`）、ログインの状態（`auth.json`） | Cloud Storage のバケットの `data/`（`LIBREAD_DATA_DIR`） |
| 所蔵状況・祝日のキャッシュ | Cloud Storage のバケットの `cache/`（`LIBREAD_CACHE_DIR`） |

バケットは Cloud Run のボリュームとして `/mnt/store` にマウントします。バックアップは取っていません。

外部サービスに送るのは、ISBN、図書館システムID、検索語、年だけです。メモはどこにも送りません。

## 環境変数

サーバー（`cmd/libread-server`）は次の環境変数で設定します。`LIBREAD_CALIL_APPKEY` 以外は必須です。

| 環境変数 | 内容 |
|---|---|
| `PORT` | 待ち受けるポート（既定 `8080`。Cloud Run が設定する） |
| `LIBREAD_DATA_DIR` | 利用者データとログインの状態を置くディレクトリ |
| `LIBREAD_CACHE_DIR` | キャッシュを置くディレクトリ |
| `LIBREAD_STATIC_DIR` | ビルドした画面（`web/dist`）のディレクトリ |
| `LIBREAD_PUBLIC_ORIGIN` | 公開する URL のオリジン（例 `https://libread-xxxx.a.run.app`。末尾に `/` を付けない） |
| `LIBREAD_OWNER_EMAIL` | ログインを許す Google アカウント |
| `LIBREAD_GOOGLE_CLIENT_ID` | OAuth クライアントの ID |
| `LIBREAD_GOOGLE_CLIENT_SECRET` | OAuth クライアントのシークレット（秘密情報） |
| `LIBREAD_SESSION_KEY` | セッション Cookie の署名の鍵。32バイト以上を base64 にした値（例 `openssl rand -base64 32`。秘密情報） |
| `LIBREAD_CALIL_APPKEY` | カーリルのアプリケーションキー（秘密情報）。[カーリルの開発者ダッシュボード](https://calil.jp/api/dashboard/)で取得する。ないと、カーリルを使う API は `503` を返す |

本番では、秘密情報は Secret Manager に置き、`gcloud run deploy --set-secrets` で渡します。

## 開発

必要なもの: Go 1.25 以上、Node.js 22.12 以上。

```sh
make check   # Go（gofmt / go vet / staticcheck / go test）と画面（Prettier / tsc / ESLint / Vitest / ビルド）の品質ゲート
```

- テストは記録済みの応答だけを使い、実際のネットワークには接続しません。
- 手元で動かすときは、環境変数を設定してから画面をビルドし、サーバーを起動します（[quickstart.md 手順 3](specs/002-web-mobile-access/quickstart.md#3-ローカルでの確認)）。

  ```sh
  (cd web && npm ci && npm run build)
  LIBREAD_STATIC_DIR=web/dist go run ./cmd/libread-server
  ```

- 画面を変えながら確かめるときは、サーバーを `LIBREAD_PUBLIC_ORIGIN=http://localhost:5173` で起動し、別の端末で `cd web && npm run dev` を実行して `http://localhost:5173` を開きます。
  Vite の開発サーバーが `/api` と `/auth` をサーバーへ中継します。
- ローカル（`http://localhost`）では Chrome か Firefox を使ってください。Safari は `http://localhost` で `Secure` の Cookie を保存しないため、ログインできません。
- 開発中は、リポジトリ直下の `.env` に環境変数を書いておき、`set -a; . ./.env; set +a` で読み込めます。`.env` は `.gitignore` の対象なのでコミットされません。

## 利用しているサービス

libread は次の外部サービスのデータを利用しています。

| サービス | 用途 |
|---|---|
| [国立国会図書館サーチ](https://ndlsearch.ndl.go.jp/) | 書名・ISBN による本の検索（書誌情報） |
| [カーリル](https://calil.jp/) | 図書館の検索と、所蔵・貸出状況の取得 |
| [holidays-jp](https://holidays-jp.github.io/) | 祝日の判定 |

- 書誌情報の検索には、**国立国会図書館サーチ API** を利用しています。
  書誌情報は、国立国会図書館サーチ API から取得した「国立国会図書館蔵書」（国立国会図書館が運営）のデータです。
  ライセンスは [クリエイティブ・コモンズ 表示 4.0 国際](https://creativecommons.org/licenses/by/4.0/legalcode.ja) です。
- 図書館の所蔵・貸出状況は、カーリルの API から取得しています。
  検索結果には、本ごとにカーリルのページへのリンクを表示します。
- 祝日のデータ（holidays-jp）は、Google カレンダーの祝日情報から自動生成されたものです。
  公的機関の一次データではありません。
