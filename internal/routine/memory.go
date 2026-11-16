package routine

import (
	log "certainstats/internal/logger"
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// maybeLogMemory writes the opt-in memory line. With MemoryLog off it does
// nothing, not even ReadMemStats.
func (e *Routine) maybeLogMemory() {
	if !e.MemoryLog {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	var b strings.Builder
	fmt.Fprintf(&b, "[Maintenance] memory heap_inuse=%s heap_alloc=%s sys=%s next_gc=%s num_gc=%d goroutines=%d",
		mib(ms.HeapInuse), mib(ms.HeapAlloc), mib(ms.Sys), mib(ms.NextGC), ms.NumGC, runtime.NumGoroutine())
	if e.Cache != nil {
		fmt.Fprintf(&b, " realtime_agents=%d", e.Cache.Len())
	}
	if e.MemoryStats != nil {
		stats := e.MemoryStats()
		keys := make([]string, 0, len(stats))
		for k := range stats {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, " %s=%d", k, stats[k])
		}
	}
	log.Printf("%s", b.String())
}

func mib(n uint64) string { return fmt.Sprintf("%.1fMiB", float64(n)/(1<<20)) }
