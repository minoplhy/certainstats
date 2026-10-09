package ws

import (
	"errors"
	"net/http"
	"strings"

	"golang.org/x/net/websocket"
)

const BrowserProtocol = "certainstats.protobuf.v1"

func ProtobufEnabled(value string) bool { return value == "true" }

func requireBrowserProtocol(w http.ResponseWriter, r *http.Request, protobuf bool) bool {
	if !protobuf {
		if len(r.Header.Values("Sec-WebSocket-Protocol")) == 0 {
			return true
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "JSON WebSockets do not accept subprotocols. Reload the page.", http.StatusUpgradeRequired)
		return false
	}
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, offered := range strings.Split(header, ",") {
			if strings.TrimSpace(offered) == BrowserProtocol {
				return true
			}
		}
	}
	w.Header().Set("Sec-WebSocket-Protocol", BrowserProtocol)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Browser protocol unsupported. Reload the page.", http.StatusUpgradeRequired)
	return false
}

func selectBrowserProtocol(config *websocket.Config, protobuf bool) error {
	if !protobuf {
		if len(config.Protocol) != 0 {
			return errors.New("JSON WebSockets do not accept subprotocols")
		}
		config.Protocol = nil
		return nil
	}
	for _, offered := range config.Protocol {
		if offered == BrowserProtocol {
			config.Protocol = []string{BrowserProtocol}
			return nil
		}
	}
	return errors.New("unsupported browser WebSocket protocol")
}
