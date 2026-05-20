package agent

import (
	resp "certainstats/internal/base/response"
	ctx "certainstats/internal/context"
	"certainstats/internal/metrics"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"net/http"
	"time"
)

func ListAgentsHandler(agents store.AgentStore, cache *metrics.RealtimeCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		list, err := agents.AgentList(r.Context(), userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Convert store.Agent → JSON shape (LastSeen as formatted string pointer)
		out := make([]resp.Agent, len(list))
		for i, a := range list {
			out[i] = resp.Agent{
				AgentID: a.AgentID, AgentType: a.AgentType, Nickname: a.Nickname,
				IsOnline: a.IsOnline, Uptime: a.Uptime,
				LinuxVersion: a.LinuxVersion, CpuModel: a.CpuModel,
				CpuCores: a.CpuCores, RamSize: a.RamSize,
				SwapSize: a.SwapSize, DiskSize: a.DiskSize,
				Net: resp.NetOdometer{
					TotalRxBytes: func(v uint64) *uint64 { return &v }(a.TotalRxBytes),
					TotalTxBytes: func(v uint64) *uint64 { return &v }(a.TotalTxBytes),
				},
				Disks: func() []resp.DiskOdometer {
					disks := make([]resp.DiskOdometer, len(a.Disks))
					for j, d := range a.Disks {
						rVal := d.ReadBytes
						wVal := d.WriteBytes
						disks[j] = resp.DiskOdometer{
							Path:       d.Path,
							ReadBytes:  &rVal,
							WriteBytes: &wVal,
						}
					}
					return disks
				}(),
			}
			if a.LastSeen != nil {
				s := a.LastSeen.Format(time.RFC3339)
				out[i].LastSeen = &s
			}
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, out)
	}
}
