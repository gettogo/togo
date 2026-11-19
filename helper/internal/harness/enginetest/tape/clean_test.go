package tape

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func lines(s ...string) [][]byte {
	var out [][]byte
	for _, l := range s {
		out = append(out, []byte(l))
	}
	return out
}

// A planted home folder, scratch folder, user name and host name do not
// survive, in a result, in a notification, or in a tool's output.
func TestPlantedPathsAndNamesDoNotSurvive(t *testing.T) {
	const (
		home  = "/Users/planted-person"
		temp  = "/var/folders/zz/plantedtemp/T/TestPlant/001"
		who   = "plantedperson"
		where = "planted-host"
	)
	c := Cleaner{
		Paths:          map[string]string{home: FixtureUser, temp: FixtureHome},
		Words:          map[string]string{who: "user", where: "host"},
		Forbidden:      []string{home, temp},
		ForbiddenWords: []string{who, where},
	}
	text := "cd " + temp + "/proj && ls " + home + "/.ssh; whoami is " + who + " on " + where
	in := lines(
		`{"jsonrpc":"2.0","id":1,"method":"harness.open","params":{"id":"s_aaaaaaaaaa","after":0}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"epoch":"abc","cursor":1,"items":[{"id":"call_1","seq":1,"kind":"tool","tool":{"output":"`+text+`","cwd":"`+temp+`/proj"}}]}}`,
		`{"jsonrpc":"2.0","method":"harness","params":{"sessionId":"s_aaaaaaaaaa","kind":"items","epoch":"abc","ts":"2026-11-17T09:00:00Z","items":[{"id":"n1","seq":2,"kind":"assistant","assistant":{"text":"`+text+`"}}]}}`,
	)
	out, _, err := c.Clean(in)
	if err != nil {
		t.Fatal(err)
	}
	all := string(join(out))
	for _, bad := range []string{home, temp, who, where, "plantedtemp", "planted-person"} {
		if strings.Contains(all, bad) {
			t.Errorf("%q survived: %s", bad, all)
		}
	}
	for _, good := range []string{"cd /fixture/proj && ls /home/user/.ssh; whoami is user on host", `"cwd":"/fixture/proj"`} {
		if !strings.Contains(all, good) {
			t.Errorf("%q missing: %s", good, all)
		}
	}
}

func join(lines [][]byte) []byte {
	var out []byte
	for _, l := range lines {
		out = append(append(out, l...), '\n')
	}
	return out
}

// The cleaner built for this machine removes this machine's own home
// folder, user name, host name and temp folder.
func TestHostCleanerRemovesThisMachine(t *testing.T) {
	scratch := t.TempDir()
	c, err := HostCleaner(map[string]string{scratch: FixtureHome})
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	var names []string
	if u, err := user.Current(); err == nil && len(u.Username) >= 3 {
		names = append(names, u.Username)
	}
	if len(host) >= 3 {
		names = append(names, host)
	}
	text := "in " + filepath.Join(scratch, "proj") + " and " + filepath.Join(home, ".ssh") + " as " + strings.Join(names, " and ")
	out, _, err := c.Clean(lines(`{"jsonrpc":"2.0","id":1,"result":{"text":"` + text + `"}}`))
	if err != nil {
		t.Fatal(err)
	}
	all := string(join(out))
	for _, bad := range append([]string{scratch, home + "/"}, names...) {
		if len(bad) >= 3 && strings.Contains(strings.ToLower(all), strings.ToLower(bad)) {
			t.Errorf("%q survived: %s", bad, all)
		}
	}
	if !strings.Contains(all, FixtureHome+"/proj") {
		t.Errorf("the scratch folder has no stand-in: %s", all)
	}
}

// A path the cleaner was not told about is caught by the last check, and
// the export fails.
func TestCleanFailsOnWhatItMissed(t *testing.T) {
	c := Cleaner{Forbidden: []string{"/Users/planted-person"}, ForbiddenWords: []string{"plantedperson"}}
	for _, text := range []string{"/Users/planted-person/x", "by plantedperson today"} {
		if _, _, err := c.Clean(lines(`{"jsonrpc":"2.0","id":1,"result":{"text":"` + text + `"}}`)); err == nil {
			t.Errorf("%q passed", text)
		}
	}
	if _, _, err := c.Clean(lines(`{"jsonrpc":"2.0","id":1,"result":{"text":"plantedpersonal notes"}}`)); err != nil {
		t.Errorf("a longer word that holds the name was refused: %v", err)
	}
}

// Scrubbing never changes an id the app compares: a record whose id looks
// like a secret fails the export, it is not rewritten.
func TestCleanRefusesToChangeAnId(t *testing.T) {
	c := Cleaner{}
	_, _, err := c.Clean(lines(`{"jsonrpc":"2.0","id":1,"result":{"items":[{"id":"sk-abcdefghijklmnopqrstuvwxyz0123456789","seq":1}]}}`))
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("err = %v", err)
	}
}

// An address is replaced with a reserved example one.
func TestAddressesAreReplaced(t *testing.T) {
	out, _, err := Cleaner{}.Clean(lines(`{"jsonrpc":"2.0","id":1,"result":{"text":"mail bob@corp.example.com"}}`))
	if err != nil || strings.Contains(string(join(out)), "corp") {
		t.Errorf("%s, %v", join(out), err)
	}
}

// Text the repo's secret rules redact is redacted, not exported.
func TestSecretsAreRedacted(t *testing.T) {
	out, _, err := Cleaner{}.Clean(lines(`{"jsonrpc":"2.0","id":1,"result":{"text":"OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz0123456789"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(join(out)), "sk-abcdef") {
		t.Errorf("the key survived: %s", join(out))
	}
}

// The same value gets the same replacement across the tape and different
// values stay different, so the references the app follows still line up.
func TestNormalisationIsConsistent(t *testing.T) {
	in := lines(
		`{"jsonrpc":"2.0","id":1,"result":{"epoch":"aaaaaaaaaaaa","cursor":3,"items":[{"id":"p0123456789ab1","seq":3}]}}`,
		`{"jsonrpc":"2.0","method":"harness","params":{"sessionId":"s_aaaaaaaaaa","kind":"items","epoch":"aaaaaaaaaaaa","ts":"2026-11-19T01:02:03.456789-05:00","items":[{"id":"p0123456789ab1","seq":4,"user":{"text":"approve r_abcdefghij"}}]}}`,
		`{"jsonrpc":"2.0","method":"harness","params":{"sessionId":"s_aaaaaaaaaa","kind":"items","epoch":"bbbbbbbbbbbb","ts":"2026-11-19T01:02:04.1-05:00","items":[{"id":"pfedcba9876541","seq":1,"user":{"text":"approve r_abcdefghij and r_bcdefghijk"}}]}}`,
	)
	out, used, err := Cleaner{}.Clean(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{NormRequest, NormEpoch, NormNoteTime, NormPhoneItem}
	if strings.Join(used, ",") != strings.Join(want, ",") {
		t.Errorf("normalised %v, want %v", used, want)
	}
	all := string(join(out))
	for _, gone := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "0123456789ab", "fedcba987654", "01:02:03", "r_abcdefghij", "r_bcdefghijk"} {
		if strings.Contains(all, gone) {
			t.Errorf("%q was not normalised: %s", gone, all)
		}
	}
	e1, e2 := `"epoch":"000000000001"`, `"epoch":"000000000002"`
	if strings.Count(all, e1) != 2 || strings.Count(all, e2) != 1 {
		t.Errorf("epochs: want two of the first and one of the second: %s", all)
	}
	if strings.Count(all, "p"+"f00000000001"+"1") != 2 {
		t.Errorf("the same phone item id got two replacements: %s", all)
	}
	for _, kept := range []string{`"cursor":3`, `"seq":3`, `"seq":4`, `"seq":1`} {
		if !strings.Contains(all, kept) {
			t.Errorf("%s changed: %s", kept, all)
		}
	}
}

// A server's event ids carry its clock: the same id keeps one stand-in.
func TestServerEventIdsAreNormalised(t *testing.T) {
	out, used, err := Cleaner{}.Clean(lines(
		`{"jsonrpc":"2.0","id":1,"result":{"items":[{"id":"interrupted-evt_18e9638a515cfeb8b","seq":1}]}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"items":[{"id":"interrupted-evt_18e9638a515cfeb8b","seq":2},{"id":"interrupted-evt_18e9638aaaf49288b","seq":3}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	all := string(join(out))
	if strings.Contains(all, "18e9638") || strings.Count(all, "evt_00000000000000001") != 2 || !strings.Contains(all, "evt_00000000000000002") {
		t.Errorf("%s (%v)", all, used)
	}
}
