APP=modeldock
GO=go

.PHONY: build frontend-install frontend-build frontend-dev test dev dev-windows release

NPM ?= npm

frontend-install:
	$(NPM) install --prefix web

frontend-build: frontend-install
	$(NPM) run typecheck --prefix web
	$(NPM) run build --prefix web

frontend-dev: frontend-install
	$(NPM) run dev --prefix web

build: frontend-build
	$(GO) build -o bin/$(APP) ./cmd/modeldock

test:
	$(GO) test ./...

dev:
	$(GO) run ./cmd/modeldock serve

dev-windows:
	powershell -ExecutionPolicy Bypass -File scripts/modeldock-dev.ps1 start

release:
	$(MAKE) frontend-build
	@mkdir -p bin
	GOOS=darwin GOARCH=arm64 $(GO) build -o bin/$(APP)-darwin-arm64 ./cmd/modeldock
	GOOS=darwin GOARCH=amd64 $(GO) build -o bin/$(APP)-darwin-amd64 ./cmd/modeldock
	GOOS=linux GOARCH=amd64 $(GO) build -o bin/$(APP)-linux-amd64 ./cmd/modeldock
	GOOS=linux GOARCH=arm64 $(GO) build -o bin/$(APP)-linux-arm64 ./cmd/modeldock
	GOOS=windows GOARCH=amd64 $(GO) build -o bin/$(APP)-windows-amd64.exe ./cmd/modeldock
