package appinventory

import (
	"strings"
	"testing"
)

func TestDecodeProofArtifact(t *testing.T) {
	record := `{"key":"omp/app/test","revision":"abc","tier":"replay-ui","result":"passed"}`
	proof, err := DecodeProof(strings.NewReader("[" + record + "]\n"))
	if err != nil || len(proof) != 1 || proof[0] != (Proof{Key: "omp/app/test", Revision: "abc", Tier: ReplayUI, Result: "passed"}) {
		t.Fatalf("valid proof lost: %v, %v", proof, err)
	}
	for _, input := range []string{
		"[" + record + "]{}",
		"[" + record + "] []",
		"[" + strings.TrimSuffix(record, "}") + `,"extra":true}]`,
		"[" + strings.TrimSuffix(record, "}") + `,"key":"another"}]`,
		"null", "{}", "[null]", `[{"key":"only"}]`,
	} {
		if _, err := DecodeProof(strings.NewReader(input)); err == nil {
			t.Errorf("accepted invalid artifact: %s", input)
		}
	}
}
