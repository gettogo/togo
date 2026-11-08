package replay

import (
	"strings"
	"testing"
)

func TestScrubRemovesSecretsPathsAndNames(t *testing.T) {
	s := Scrubber{
		Paths: map[string]string{"/Users/alice": "~", "/Users/alice/work/scratch": "/tmp/replay"},
		Words: map[string]string{"alice": "user", "alices-mbp": "host"},
	}
	raw := []byte(`{"jsonrpc":"2.0","method":"harness","params":{"items":[{"tool":{"output":"cd /Users/alice/work/scratch && ls /Users/alice/.ssh\nOPENAI_API_KEY=sk-abcdefghijklmnopqrstuv\nmail alice@corp.example.com on alices-mbp"}}],"apiKey":"xyz","token_count":12,"hostname":"alices-mbp"}}`)
	out := string(s.Frame(raw))
	for _, bad := range []string{"alice", "sk-abc", "xyz", "corp.example.com", "/Users/"} {
		if strings.Contains(strings.ToLower(out), bad) {
			t.Fatalf("%q survived: %s", bad, out)
		}
	}
	for _, good := range []string{"/tmp/replay && ls ~/.ssh", `"token_count":12`, "user@example.com", `"hostname":"host"`} {
		if !strings.Contains(out, good) {
			t.Fatalf("%q missing: %s", good, out)
		}
	}
}

func TestScrubDropsUnparseableFrames(t *testing.T) {
	if got := (Scrubber{}).Frame([]byte(`{nope`)); got != nil {
		t.Fatalf("got %s, want nil", got)
	}
}

func TestLeaksFindsForbiddenWords(t *testing.T) {
	if hits := Leaks(`{"cwd":"/Users/Alice/x"}`, []string{"alice"}); len(hits) != 1 {
		t.Fatalf("hits = %v", hits)
	}
	if hits := Leaks(`{"a":"user@example.com"}`, []string{"alice"}); len(hits) != 0 {
		t.Fatalf("hits = %v", hits)
	}
}

func TestScrubSignInURLParameters(t *testing.T) {
	out := (Scrubber{}).String("open https://x.ai/auth?client_id=abc&code_challenge=Zz9_k&state=s3cr3t&code_challenge_method=S256")
	if strings.Contains(out, "Zz9_k") || strings.Contains(out, "s3cr3t") || !strings.Contains(out, "client_id=abc") {
		t.Fatalf("got %s", out)
	}
}

func TestScrubWrappedSignInValues(t *testing.T) {
	raw := []byte(`{"a":"https://x.ai/auth?state=abcdefgh123","b":"Open: https://x.ai/auth?state=abcd\n   efgh123 to sign in"}`)
	out := string((Scrubber{}).Frame(raw))
	if strings.Contains(out, "efgh123") || strings.Contains(out, "abcdefgh") {
		t.Fatalf("got %s", out)
	}
}

func TestScrubKeepsReservedExampleAddresses(t *testing.T) {
	out := (Scrubber{}).String("ADA@example.com and ada@example.org pass; bob@corp.example.com and me@gmail.com do not")
	for _, good := range []string{"ADA@example.com", "ada@example.org"} {
		if !strings.Contains(out, good) {
			t.Fatalf("%q missing: %s", good, out)
		}
	}
	for _, bad := range []string{"bob@", "corp.example.com", "gmail"} {
		if strings.Contains(out, bad) {
			t.Fatalf("%q survived: %s", bad, out)
		}
	}
	if hits := Leaks(`{"a":"ada@example.com"}`, nil); len(hits) != 0 {
		t.Fatalf("hits = %v", hits)
	}
	if hits := Leaks(`{"a":"bob@corp.example.com"}`, nil); len(hits) != 1 {
		t.Fatalf("hits = %v", hits)
	}
}
