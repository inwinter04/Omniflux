.PHONY: test vet tidy run infra infra-down admin-install admin-dev admin-build admin-sync build-admin docker-build
test:
	go test -race ./...
vet:
	go vet ./...
tidy:
	go mod tidy
infra:
	docker compose up -d
infra-down:
	docker compose down
admin-install:
	corepack pnpm --dir web/admin install
admin-dev:
	corepack pnpm --dir web/admin dev
admin-build:
	corepack pnpm --dir web/admin typecheck
	corepack pnpm --dir web/admin build
admin-sync:
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/sync-admin.ps1
build-admin: admin-build admin-sync
run:
	go run ./cmd/omnifluxdocker-build:
	docker build -t omniflux:latest .
