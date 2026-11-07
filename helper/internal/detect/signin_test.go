package detect

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/wire"
)

// What each real sign-in capture carries besides its title.
func TestSignInFixtures(t *testing.T) {
	for name, want := range map[string]wire.SignIn{
		"real-amp-login":        {URL: "https://ampcode.com/auth/cli/device?user_code=QRLJ-CBJZ", Code: "QRLJ-CBJZ"},
		"real-kimi-login":       {URL: "https://www.kimi.ai/code/authorize_device?user_code=8S0V-Z822", Code: "8S0V-Z822"},
		"real-crush-hyper-code": {URL: "https://hyper.charm.land/device", Code: "D2QN-PKMZ"},
		"real-opencode-api-key": {Secret: true},
		"real-cursor-login":     {URL: "https://cursor.com/loginDeepControl?challenge=XJZ_mc1EQE-Tm3cL6m02de9HPXporIvStRemKCUX_2k&uuid=41acd3ec-700c-499d-8eda-04567e7e36b1&mode=login&redirectTarget=cli"},
	} {
		p := mustDetect(t, fixture(t, "signin/"+name))
		if p.SignIn == nil || *p.SignIn != want {
			t.Errorf("%s: sign-in = %+v, want %+v", name, p.SignIn, want)
		}
	}
	for _, name := range []string{"real-claude-auth-login", "real-gemini-no-browser"} {
		p := mustDetect(t, fixture(t, "signin/"+name))
		if p.SignIn == nil || !p.SignIn.Secret || p.SignIn.URL == "" {
			t.Errorf("%s: sign-in = %+v, want a page and a secret", name, p.SignIn)
		}
	}
}

func TestSignInNegatives(t *testing.T) {
	for _, screen := range []string{
		// Agent output quoting a sign-in link, input box below.
		"The docs are at https://github.com/login/device and explain it.\n╭────╮\n│ >  │\n╰────╯\n",
		"Press any key to continue:\n",
		"Here is the code:\n",
		// A code-shaped token without the word code.
		"Build ABCD-1234 finished\nhttps://example.com/oauth/authorize?client_id=x\n$ \n",
		// A plain link that is not a sign-in page.
		"Waiting for the server\nhttps://example.com/docs/start\n",
	} {
		if p, ok := Detect(screen); ok {
			t.Errorf("%q: detected %+v", screen, p)
		}
	}
}

func TestWrappedURLJoins(t *testing.T) {
	line := "https://example.com/oauth/authorize?client_id=abc&state=0123456789012345678901234567890123456789"
	screen := "Open this link to sign in:\n" + line + "\n" + "&scope=read-write\nWaiting...\n"
	p := mustDetect(t, screen)
	if p.SignIn == nil || p.SignIn.URL != line+"&scope=read-write" {
		t.Fatalf("url = %+v", p.SignIn)
	}
}
