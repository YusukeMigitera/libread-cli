.PHONY: check

# 品質ゲート（quickstart.md 手順 1）
# web/dist の検査は秘密情報の「名前」だけを探す。値はローカルの環境にしかないため照合しない（FR-113）。
check:
	@out="$$(gofmt -l $$(git ls-files '*.go'))"; if [ -n "$$out" ]; then echo "gofmt が必要なファイル:"; echo "$$out"; exit 1; fi
	go vet ./...
	go tool staticcheck ./...
	go test ./...
	cd web && npm ci && npm run format:check && npm run typecheck && npm run lint && npm test
	cd web && npm run build
	@if grep -rE 'LIBREAD_|client_secret|appkey' web/dist; then echo "web/dist に秘密情報の名前が含まれています"; exit 1; fi
