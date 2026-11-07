package spec

import "testing"

func TestPasswordEnvChecked(t *testing.T) {
	base := "adapter: x\nname: X\nrevision: 1\nmatch: { agents: [x] }\ncapabilities: []\n"
	for ep, ok := range map[string]bool{
		"endpoint: { kind: http, password_env: X_SERVER_PASSWORD, username: x }\n": true,
		"endpoint: { kind: http, password_env: LD_PRELOAD, username: x }\n":        false,
		"endpoint: { kind: http, password_env: X_PASSWORD }\n":                     false,
		"endpoint: { kind: unix, password_env: X_PASSWORD, username: x }\n":        false,
		"endpoint: { kind: http, password_env: X_PASSWORD, username: 'a:b' }\n":    false,
	} {
		_, err := Parse([]byte(base + ep))
		if (err == nil) != ok {
			t.Errorf("%s: err %v", ep, err)
		}
	}
}
