public enum HelperCommand {
    /// Starts `togo serve --stdio` on the host from an SSH exec channel.
    ///
    /// An exec channel gets a non-interactive shell whose PATH rarely includes
    /// `~/.togo/bin` or Homebrew, so the install locations are tried first and a bare
    /// `togo` from PATH last. The script is one line with no backslashes or single
    /// quotes: csh rejects newlines inside single quotes and fish treats backslashes
    /// there as escapes, and the login shell parses the quoted word before `/bin/sh` runs.
    public static let serveStdio: String = shellCommand(
        #"for t in "$HOME/.togo/bin/togo" /opt/homebrew/bin/togo /usr/local/bin/togo; do "#
            + #"if [ -x "$t" ]; then exec "$t" serve --stdio; fi; done; exec togo serve --stdio"#
    )
}
