# libread

本を借りずに図書館の館内で読む人のための、個人用のコマンドラインツールです。
登録した読みかけの本について、近隣の図書館のうち「今日開館していて、その本を館内で手に取れる館」を一覧で表示します。

仕様は [specs/001-readable-libraries-today/](specs/001-readable-libraries-today/) にあります。

## 必要なもの

- Go 1.25 以上
- カーリルのアプリケーションキー
  - [カーリルの開発者ダッシュボード](https://calil.jp/api/dashboard/)で取得し、環境変数 `LIBREAD_CALIL_APPKEY` に設定します。
  - `check`（引数なしの `libread`）、`lib find`、`lib add` で使います。

## インストール

リポジトリを取得し、リポジトリ直下で次を実行します。`$(go env GOPATH)/bin` に `libread` が入ります。

```sh
go install ./cmd/libread
```

## はじめに: 図書館と本を登録する

1. 利用する図書館を探して登録します。都道府県の指定は必須です。

   ```sh
   libread lib find --pref 東京都 --city 世田谷区 --name 梅丘
   libread lib add Tokyo_Setagaya:梅丘図書館
   ```

2. 開館スケジュールを入力します。曜日ごとに `HH:MM-HH:MM` または `closed` を指定します。
   祝日の扱いは `--holiday same|closed|HH:MM-HH:MM` で指定し、既定は `same`（曜日どおり）です。
   入力していない館は「開館情報未入力」と表示され、「読める」には含まれません。

   ```sh
   libread lib hours 梅丘 --sun 09:00-17:00 --mon closed --tue 09:00-19:00 --wed 09:00-19:00 \
     --thu 09:00-19:00 --fri 09:00-19:00 --sat 09:00-17:00 --holiday same
   ```

   臨時休館や特別整理期間は、図書館のお知らせを見て入力します。

   ```sh
   libread lib closure add 梅丘 2026-11-10 2026-11-14 --reason 特別整理期間
   ```

3. 読みかけの本を探して登録します。読んだ位置などのメモも付けられます。
   書名だけでは候補が多すぎることがあるので、`--author` と `--publisher` で絞り込むと見つけやすくなります。

   ```sh
   libread book find 吾輩は猫である --author 夏目漱石 --publisher 岩波書店
   libread book add 9784003101018 --note "p.120まで"
   ```

   国立国会図書館サーチは応答に時間がかかることがあり、30秒で応答がなければエラーで終了します。
   そのときは少し待ってから実行し直してください。

## 毎日の使い方

引数なしで実行すると、読みかけの本すべてについて、今日読める図書館を表示します。

```sh
libread
```

```text
2026-10-30(金) 01:09 時点

■ 今日読める図書館
1. 世田谷区立梅丘図書館  開館前 09:00〜19:00
   - 『吾輩は猫である』 在架（貸出可）

■ 今日は読めない図書館
- 世田谷区立中央図書館  開館前 09:00〜19:00
   - 『吾輩は猫である』 所蔵なし
- 世田谷区立経堂図書館  開館情報未入力

所蔵情報: カーリル 01:09 取得（30分以内の取得結果を使用）
  『吾輩は猫である』 https://calil.jp/book/9784003101018
開館情報: 利用者入力（最終更新 中央図書館 2026-10-30 / 梅丘図書館 2026-10-30）
※ 臨時休館は入力済みの情報のみ反映しています
```

- `libread check <本>` で1冊だけを調べます。`<本>` は ISBN か書名の一部です。
- 所蔵状況は30分間キャッシュします。30分以内に実行し直すとカーリルに問い合わせず、前回の取得結果を表示します。
- 取得できなかった所蔵状況や、30分より古い所蔵状況は「不明」と表示し、「読める」には含めません。
- 開館情報は利用者が入力したものです。図書館の公式情報と照らし合わせて、ときどき更新してください。

## コマンド一覧

| コマンド | 機能 |
|---|---|
| `libread [check [<本>]]` | 今日読める図書館を表示する |
| `libread book find <書名\|ISBN> [--author <著者>] [--publisher <出版社>]` | 本を探す |
| `libread book add <ISBN> [--note <テキスト>]` | 読みかけの本として登録する |
| `libread book list` | 読みかけの本とメモを表示する |
| `libread book note <本> <テキスト>` | メモを書き換える（空文字で消去） |
| `libread book rm <本>` | 読みかけの本から外す |
| `libread lib find --pref <都道府県> [--city <市区町村>] [--name <部分一致>]` | 図書館を探す |
| `libread lib add <SystemID:LibKey>` | 利用図書館として登録する |
| `libread lib list` | 利用図書館と開館スケジュールを表示する |
| `libread lib rm <図書館>` | 利用図書館から外す |
| `libread lib hours <図書館> [--sun 〜 --sat <時間>] [--holiday <祝日>]` | 開館スケジュールを設定する |
| `libread lib closure add <図書館> <開始日> <終了日> [--reason <テキスト>]` | 臨時休館期間を追加する |
| `libread lib closure list <図書館>` | 臨時休館期間を番号付きで表示する |
| `libread lib closure rm <図書館> <番号>` | 臨時休館期間を削除する |

`<図書館>` は `SystemID:LibKey` か図書館名の一部で指定します。
候補が複数あるときは一覧を表示して終了します。
出力の形式や終了コードなど、詳しい仕様は [contracts/cli.md](specs/001-readable-libraries-today/contracts/cli.md) を参照してください。

## データの保存場所

| 内容 | 既定の場所 | 変更する環境変数 |
|---|---|---|
| 読みかけの本・メモ・利用図書館（`data.json`） | `os.UserConfigDir()/libread`（Linux は `~/.config/libread`、macOS は `~/Library/Application Support/libread`） | `LIBREAD_DATA_DIR` |
| 所蔵状況・祝日のキャッシュ | `os.UserCacheDir()/libread`（Linux は `~/.cache/libread`、macOS は `~/Library/Caches/libread`） | `LIBREAD_CACHE_DIR` |

外部サービスに送るのは、ISBN、図書館システムID、検索語、年だけです。メモはどこにも送りません。

## 開発

```sh
go build -o bin/libread ./cmd/libread
make check   # gofmt / go vet / staticcheck / go test
```

- テストは記録済みの応答だけを使い、実際のネットワークには接続しません。
- 開発中は、リポジトリ直下の `.env` に `LIBREAD_CALIL_APPKEY=<キー>` と書いておき、
  `set -a; . ./.env; set +a` で読み込めます。`.env` は `.gitignore` の対象なのでコミットされません。
- 実際のサービスで動きを確かめる手順は [quickstart.md](specs/001-readable-libraries-today/quickstart.md) にあります。

## 利用しているサービス

libread は次の外部サービスのデータを利用しています。

| サービス | 用途 |
|---|---|
| [国立国会図書館サーチ](https://ndlsearch.ndl.go.jp/) | 書名・ISBN による本の検索（書誌情報） |
| [カーリル](https://calil.jp/) | 図書館の検索と、所蔵・貸出状況の取得 |
| [holidays-jp](https://holidays-jp.github.io/) | 祝日の判定 |

- 書誌情報の検索には、**国立国会図書館サーチ API** を利用しています。
- 図書館の所蔵・貸出状況は、カーリルの API から取得しています。
  検索結果には、本ごとにカーリルのページへのリンクを表示します。
- 祝日のデータ（holidays-jp）は、Google カレンダーの祝日情報から自動生成されたものです。
  公的機関の一次データではありません。
