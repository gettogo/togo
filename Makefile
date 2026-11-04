.PHONY: lint test build check-trailers relay-test \
	ios-sims ios-bootstrap ios-gen ios-build ios-test ios-release-check tokens tokens-check

IOS_DIR := apps/ios
IOS_PROJECT := $(IOS_DIR)/Togo.xcodeproj
IOS_DERIVED := $(IOS_DIR)/DerivedData
IOS_TEST_DEST := platform=iOS Simulator,name=iPhone 17
# SwiftTerm (via TogoTerminal) ships a build plugin; unattended builds cannot
# click through the trust prompt, so validation is skipped for packages and macros.
XCODEBUILD := xcodebuild -project $(IOS_PROJECT) -scheme Togo -derivedDataPath $(IOS_DERIVED) \
	-skipPackagePluginValidation -skipMacroValidation -quiet

lint:
	@files="$$(gofmt -l helper)"; \
	if [ -n "$$files" ]; then echo "gofmt -l reports:"; echo "$$files"; exit 1; fi
	cd helper && go vet ./...

test:
	cd helper && go test -race ./...

build:
	cd helper && go build -o bin/togo -ldflags "-X github.com/gettogo/togo/helper/internal/version.Version=$$(git describe --always --dirty)" ./cmd/togo

check-trailers:
	@test -n "$(RANGE)" || { echo "check-trailers: RANGE is required, e.g. RANGE=HEAD~1..HEAD" >&2; exit 1; }
	./scripts/check-trailers.sh "$(RANGE)"

ios-sims:
	./scripts/ios-sims.sh

ios-bootstrap:
	./scripts/bootstrap-ios.sh

ios-gen:
	cd $(IOS_DIR) && xcodegen generate --quiet

ios-build: ios-gen
	$(XCODEBUILD) -configuration Debug -destination 'generic/platform=iOS Simulator' build

ios-test: ios-gen
	$(XCODEBUILD) -configuration Debug -destination '$(IOS_TEST_DEST)' test

# Signing is off because this checks what the Release binary contains, not
# whether it can be provisioned; that happens in Xcode against the team.
ios-release-check: ios-gen
	$(XCODEBUILD) -configuration Release -destination 'generic/platform=iOS' CODE_SIGNING_ALLOWED=NO build
	./scripts/ios-release-check.sh $(IOS_DERIVED)/Build/Products/Release-iphoneos/Togo.app

tokens:
	swift scripts/gen-tokens.swift

tokens-check:
	swift scripts/gen-tokens.swift --check

relay-test:
	cd relay && npm ci && npm test && npx tsc --noEmit
