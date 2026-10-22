# libread

本を借りずに図書館の館内で読む人のための、個人用のコマンドラインツールです。
登録した読みかけの本について、近隣の図書館のうち「今日開館していて、その本を館内で手に取れる館」を一覧で表示します。

> 開発中です。今の時点では、まだ図書館の検索はできません。
> 仕様と進み具合は [specs/001-readable-libraries-today/](specs/001-readable-libraries-today/) を参照してください。

## 必要なもの

- Go 1.25 以上
- カーリルのアプリケーションキー（環境変数 `LIBREAD_CALIL_APPKEY` に設定する）
  - 開発中は、リポジトリ直下の `.env` に `LIBREAD_CALIL_APPKEY=<キー>` と書いておき、
    `set -a; . ./.env; set +a` で読み込めます。`.env` は `.gitignore` の対象なのでコミットされません。

## ビルドとテスト

```sh
go build -o bin/libread ./cmd/libread
make check   # gofmt / go vet / staticcheck / go test
```

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
