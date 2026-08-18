.PHONY: check demo demo-down demo-status go-check web-check site-check postgres-check

demo:
	go run ./cmd/amsonia-local up

demo-down:
	go run ./cmd/amsonia-local down

demo-status:
	go run ./cmd/amsonia-local status

check: go-check web-check site-check

go-check:
	test -z "$$(gofmt -l -- $$(find . -path './web/node_modules' -prune -o -name '*.go' -print))"
	go list ./... | awk '!/\/web\/node_modules\//' | xargs go vet
	go list ./... | awk '!/\/web\/node_modules\//' | xargs go test
	go list ./... | awk '!/\/web\/node_modules\//' | xargs go test -race
	go list ./... | awk '!/\/web\/node_modules\//' | xargs go run golang.org/x/vuln/cmd/govulncheck@v1.7.0

web-check:
	npm --prefix web ci
	npm --prefix web audit --audit-level=moderate
	npm --prefix web run typecheck
	npm --prefix web run lint
	npm --prefix web run test
	npm --prefix web run build
	npx --yes @redocly/cli@2.46.1 lint openapi/openapi.yaml

site-check:
	npm --prefix site ci
	npm --prefix site audit --audit-level=moderate
	npm --prefix site run check

postgres-check:
	test -n "$(TEST_DATABASE_ADMIN_URL)"
	go test -tags postgres -count=1 ./postgres/
