APP_NAME=maild
MAIN_PACKAGE=./cmd/server

.PHONY: setup run build test tidy fmt-check check-attribution vuln verify verify-full

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

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

check-attribution:
	./scripts/check-commit-attribution.sh

verify: fmt-check build test check-attribution

vuln:
	@command -v govulncheck >/dev/null 2>&1 || { \
		echo "govulncheck not found."; \
		echo "Install with:"; \
		echo "  go install golang.org/x/vuln/cmd/govulncheck@latest"; \
		exit 1; \
	}
	govulncheck ./...

verify-full: verify vuln
