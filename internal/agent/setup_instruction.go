package agent

import (
	base "certainstats/internal/base/agent"
	"fmt"
	"strings"
)

func getSetupInstructions(agentType, token, host, panelPath, publicKey string) []base.ProvisionMessage {
	scheme := "https" // TODO detect this from config
	var messages []base.ProvisionMessage

	// Normalize panelPath
	panelPath = strings.TrimSuffix(panelPath, "/")
	if panelPath != "" && !strings.HasPrefix(panelPath, "/") {
		panelPath = "/" + panelPath
	}

	hubBaseURL := fmt.Sprintf("%s://%s%s", scheme, host, panelPath)

	switch agentType {
	case "beszel":
		messages = append(messages, base.ProvisionMessage{
			Name:        "Hub URL",
			MessageType: "copy",
			Content:     hubBaseURL,
		})
		messages = append(messages, base.ProvisionMessage{
			Name:        "Token",
			MessageType: "copy",
			Content:     token,
		})
		messages = append(messages, base.ProvisionMessage{
			Name:        "Hub Public Key",
			MessageType: "copy",
			Content:     publicKey,
		})
		messages = append(messages, base.ProvisionMessage{
			Name:        "Linux Command",
			MessageType: "command",
			Content:     fmt.Sprintf("curl -sL https://get.beszel.dev -o /tmp/install-agent.sh && chmod +x /tmp/install-agent.sh && /tmp/install-agent.sh -p 45876 -k \"%s\" -t \"%s\" -url \"%s\"", publicKey, token, hubBaseURL),
		})

		messages = append(messages, base.ProvisionMessage{
			Name:        "docker-compose.yml",
			MessageType: "big_copy",
			Description: "Deployment configuration for Beszel Agent",
			Content: fmt.Sprintf(`services:
  beszel-agent:
    image: henrygd/beszel-agent
    container_name: beszel-agent
    restart: unless-stopped
    network_mode: host
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./beszel_agent_data:/var/lib/beszel-agent
      # monitor other disks / partitions by mounting a folder in /extra-filesystems
      # - /mnt/disk/.beszel:/extra-filesystems/sda1:ro
    environment:
      LISTEN: 45876
      KEY: "%s"
      TOKEN: "%s"
      HUB_URL: "%s"`,
				publicKey, token, hubBaseURL),
		})

	case "ltstats":
		messages = append(messages, base.ProvisionMessage{
			Name:        "Token",
			MessageType: "copy",
			Content:     token,
		})
		messages = append(messages, base.ProvisionMessage{
			Name:        "Linux command(Debian-based)",
			MessageType: "command",
			Content:     fmt.Sprintf("curl -s https://ltstats.de/v1.3/systemd:agent | tee install.sh | sha256sum -c <(echo 123bdcc123d39dfe915eb3ed9223ea75c845a8bef5c79b994f4b2de20530085c -) && bash install.sh %s %s ntp", host, token),
		})

		if panelPath != "/" {
			messages = append(messages, base.ProvisionMessage{
				Name:        "Warning",
				MessageType: "warning",
				Content:     "LTstats does not support non-default panel path!",
			})

		}
	case "hetrixtools":
		messages = append(messages, base.ProvisionMessage{
			Name:        "Token",
			MessageType: "copy",
			Content:     token,
		})
		messages = append(messages, base.ProvisionMessage{
			Name:        "Linux Command(Debian-based)",
			MessageType: "command",
			Content: fmt.Sprintf(`wget -4 -qO- https://raw.githubusercontent.com/hetrixtools/agent/master/hetrixtools_install.sh \
| sed -e 's|https://sm.hetrixtools.net/|%s/submit|g' \
      -e '/Fetching the agent/,/\.\.\. done\./{/\.\.\. done\./a sed -i '\''s|https://sm.hetrixtools.net/v2/|%s/submit|g'\'' /etc/hetrixtools/hetrixtools_agent.sh
}' \
| sudo bash -s -- %s 0 0 0 0 0 0`, hubBaseURL, hubBaseURL, token),
		})

	default:
		messages = append(messages, base.ProvisionMessage{
			Name:        "Connection Token",
			MessageType: "copy",
			Content:     token,
		})
		messages = append(messages, base.ProvisionMessage{
			Name:        "Note",
			MessageType: "note",
			Content:     "Generic agent type. Use the token provided.",
		})
	}
	return messages
}

func getUninstallInstructions(agentType, token string) []base.ProvisionMessage {
	var messages []base.ProvisionMessage

	switch agentType {
	case "beszel":
		messages = append(messages, base.ProvisionMessage{
			Name:        "Linux Command",
			MessageType: "command",
			Content:     "curl -sL https://get.beszel.dev -o /tmp/install-agent.sh && chmod +x /tmp/install-agent.sh && /tmp/install-agent.sh -u",
		})
	case "ltstats":
		messages = append(messages, base.ProvisionMessage{
			Name:        "Linux Command",
			MessageType: "command",
			Content:     "systemctl disable --now ltstats_agent; rm /etc/systemd/system/ltstats_agent.service /etc/monitoring_token /bin/ltstats_agent\nsystemctl disable --now ltstats_ntp; rm /etc/systemd/system/ltstats_ntp.service /bin/ltstats_ntp",
		})
	case "hetrixtools":
		messages = append(messages, base.ProvisionMessage{
			Name:        "Linux Command",
			MessageType: "command",
			Content:     fmt.Sprintf("wget -4 -qO- https://raw.githubusercontent.com/hetrixtools/agent/master/hetrixtools_uninstall.sh | sudo bash -s %s", token),
		})
	default:
		messages = append(messages, base.ProvisionMessage{
			Name:        "Note",
			MessageType: "note",
			Content:     fmt.Sprintf("No specific uninstall command for agent type: %s", agentType),
		})
	}
	return messages
}
