-include .env

APP_NAME ?= app
DATABASE_PATH ?= app.db

.PHONY: atproto-up
atproto-up:
	docker compose up --detach --wait

.PHONY: atproto-down
atproto-down:
	docker compose down

# Create an account on the local PDS: make atproto-account HANDLE=alice PASSWORD=alice-password
.PHONY: atproto-account
atproto-account:
	curl --fail --silent --show-error --cacert local/caddy/caddy/pki/authorities/local/root.crt \
		--request POST --header "Content-Type: application/json" \
		--data '{"email":"$(HANDLE)@example.com","handle":"$(HANDLE).test","password":"$(PASSWORD)"}' \
		https://pds.localhost/xrpc/com.atproto.server.createAccount

.PHONY: benchmark
benchmark:
	go test -tags sqlite_fts5,sqlite_math_functions -bench . ./...

.PHONY: build-docker
build-docker:
	docker build --platform linux/arm64 -t $(APP_NAME) .

.PHONY: clean-all
clean-all:
	rm -f $(DATABASE_PATH) $(DATABASE_PATH)-wal $(DATABASE_PATH)-shm

.PHONY: cover
cover:
	go tool cover -html cover.out

.PHONY: deps
deps:
	curl -Lf -o public/scripts/datastar.js https://cdn.jsdelivr.net/gh/starfederation/datastar@1.0.3/bundles/datastar.js

.PHONY: fmt
fmt:
	goimports -w -local `head -n 1 go.mod | sed 's/^module //'` .

.PHONY: lint
lint:
	golangci-lint run

tailwindcss:
	curl -sfL -o tailwindcss https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-macos-arm64
	chmod a+x tailwindcss

.PHONY: test
test:
	go test -tags sqlite_fts5,sqlite_math_functions -coverprofile cover.out -shuffle on ./...

.PHONY: watch
watch: tailwindcss
	go tool redo
