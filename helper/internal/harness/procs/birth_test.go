package procs

import (
	"os/exec"
	"testing"
)

func TestBirthRejectsAReapedProcess(t *testing.T) {
	bin, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat unavailable")
	}
	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	first, err := Birth(cmd.Process.Pid)
	if err != nil || first == "" {
		t.Fatalf("live process has no identity: %q %v", first, err)
	}
	second, err := Birth(cmd.Process.Pid)
	if err != nil || second != first {
		t.Fatalf("stable process changed identity: %q %q %v", first, second, err)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := Birth(cmd.Process.Pid); err == nil {
		t.Fatal("reaped process still has a live identity")
	}
}
