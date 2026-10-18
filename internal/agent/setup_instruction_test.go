package agent

import "testing"

func TestLTstatsPanelPathWarning(t *testing.T) {
	for _, c := range []struct {
		panelPath string
		want      bool
	}{{"", false}, {"/", false}, {"/admin", true}, {"/admin/", true}} {
		got := false
		for _, m := range getSetupInstructions("ltstats", "tok", "https", "example.com", c.panelPath, "") {
			if m.MessageType == "warning" {
				got = true
			}
		}
		if got != c.want {
			t.Errorf("panelPath %q: warning=%v, want %v", c.panelPath, got, c.want)
		}
	}
}

func TestBeszelHubURLUsesRequestScheme(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		msgs := getSetupInstructions("beszel", "tok", scheme, "hub.example:8080", "/", "key")
		if got, want := msgs[0].Content, scheme+"://hub.example:8080"; got != want {
			t.Errorf("hub URL = %q, want %q", got, want)
		}
	}
}
