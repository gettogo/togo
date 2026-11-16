package detect

import "testing"

// What an agent writes in a reply is not a request to sign in, whatever
// ticket keys, links and words it holds.
func TestReplyTextIsNotASignIn(t *testing.T) {
	for _, screen := range []string{
		"● Updated the code for PROJ-1234 and confirmed the tests pass.\n\n> \n",
		"● Open http://localhost:3000/login to confirm it works.\n\n> \n",
		"⏺ I fixed the login page. Visit https://example.com/auth/callback to confirm.\n\n> \n",
		"The code for PROJ-1234 is merged, waiting on review.\n\n> \n",
		"• Renamed task-management-service; primary_key=True stays. 100%! done\n\n> \n",
	} {
		if p, ok := Detect(screen); ok && p.SignIn != nil {
			t.Errorf("%q read as sign-in %+v", screen, p.SignIn)
		}
	}
}

// A device code shaped like a ticket key still counts next to its page.
func TestTicketShapedCodeCountsWithItsPage(t *testing.T) {
	screen := "Open https://github.com/login/device in your browser\nand enter the code: ABCD-1234\n\nWaiting for authorization...\n"
	p, ok := Detect(screen)
	if !ok || p.SignIn == nil || p.SignIn.Code != "ABCD-1234" {
		t.Fatalf("got %v %+v, want the code with its page", ok, p.SignIn)
	}
}
