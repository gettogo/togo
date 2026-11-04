package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/gettogo/togo/helper/internal/doctor"
)

// runDoctor prints the host readiness report. It exits 1 only when tmux is
// missing or too old, since Togo cannot work without it; every other
// finding is advice.
func runDoctor(args []string, e doctor.Env, out io.Writer) int {
	fs := newFlagSet("doctor")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(out, err)
	}
	if len(positional) > 0 {
		return usageError(out, fmt.Errorf("doctor takes no arguments"))
	}

	checks := doctor.Run(context.Background(), e)
	if *asJSON {
		if err := json.NewEncoder(out).Encode(struct {
			Checks []doctor.Check `json:"checks"`
		}{checks}); err != nil {
			return exitError
		}
	} else {
		for _, c := range checks {
			detail := c.Detail
			if c.Fix != "" {
				detail += "; fix: " + c.Fix
			}
			fmt.Fprintf(out, "%-13s %-9s %s\n", c.Name, c.Status, detail)
		}
	}
	if len(checks) > 0 && checks[0].Name == "tmux" && (checks[0].Status == doctor.StatusMissing || checks[0].Status == doctor.StatusWarn && checks[0].Version != "") {
		return exitError
	}
	return exitOK
}
