.PHONY: check

# 品質ゲート（quickstart.md 手順 1）
# web/dist の検査は秘密情報の「名前」だけを探す。値はローカルの環境にしかないため照合しない（FR-113）。
# 型チェックは npm run typecheck で済ませるため、web/dist は vite build だけで作る。
check:
	@files="$$(git ls-files -- '*.go')" || exit 1; \
	if [ -z "$$files" ]; then echo "Go のファイルが見つかりません"; exit 1; fi; \
	out="$$(gofmt -l $$files)" || exit 1; \
	if [ -n "$$out" ]; then echo "gofmt が必要なファイル:"; echo "$$out"; exit 1; fi
	go vet ./...
	go tool staticcheck ./...
	go test ./...
	cd web && npm ci && npm run format:check && npm run typecheck && npm run lint && npm test
	cd web && npx vite build
	@if [ ! -d web/dist ]; then echo "web/dist がありません"; exit 1; fi; \
	grep -rniE 'LIBREAD_|client_?secret|app_?key' web/dist; rc=$$?; \
	if [ $$rc -eq 0 ]; then echo "web/dist に秘密情報の名前が含まれています"; exit 1; fi; \
	if [ $$rc -ne 1 ]; then echo "web/dist の検査に失敗しました"; exit 1; fi
