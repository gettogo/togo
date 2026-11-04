package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestRegisteredCommandRunsWithStdinAndUsageLine(t *testing.T) {
	saved := usage
	t.Cleanup(func() { usage = saved; delete(commands, "echo-test") })
	register("echo-test", func(_ context.Context, args []string, env map[string]string, stdin io.Reader, stdout, _ io.Writer) int {
		in, _ := io.ReadAll(stdin)
		stdout.Write([]byte(strings.Join(args, ",") + "|" + env["K"] + "|" + string(in)))
		return 7
	})
	addUsage("  echo-test                                     test command")

	var out, errOut bytes.Buffer
	code := runContext(context.Background(), []string{"echo-test", "a", "b"}, map[string]string{"K": "v"}, strings.NewReader("in"), &out, &errOut)
	if code != 7 || out.String() != "a,b|v|in" {
		t.Errorf("code=%d stdout=%q", code, out.String())
	}
	runContext(context.Background(), nil, nil, strings.NewReader(""), &out, &errOut)
	if !strings.Contains(errOut.String(), "echo-test") {
		t.Errorf("usage lacks the added line: %q", errOut.String())
	}
}

func TestRegisterTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("duplicate register did not panic")
		}
	}()
	register("version", nil)
}
