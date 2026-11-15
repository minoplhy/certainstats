package crowdsec

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"certainstats/internal/crowdsec/wire"
)

// credentials is the file `cscli machines add … -f` writes.
type credentials struct {
	URL, Login, Password string
}

// readCredentials parses the flat "key: value" YAML that cscli writes, so the
// collector needs no YAML library.
func readCredentials(path string) (credentials, error) {
	var c credentials
	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("cannot read the Local API credentials file %s (create it with cscli machines add certainstats --auto -f %s): %w", path, path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		switch strings.TrimSpace(key) {
		case "url":
			c.URL = strings.TrimRight(value, "/")
		case "login":
			c.Login = value
		case "password":
			c.Password = value
		}
	}
	if err := sc.Err(); err != nil {
		return c, err
	}
	if c.URL == "" || c.Login == "" || c.Password == "" {
		return c, fmt.Errorf("%s must contain url, login and password", path)
	}
	if _, err := url.Parse(c.URL); err != nil {
		return c, fmt.Errorf("%s: invalid url: %w", path, err)
	}
	return c, nil
}

// lapi is a read-only client for CrowdSec's Local API, logged in as a machine.
type lapi struct {
	creds     credentials
	http      *http.Client
	userAgent string
	mu        sync.Mutex
	token     string
	expires   time.Time
	// rfc3339 is set when this Local API rejected duration-style since/until values.
	rfc3339 bool
}

var errUnauthorized = errors.New("Local API rejected the machine login")

// login returns a valid JWT, logging in when there is none. The lock only guards
// the token; it is never held during the request. Two logins at once (--check
// racing a pass) are harmless: the last token stored wins.
func (c *lapi) login(ctx context.Context) (string, error) {
	c.mu.Lock()
	token, valid := c.token, c.token != "" && time.Now().Before(c.expires)
	c.mu.Unlock()
	if valid {
		return token, nil
	}
	// Marshalling a map of strings cannot fail.
	body, _ := json.Marshal(map[string]any{"machine_id": c.creds.Login, "password": c.creds.Password, "scenarios": []string{}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.creds.URL+"/v1/watchers/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach the Local API at %s: %w", c.creds.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("%w (%s); check login and password, and that the machine is validated (cscli machines list)", errUnauthorized, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Local API login: %s", resp.Status)
	}
	var out struct {
		Token  string `json:"token"`
		Expire string `json:"expire"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || out.Token == "" {
		return "", fmt.Errorf("Local API login: unexpected response")
	}
	expires := time.Now().Add(30 * time.Minute)
	if t, err := time.Parse(time.RFC3339, out.Expire); err == nil {
		expires = t.Add(-time.Minute)
	}
	c.mu.Lock()
	c.token, c.expires = out.Token, expires
	c.mu.Unlock()
	return out.Token, nil
}

func (c *lapi) dropToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// timeParam formats a window edge. CrowdSec reads since/until as a duration
// before now; the timestamp form is a fallback for a Local API that wants one.
func (c *lapi) timeParam(t, now time.Time) string {
	if c.rfc3339 {
		return t.UTC().Format(time.RFC3339)
	}
	return strconv.FormatInt(max(int64(now.Sub(t)/time.Second), 0), 10) + "s"
}

// alerts lists alerts that started between from and to, at most limit of them.
func (c *lapi) alerts(ctx context.Context, from, to time.Time, limit int) ([]wire.Alert, error) {
	list, status, err := c.get(ctx, from, to, limit)
	if status == http.StatusUnauthorized {
		c.dropToken()
		list, status, err = c.get(ctx, from, to, limit)
	}
	if (status == http.StatusBadRequest || status == http.StatusInternalServerError) && !c.rfc3339 {
		c.rfc3339 = true
		list, status, err = c.get(ctx, from, to, limit)
		if err != nil {
			c.rfc3339 = false
		}
	}
	return list, err
}

func (c *lapi) get(ctx context.Context, from, to time.Time, limit int) ([]wire.Alert, int, error) {
	token, err := c.login(ctx)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	q := url.Values{}
	q.Set("since", c.timeParam(from, now))
	if now.Sub(to) > time.Second {
		q.Set("until", c.timeParam(to, now))
	}
	q.Set("limit", strconv.Itoa(limit))
	// Community blocklist and subscribed lists arrive as alerts holding thousands of
	// decisions. They are not attacks seen here, so leave them out, as cscli does.
	q.Set("include_capi", "false")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.creds.URL+"/v1/alerts?"+q.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("cannot reach the Local API at %s: %w", c.creds.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The body only adds detail to the error; a failed read leaves it out.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, resp.StatusCode, fmt.Errorf("Local API alerts: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var list []wire.Alert
	// json drops events and meta, which hold raw log lines and never leave the host.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(&list); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("Local API alerts: %w", err)
	}
	return list, resp.StatusCode, nil
}
