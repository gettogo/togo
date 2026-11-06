package awake

import "os"

// ForHost returns the OS pieces of a Config for goos, holding the machine
// for as long as the process pid lives. Platforms without a way to hold
// the machine get an unsupported Config.
func ForHost(goos string, pid int) Config {
	switch goos {
	case "darwin":
		return Config{
			Supported:  true,
			Holder:     Caffeinate(pid),
			Override:   SudoPmset{},
			AdminReady: func() bool { return exists(DefaultSudoersPath) },
			Power:      Pmset{},
		}
	case "linux":
		inhibit := firstExisting("/usr/bin/systemd-inhibit", "/bin/systemd-inhibit")
		tail := firstExisting("/usr/bin/tail", "/bin/tail")
		if inhibit == "" || tail == "" {
			return Config{}
		}
		return Config{Supported: true, Holder: Inhibitor(pid, inhibit, tail)}
	}
	return Config{}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if exists(p) {
			return p
		}
	}
	return ""
}
