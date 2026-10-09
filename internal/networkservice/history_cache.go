package networkservice

import (
	nm "certainstats/internal/networkmonitor"
	api "certainstats/internal/response"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxConcurrentBuilds bounds distinct history responses built at once.
const maxConcurrentBuilds = 32

// historyKey identifies one monitor's history response. Absolute bounds are
// exact; relative requests share a key for historyTTL even though their
// resolved wall time advances. The data revision isolates responses from
// newer commits.
func (s *Service) historyKey(userID string, m nm.Monitor, start, end int64, relative bool) string {
	revision := uint64(0)
	if s.Cache != nil {
		revision = s.Cache.NetworkRevision(networkKey(m))
	}
	window := "absolute_" + strconv.FormatInt(start, 10) + "_" + strconv.FormatInt(end, 10)
	if relative {
		window = "relative_" + strconv.FormatInt((end-start)/time.Hour.Milliseconds(), 10)
	}
	// Target and agent name are part of the response body.
	return strings.Join([]string{
		"network", userID, m.AgentID, m.ID, window,
		strconv.FormatUint(revision, 10),
		strconv.Quote(m.Target), strconv.Quote(m.AgentName),
	}, "_")
}

// beginHistory elects one builder per key. Other requests wait for it and are
// then served again through History; the returned bool is false for them.
func (s *Service) beginHistory(w http.ResponseWriter, r *http.Request, key string) (func(), bool) {
	s.buildMu.Lock()
	if done := s.builds[key]; done != nil {
		s.buildMu.Unlock()
		select {
		case <-done:
			s.History(w, r)
		case <-r.Context().Done():
		}
		return nil, false
	}
	if s.builds == nil {
		s.builds = make(map[string]chan struct{})
	}
	if len(s.builds) >= maxConcurrentBuilds {
		s.buildMu.Unlock()
		api.Error(w, http.StatusServiceUnavailable, "Query capacity unavailable")
		return nil, false
	}
	done := make(chan struct{})
	s.builds[key] = done
	s.buildMu.Unlock()

	finish := func() {
		s.buildMu.Lock()
		delete(s.builds, key)
		close(done)
		s.buildMu.Unlock()
	}
	return finish, true
}
