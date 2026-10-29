# Quickstart: 今日読める図書館の検索

**Feature**: [spec.md](./spec.md) | **CLI**: [contracts/cli.md](./contracts/cli.md)

この機能が端から端まで動くことを確かめる手順。

## 前提

- Go 1.25 以上
- カーリルのアプリケーションキー（カーリルの開発者ダッシュボードで取得）。
  手順 2 と 3 で使う。手順 1 には不要。

## 1. 品質ゲート（ネットワーク不要）

```sh
gofmt -l .               # 何も出力されないこと
go vet ./...
go tool staticcheck ./...
go test ./...            # 記録済みレスポンスだけを使い、実ネットワークには接続しない
```

**期待結果**: すべて警告なしで成功する。

## 2. 実データでの確認

検証用に専用のデータ置き場を使い、普段のデータを汚さないようにする。

```sh
export LIBREAD_CALIL_APPKEY=<your key>
export LIBREAD_DATA_DIR="$(mktemp -d)"
go build -o ./bin/libread ./cmd/libread

# 利用図書館の登録（User Story 3）
./bin/libread lib find --pref 東京都 --city 世田谷区
./bin/libread lib add <表示された SystemID:LibKey>
./bin/libread lib hours <図書館> --sun 09:00-17:00 --mon closed \
  --tue 09:00-19:00 --wed 09:00-19:00 --thu 09:00-19:00 --fri 09:00-19:00 --sat 09:00-17:00 \
  --holiday same

# 読みかけの本の登録（User Story 2）
./bin/libread book find 吾輩は猫である --author 夏目漱石 --publisher 岩波書店
./bin/libread book add <表示された ISBN> --note "p.120まで"

# 検索（User Story 1）
./bin/libread
```

**期待結果**:

- 開館中で在架の館が「■ 今日読める図書館」に表示される。
- 表示内容がカーリルの書籍ページ（出力に表示される URL）の状況と一致する。
- 所蔵の取得日時と、開館情報の「利用者入力」表記・最終更新日が表示される。

## 3. 境界条件の確認

| 操作 | 期待結果 |
|---|---|
| `lib closure add <図書館> <今日> <今日>` の後に `libread` | その館が「休館（期間）」になり、読めない側に表示される |
| 開館スケジュールを入力していない館を `lib add` した後に `libread` | 「開館情報未入力」と表示される |
| 30分以内に `libread` をもう一度実行 | 同じ取得日時が表示される（キャッシュを利用し、再取得しない） |
| `LIBREAD_CALIL_APPKEY` を外して `libread` | 終了コード 1 で、キーの設定方法が案内される |
| `libread check <ISBN>` | その本だけが対象になる |

時刻に依存する条件（閉館後、閉館まで30分未満、祝日、取得タイムアウト）は、
現在時刻と外部応答を差し替えた自動テストで確かめる（手順 1 の `go test`）。
