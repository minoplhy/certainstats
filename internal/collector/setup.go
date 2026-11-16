package collector

import (
	"certainstats/internal/auth"
	base "certainstats/internal/base/agent"
	ctxkeys "certainstats/internal/context"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"database/sql"
	"errors"
	"html"
	"net/http"
	"strings"
)

// Install steps for a collector, in the provision message format the agent install
// modal already renders (base.ProvisionMessage).

const (
	releasesURL    = "https://github.com/minoplhy/certainstats/releases/latest"
	collectorImage = "ghcr.io/minoplhy/certainstats-collector:latest"
	tokenMissing   = "<reset the token in CertainStats>"
)

// Step is one host command a type needs before the collector can run.
type Step struct {
	Label, Command string
}

// TypeSetup is what a type adds to the install steps.
type TypeSetup struct {
	Env     [][2]string // the type's switch and its own settings
	Prepare []Step      // host commands, run before installing the collector
	Volumes []string    // Docker Compose mounts, host:container[:ro]
	Note    string      // plain text shown under the steps
}

// Setup is implemented by types that need more than their switch set to true.
type Setup interface {
	Setup() TypeSetup
}

func (s *Service) typeSetup(name string) TypeSetup {
	t := s.types[name]
	if st, ok := t.(Setup); ok {
		return st.Setup()
	}
	if t == nil {
		return TypeSetup{Env: [][2]string{{"COLLECT_" + strings.ToUpper(name), "true"}}}
	}
	return TypeSetup{Env: [][2]string{{SwitchOf(t), "true"}}}
}

// hubURL is the panel address the collector should dial, as the agent steps build it.
func hubURL(r *http.Request) string {
	scheme := "http"
	if auth.IsRequestSecure(r) {
		scheme = "https"
	}
	panel, _ := r.Context().Value(ctxkeys.PanelPathKey).(string)
	panel = strings.TrimSuffix(panel, "/")
	if panel != "" && !strings.HasPrefix(panel, "/") {
		panel = "/" + panel
	}
	return scheme + "://" + r.Host + panel
}

func command(name, content string) base.ProvisionMessage {
	return base.ProvisionMessage{Name: name, MessageType: "command", Content: content}
}

// instructions builds the install steps for a collector collecting types.
func (s *Service) instructions(token, hub string, types []string) []base.ProvisionMessage {
	var msgs []base.ProvisionMessage
	shownToken := token
	if token == "" {
		shownToken = tokenMissing
		msgs = append(msgs, base.ProvisionMessage{MessageType: "warning",
			Content: "This collector was created before tokens were kept, so its token can't be shown. Reset the token to see it here; the collector stays offline until its TOKEN setting is replaced."})
	}
	env := [][2]string{{"HUB_URL", hub}, {"TOKEN", shownToken}}
	var prepare []base.ProvisionMessage
	var volumes []string
	var notes []string
	for _, name := range types {
		ts := s.typeSetup(name)
		env = append(env, ts.Env...)
		for _, p := range ts.Prepare {
			prepare = append(prepare, command(p.Label, p.Command))
		}
		volumes = append(volumes, ts.Volumes...)
		if ts.Note != "" {
			notes = append(notes, ts.Note)
		}
	}

	msgs = append(msgs,
		base.ProvisionMessage{Name: "Hub URL", MessageType: "copy", Content: hub},
		base.ProvisionMessage{Name: "Token", MessageType: "copy", Content: shownToken})

	var envFile strings.Builder
	for _, kv := range env {
		envFile.WriteString(kv[0] + "=" + kv[1] + "\n")
	}
	systemd := append(append([]base.ProvisionMessage{}, prepare...),
		command("Install the collector (for arm64, use the linux-arm64 file from "+releasesURL+")",
			"curl -fsSL -o /tmp/certainstats-collector.tar.gz "+releasesURL+"/download/certainstats-collector-linux-amd64.tar.gz\n"+
				"tar -xzf /tmp/certainstats-collector.tar.gz -C /tmp\n"+
				"install -m 755 /tmp/certainstats-collector-linux-amd64 /usr/local/bin/certainstats-collector"),
		command("Write its settings", "mkdir -p /etc/certainstats\ninstall -m 600 /dev/null /etc/certainstats/collector.env\ncat > /etc/certainstats/collector.env <<'EOF'\n"+envFile.String()+"EOF"),
		command("Check every prerequisite", "set -a; . /etc/certainstats/collector.env; set +a\ncertainstats-collector --check"),
		command("Run it as a service", "cat > /etc/systemd/system/certainstats-collector.service <<'EOF'\n[Unit]\nDescription=CertainStats collector\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nEnvironmentFile=/etc/certainstats/collector.env\nExecStart=/usr/local/bin/certainstats-collector\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\nEOF\nsystemctl daemon-reload\nsystemctl enable --now certainstats-collector"))

	var compose strings.Builder
	compose.WriteString("services:\n  certainstats-collector:\n    image: " + collectorImage + "\n    restart: unless-stopped\n    environment:\n")
	for _, kv := range env {
		compose.WriteString("      " + kv[0] + ": \"" + kv[1] + "\"\n")
	}
	if len(volumes) > 0 {
		compose.WriteString("    volumes:\n")
		for _, v := range volumes {
			compose.WriteString("      - " + v + "\n")
		}
	}
	docker := append(append([]base.ProvisionMessage{}, prepare...),
		command("docker-compose.yml", compose.String()),
		command("Check every prerequisite", "docker compose run --rm certainstats-collector --check"),
		command("Start it", "docker compose up -d certainstats-collector\ndocker compose logs -f certainstats-collector"))

	msgs = append(msgs, base.ProvisionMessage{Name: "Installation Guide", MessageType: "tabs", Children: []base.ProvisionMessage{
		{Name: "systemd", MessageType: "tab", Children: systemd},
		{Name: "Docker Compose", MessageType: "tab", Children: docker},
	}})
	for _, n := range notes {
		// The renderer shows notes as HTML.
		msgs = append(msgs, base.ProvisionMessage{MessageType: "note", Content: html.EscapeString(n)})
	}
	return msgs
}

// install returns a collector's install steps, with its kept token.
func (s *Service) install(w http.ResponseWriter, r *http.Request) {
	c := FromRequest(r)
	token, err := s.Store.CollectorToken(r.Context(), c.CollectorID, userID(r))
	if errors.Is(err, sql.ErrNoRows) {
		apiresponse.Error(w, http.StatusNotFound, "Collector not found")
		return
	}
	if err != nil {
		log.Printf("[Collector] token: %v", err)
		apiresponse.Error(w, http.StatusInternalServerError, "Failed to load collector")
		return
	}
	var types []string
	for _, t := range c.Types {
		if t.Enabled {
			types = append(types, t.Type)
		}
	}
	apiresponse.JSON(w, http.StatusOK, map[string]any{
		"collector_id": c.CollectorID,
		"name":         c.Name,
		"messages":     s.instructions(token, hubURL(r), types),
		"message":      "Installation instructions generated.",
	})
}
