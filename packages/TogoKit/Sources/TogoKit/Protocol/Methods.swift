public enum Method: String, Sendable {
    case hello
    case sessionList = "session.list"
    case sessionCreate = "session.create"
    case sessionKill = "session.kill"
    case sessionRename = "session.rename"
    case sessionAttachCommand = "session.attachCommand"
    case sessionCapture = "session.capture"
    case agentsList = "agents.list"
    case fsRecentDirs = "fs.recentDirs"
    case fsListDir = "fs.listDir"
}
