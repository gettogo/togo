.PHONY: lint test build check-trailers

# Empty if the helper has no Go sources yet, so vet/test can be skipped
# without hiding failures once packages exist.
GO_SRC := $(shell find helper -name '*.go' -not -path 'helper/bin/*' -print -quit)

lint:
	@test -z "$$(gofmt -l helper)"
	@if test -n "$(GO_SRC)"; then (cd helper && go vet ./...); fi

test:
	@if test -n "$(GO_SRC)"; then (cd helper && go test -race ./...); fi

build:
	cd helper && go build -o bin/togo -ldflags "-X github.com/gettogo/togo/helper/internal/version.Version=$$(git describe --always --dirty)" ./cmd/togo

check-trailers:
	@test -n "$(RANGE)" || { echo "check-trailers: RANGE is required, e.g. RANGE=HEAD~1..HEAD" >&2; exit 1; }
	./scripts/check-trailers.sh "$(RANGE)"
