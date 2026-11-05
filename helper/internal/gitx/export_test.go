package gitx

// SetFingerprintBudget changes the fingerprint byte budget until the
// returned function restores it.
func SetFingerprintBudget(n int64) (restore func()) {
	old := fingerprintBudget
	fingerprintBudget = n
	return func() { fingerprintBudget = old }
}
