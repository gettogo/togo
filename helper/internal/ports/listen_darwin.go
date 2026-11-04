package ports

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// listeners asks lsof for the TCP LISTEN sockets of pids. lsof exits 1 when
// nothing matches, which is an empty answer, not a failure.
func listeners(ctx context.Context, pids []int) ([]Listener, error) {
	if len(pids) == 0 {
		return nil, nil
	}
	list := make([]string, len(pids))
	for i, p := range pids {
		list[i] = strconv.Itoa(p)
	}
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-a", "-p", strings.Join(list, ","), "-F", "pcn").Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, fmt.Errorf("lsof: %w", err)
	}
	return parseLsofF(string(out)), nil
}
