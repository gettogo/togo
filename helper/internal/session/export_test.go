package session

// Internals the external tests pin.
var LaunchWrapper = launchWrapper

var TmuxConf = tmuxConf

var WheelBindings = func(target string) [][2]string {
	var out [][2]string
	for _, b := range wheelBindings(target) {
		out = append(out, [2]string{b.Key, b.Cmd})
	}
	return out
}

const (
	PosixWrapper = posixWrapper
	FishWrapper  = fishWrapper
	OptionArgv   = optionArgv
)
