.PHONY: lint test build check-trailers relay-test \
	ios-sims ios-bootstrap ios-gen ios-build ios-test ios-ui-test ios-snapshots ios-snapshots-record \
	ios-release-check release-build release-test install-test tokens tokens-check togopush-test watch-test

IOS_DIR := apps/ios
IOS_PROJECT := $(IOS_DIR)/Togo.xcodeproj
IOS_DERIVED := $(IOS_DIR)/DerivedData
IOS_TEST_DEST := platform=iOS Simulator,name=iPhone 17
WATCH_TEST_DEST := platform=watchOS Simulator,name=Apple Watch Series 11 (46mm)
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

# Reproducible helper release assets in dist/. VERSION is required, e.g.
# VERSION=v1.0.0. ALLOW_DIRTY=1 makes a development build: it is marked dirty
# and written to dist/dev/, never next to release assets.
RELEASE_DIR := $(if $(ALLOW_DIRTY),../dist/dev,../dist)
release-build:
	@test -n "$(VERSION)" || { echo "release-build: VERSION is required, e.g. VERSION=v1.0.0" >&2; exit 1; }
	@test -z "$(ALLOW_DIRTY)" || echo "release-build: development build, output goes to dist/dev and is not releasable" >&2
	cd helper && go run ./cmd/togo-release build --version "$(VERSION)" --out ../dist --module-dir . $(if $(ALLOW_DIRTY),--allow-dirty)
	cd helper && go run ./cmd/togo-release sums $(RELEASE_DIR)

release-test:
	cd helper && go test ./internal/release/... ./cmd/togo-release/... ./internal/version/...

# Runs scripts/install.sh against signed test releases on 127.0.0.1.
install-test:
	./scripts/install-test.sh

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
	$(XCODEBUILD) -configuration Debug -destination '$(IOS_TEST_DEST)' -skip-testing:TogoSnapshotTests test

# UI tests launch the app on fake services, on any iPhone or iPad simulator.
ios-ui-test: ios-gen
	xcodebuild -project $(IOS_PROJECT) -scheme TogoUITests -derivedDataPath $(IOS_DERIVED) \
		-skipPackagePluginValidation -skipMacroValidation -quiet \
		-configuration Debug -destination '$(IOS_TEST_DEST)' test

# References only match renders from the iPhone 17 simulator at its default settings.
ios-snapshots: ios-gen
	$(XCODEBUILD) -configuration Debug -destination '$(IOS_TEST_DEST)' -only-testing:TogoSnapshotTests test

# TEST_RUNNER_ variables reach the test process with the prefix removed.
ios-snapshots-record: ios-gen
	TEST_RUNNER_SNAPSHOT_TESTING_RECORD=all \
	$(XCODEBUILD) -configuration Debug -destination '$(IOS_TEST_DEST)' -only-testing:TogoSnapshotTests test || true
	$(MAKE) ios-snapshots

togopush-test:
	swift test --package-path packages/TogoPush

watch-test: ios-gen
	xcodebuild -project $(IOS_PROJECT) -scheme TogoWatch -derivedDataPath $(IOS_DERIVED) \
		-skipPackagePluginValidation -skipMacroValidation -quiet \
		-configuration Debug -destination '$(WATCH_TEST_DEST)' test

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
	scripts/relay-deploy-test.sh
	cd relay && npm ci && npm test && npx tsc --noEmit
