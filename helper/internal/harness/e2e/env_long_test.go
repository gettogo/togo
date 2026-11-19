package e2e

import "os"

// longEnv is true when TOGO_E2E_LONG=1 asks for the bigger sizes.
func longEnv() bool { return os.Getenv("TOGO_E2E_LONG") == "1" }
