# libread の Web サーバー（quickstart.md 手順 4）。画面をビルドし、Go のサーバーと1つのイメージにまとめる。

# 1. 画面（web/dist）
FROM node:22 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. サーバー
FROM golang:1.25 AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /out/libread-server ./cmd/libread-server

# 3. 実行用
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/libread-server /app/libread-server
COPY --from=web /src/web/dist /app/static
ENV LIBREAD_STATIC_DIR=/app/static
ENTRYPOINT ["/app/libread-server"]
