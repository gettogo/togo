import Foundation
import Testing
@testable import Togo

struct RecentFoldersTests {
    @Test func leavesOutTemporaryFoldersAndKeepsFive() {
        let dirs = [
            "/private/tmp/workdir-01/x/scratchpad/harness-e2e/a",
            "/Users/me/Dev/togo",
            "/tmp/build",
            "/var/folders/ab/T/x",
            "/Users/me/scratchpad/notes",
            "/Users/me/a", "/Users/me/b", "/Users/me/Dev/togo", "/Users/me/c", "/Users/me/d", "/Users/me/e",
        ]
        #expect(RecentFolders.shown(dirs) == [
            "/Users/me/Dev/togo", "/Users/me/a", "/Users/me/b", "/Users/me/c", "/Users/me/d",
        ])
    }

    @Test func temporaryFolders() {
        #expect(RecentFolders.isTemporary("/tmp"))
        #expect(RecentFolders.isTemporary("/private/tmp/x"))
        #expect(RecentFolders.isTemporary("/private/var/folders/ab/T/y"))
        #expect(RecentFolders.isTemporary("/home/me/work/scratchpad/z"))
        #expect(!RecentFolders.isTemporary("/tmpfiles/app"))
        #expect(!RecentFolders.isTemporary("/Users/me/scratchpads"))
    }

    @Test func displayIsHomeRelativeWithTheMiddleCollapsed() {
        #expect(RecentFolders.display("/Users/me/Dev/togo/.worktrees/fix") == ("~/…/.worktrees/", "fix"))
        #expect(RecentFolders.display("/Users/me/Dev/togo") == ("~/Dev/", "togo"))
        #expect(RecentFolders.display("/Users/me/app") == ("~/", "app"))
        #expect(RecentFolders.display("/Users/me") == ("", "~"))
        #expect(RecentFolders.display("/srv/apps/site/current/web") == ("/srv/…/current/", "web"))
        #expect(RecentFolders.display("/opt/app") == ("/opt/", "app"))
        #expect(RecentFolders.display("/") == ("", "/"))
    }

    @MainActor @Test func removingARecentDropsItAtOnce() async {
        let model = NewSessionModel(
            services: FakeAppServices(), hostID: nil,
            machines: Array(Samples.controlCenter.machines.prefix(1)), open: { _, _ in }
        )
        await model.start()
        model.removeRecent("/Users/sam/src/site")
        #expect(model.recent == .loaded(["/Users/sam/src/app", "/Users/sam/notes"]))
        await model.forgetRecent("/Users/sam/src/site")
        #expect(model.recent == .loaded(["/Users/sam/src/app", "/Users/sam/notes"]))
    }
}
