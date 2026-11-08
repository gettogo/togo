# Contributing to Togo

Thanks for taking the time. This guide covers setup, what you can work on without an Apple account, and what a pull request needs.

## Setup

Tools: Go (the version pinned in `helper/go.mod`), tmux 3.2 or later, Node.js for the relay, and on a Mac, Xcode with XcodeGen for the app.

```sh
make build        # the togo helper, into helper/bin
make lint test    # gofmt, go vet and the helper tests
make relay-test   # the push relay
make togokit-test # the TogoKit Swift package, on a Mac
make repo-docs-check  # links and copy in these documents
```

For the app, on a Mac:

```sh
make ios-bootstrap   # simulators, signing team, Togo.xcodeproj
make ios-test
make ios-snapshots
```

The Xcode project is generated from `apps/ios/project.yml` and is not committed. Run `make ios-gen` after changing `project.yml` or adding files.

## Work that needs no Apple account

The helper, agent adapters, relay and protocol schema build and test on macOS or Linux with no Apple developer account. The Swift packages in `packages/` also test on a Mac without one. Only running the app on a device and the push path through Apple need a team.

## Good first issues

- **Agent adapters.** Each agent Togo supports has an adapter in `helper/internal/harness/adapters`: a YAML file naming the agent's structured sources (hooks, status line, transcripts, an app server) and actions, plus small Go mappers that turn them into chat items. The Claude Code and Codex ones show the full shape; `hookstyle` covers agents whose hooks follow Claude Code's. Fixes for an agent's newer version, and adapters for agents Togo does not know yet, are welcome.
- **Themes.** Terminal color themes for the app.
- **Keyboard presets.** Key rows above the software keyboard for particular agents and tools.

Look for the `good first issue` label, or ask in Discussions before starting something large.

## Code style

- Comments explain why: a constraint, an invariant or a choice that is not obvious. They do not narrate the next line. No commented-out code. A TODO names its owner and the reason it is waiting.
- Go: `gofmt` and `go vet` clean. Every package has a package doc comment, and doc comments on exported names start with the name. Wrap errors with context, as in `fmt.Errorf("tmux new-session: %w", err)`.
- Swift: Swift 6 with strict concurrency and no warnings. `///` docs on public API where the name does not say it all.
- Processes run from argv arrays. Nothing the user or a repository controls (session names, paths, agent names) reaches a shell.
- The helper never opens a listening network port.
- Never log secrets, and never read an agent's credential files.
- Keep files under 500 lines. Prefer names to comments and small functions to clever ones. No speculative abstractions.
- Tests assert behaviour, and their names say what the behaviour is.
- App copy: no emoji, no em dashes, no uppercase section labels. The accent color is reserved for things that need the user.

## Commits and sign-off

Sign off every commit with `git commit -s`. That adds a `Signed-off-by` line certifying the [Developer Certificate of Origin](https://developercertificate.org/): that you wrote the change or have the right to submit it under the project's licenses. A check on every pull request looks for the line.

Contributions to the app (`apps/` and `packages/`) are licensed under the GNU General Public License v3 with the additional permission for app store distribution, both stated in [LICENSE](LICENSE). By adding the `Signed-off-by:` line you confirm the [Developer Certificate of Origin](https://developercertificate.org) and that you submit your change under those terms. Contributions to `helper/`, `relay/` and `protocol/` are licensed under MIT.

Write commit subjects in the imperative mood and under 72 characters. Add a body when the reason for the change is not obvious from the diff.

## Pull requests

- Keep each pull request to one change. Say what it changes and how you tested it; the template asks for both.
- CI must pass: helper lint and tests, relay tests and, for app changes, unit and snapshot tests.
- UI changes include screenshots in light and dark mode, and updated snapshot references when the change is intended.
- A maintainer reviews within a week or so. Expect questions about behaviour, tests and naming. Review is about the code, never the person.

## Security issues

Do not open a public issue for a vulnerability. Follow [SECURITY.md](SECURITY.md).

## Conduct

Everyone taking part follows the [Code of Conduct](CODE_OF_CONDUCT.md).
