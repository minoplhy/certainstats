package agent

import "testing"

func TestLTstatsPanelPathWarning(t *testing.T) {
	for _, c := range []struct {
		panelPath string
		want      bool
	}{{"", false}, {"/", false}, {"/admin", true}, {"/admin/", true}} {
		got := false
		for _, m := range getSetupInstructions("ltstats", "tok", "example.com", c.panelPath, "") {
			if m.MessageType == "warning" {
				got = true
			}
		}
		if got != c.want {
			t.Errorf("panelPath %q: warning=%v, want %v", c.panelPath, got, c.want)
		}
	}
}
