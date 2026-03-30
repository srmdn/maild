APP_NAME=maild
MAIN_PACKAGE=./cmd/server

.PHONY: setup run build test tidy

setup:
	@if [ ! -f .env ]; then cp .env.example .env; fi
	go mod download
	docker compose up -d

run:
	go run $(MAIN_PACKAGE)

build:
	mkdir -p bin
	go build -o bin/$(APP_NAME) $(MAIN_PACKAGE)

test:
	go test ./...

tidy:
	go mod tidy
