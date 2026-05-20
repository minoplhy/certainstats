package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"

	"database/sql"

	_ "modernc.org/sqlite"
)

// This tool deletes stale Beszel I/O metrics from TSDB.
//
// Before the IO normalization fix, Beszel stored:
//   - Network: cumulative total bytes (should have been delta)
//   - Disk: B/s rate (should have been delta)
//
// These old values are misinterpreted by the new frontend, showing
// absurd GB/s readings. This tool removes them so fresh, correct
// data can be collected.
//
// Usage: go run ./cmd/cleanup-beszel-io [DATA_DIR]
//   DATA_DIR defaults to /app/data

func main() {
	dataDir := "/app/data"
	if len(os.Args) > 1 {
		dataDir = os.Args[1]
	}

	// 1. Find Beszel agent IDs from SQLite
	dbPath := filepath.Join(dataDir, "agent_state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("sqlite open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT agent_id FROM agents WHERE agent_type = 'beszel'")
	if err != nil {
		log.Fatalf("query agents: %v", err)
	}

	var beszelIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			log.Fatalf("scan: %v", err)
		}
		beszelIDs = append(beszelIDs, id)
	}
	rows.Close()

	if len(beszelIDs) == 0 {
		fmt.Println("No Beszel agents found. Nothing to clean.")
		return
	}
	fmt.Printf("Found %d Beszel agent(s): %v\n", len(beszelIDs), beszelIDs)

	// 2. Open TSDB (server must be stopped!)
	tsdbPath := filepath.Join(dataDir, "tsdb")
	fmt.Printf("Opening TSDB at %s ...\n", tsdbPath)
	fmt.Println("⚠  Make sure the CertainStats server is STOPPED before running this.")

	opts := tsdb.DefaultOptions()
	opts.RetentionDuration = 0
	tdb, err := tsdb.Open(tsdbPath, nil, nil, opts, nil)
	if err != nil {
		log.Fatalf("tsdb open: %v", err)
	}
	defer tdb.Close()

	// 3. Delete affected metrics for each Beszel agent
	metricsToDelete := []string{
		"agent_rx_bytes",
		"agent_tx_bytes",
		"agent_disk_read_bytes",
		"agent_disk_write_bytes",
	}

	ctx := context.Background()
	cleaned := 0

	for _, agentID := range beszelIDs {
		for _, metric := range metricsToDelete {
			nameMatcher, err := labels.NewMatcher(labels.MatchEqual, "__name__", metric)
			if err != nil {
				log.Fatalf("matcher: %v", err)
			}
			agentMatcher, err := labels.NewMatcher(labels.MatchEqual, "agent_id", agentID)
			if err != nil {
				log.Fatalf("matcher: %v", err)
			}

			// Delete all data points from epoch to far future
			if err := tdb.Delete(ctx, 0, 4102444800000, nameMatcher, agentMatcher); err != nil {
				log.Printf("  WARN: delete %s for %s: %v", metric, agentID, err)
				continue
			}
			fmt.Printf("  ✓ Deleted %s for agent %s\n", metric, agentID)
			cleaned++
		}
	}

	if cleaned == 0 {
		fmt.Println("\nNo matching series found to delete.")
	} else {
		fmt.Printf("\n✓ Cleaned %d metric series across %d Beszel agent(s).\n", cleaned, len(beszelIDs))
		fmt.Println("  New correct data will appear on next agent submission.")
	}
}
