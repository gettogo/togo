.PHONY: lint test build check-trailers relay-test

lint:
	@test -z "$$(gofmt -l helper)"
	cd helper && go vet ./...

test:
	cd helper && go test -race ./...

build:
	cd helper && go build -o bin/togo -ldflags "-X github.com/gettogo/togo/helper/internal/version.Version=$$(git describe --always --dirty)" ./cmd/togo

check-trailers:
	@test -n "$(RANGE)" || { echo "check-trailers: RANGE is required, e.g. RANGE=HEAD~1..HEAD" >&2; exit 1; }
	./scripts/check-trailers.sh "$(RANGE)"

relay-test:
	cd relay && npm ci && npm test && npx tsc --noEmit
