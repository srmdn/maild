APP_NAME=maild
MAIN_PACKAGE=./cmd/server

.PHONY: run build test tidy

run:
	go run $(MAIN_PACKAGE)

build:
	mkdir -p bin
	go build -o bin/$(APP_NAME) $(MAIN_PACKAGE)

test:
	go test ./...

tidy:
	go mod tidy

