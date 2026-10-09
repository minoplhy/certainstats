package notifications

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func ValidateWebhook(destination string) error {
	u, e := url.Parse(destination)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return fmt.Errorf("webhook must be an HTTP(S) URL without embedded credentials")
	}
	return nil
}
func webhookClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many webhook redirects")
		}
		if err := ValidateWebhook(req.URL.String()); err != nil {
			return err
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("webhook redirect changed host")
		}
		return nil
	}}
}
