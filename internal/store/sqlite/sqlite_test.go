package sqlite

import (
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/store"
	"context"
	"path/filepath"
	"testing"
	"time"

	b "certainstats/internal/base/agent"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
	})

	return s
}

func TestUserAndSessionStore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 1. IsUserZero check
	isZero, err := s.IsUserZero(ctx)
	if err != nil {
		t.Fatalf("IsUserZero error: %v", err)
	}
	if !isZero {
		t.Fatalf("expected IsUserZero true for empty database")
	}

	// 2. Create User
	userID := "usr_test123"
	username := "admin_test"
	passHash := "$2a$10$abcdef"
	err = s.CreateUser(ctx, userID, username, passHash, true)
	if err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}

	// Verify IsUserZero is false now
	isZero, err = s.IsUserZero(ctx)
	if err != nil || isZero {
		t.Fatalf("expected IsUserZero false after creating user")
	}

	// Get User by Username
	u, err := s.GetByUsername(ctx, username)
	if err != nil {
		t.Fatalf("GetByUsername error: %v", err)
	}
	if u.UserID != userID || u.Username != username {
		t.Fatalf("user mismatch: got %+v", u)
	}

	// 3. Session Store Operations
	token := "sess_tok_123456"
	now := time.Now().Truncate(time.Second)
	expiresAt := now.Add(24 * time.Hour)

	err = s.SessionCreate(ctx, store.Session{
		Token:           token,
		UserID:          userID,
		ExpiresAt:       expiresAt,
		CreatedAt:       now,
		LastConnectedAt: now,
		IPAddress:       "127.0.0.1",
		UserAgent:       "GoTest",
	})
	if err != nil {
		t.Fatalf("SessionCreate error: %v", err)
	}

	sess, err := s.SessionGet(ctx, token)
	if err != nil {
		t.Fatalf("SessionGet error: %v", err)
	}
	if sess.UserID != userID || sess.Token != token {
		t.Fatalf("session mismatch: got %+v", sess)
	}

	// Update Activity
	newActivity := now.Add(5 * time.Minute)
	err = s.SessionUpdateActivity(ctx, token, newActivity)
	if err != nil {
		t.Fatalf("SessionUpdateActivity error: %v", err)
	}

	// List Sessions
	sessions, err := s.SessionListByUser(ctx, userID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("SessionListByUser error: %v, count: %d", err, len(sessions))
	}

	// Delete Session
	err = s.SessionDelete(ctx, token)
	if err != nil {
		t.Fatalf("SessionDelete error: %v", err)
	}

	_, err = s.SessionGet(ctx, token)
	if err == nil {
		t.Fatalf("expected error getting deleted session")
	}
}

func TestAgentStoreOperations(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Setup user first
	userID := "usr_agent_owner"
	_ = s.CreateUser(ctx, userID, "agent_owner", "hash", true)

	// 1. Provision Agent
	agentID := "ag_test_001"
	agentNickname := "Test Agent"
	token := "tok_secret_999"
	agentType := "beszel"

	err := s.AgentProvision(ctx, agentID, userID, token, agentNickname, agentType)
	if err != nil {
		t.Fatalf("AgentProvision error: %v", err)
	}

	// Lookup Agent by Token
	identity, err := s.AgentGetByToken(ctx, token)
	if err != nil {
		t.Fatalf("AgentGetByToken error: %v", err)
	}
	if identity.AgentID != agentID || identity.UserID != userID {
		t.Fatalf("identity mismatch: got %+v", identity)
	}

	// Upsert Details & Heartbeat
	err = s.AgentUpsertDetails(ctx, store.Agent{
		AgentID:      agentID,
		UserID:       userID,
		Uptime:       3600,
		LinuxVersion: "Linux 6.8",
		CpuModel:     "AMD EPYC",
		CpuCores:     8,
		RamSize:      16000000000,
		SwapSize:     2000000000,
		DiskSize:     100000000000,
	})
	if err != nil {
		t.Fatalf("AgentUpsertDetails error: %v", err)
	}

	// Increment Traffic
	disks := []store.DiskDelta{
		{Path: "/", ReadBytes: 1024, WriteBytes: 2048},
	}
	err = s.AgentIncrementTraffic(ctx, agentID, userID, 5000, 7000, disks)
	if err != nil {
		t.Fatalf("AgentIncrementTraffic error: %v", err)
	}

	// Agent List
	agents, err := s.AgentList(ctx, userID)
	if err != nil || len(agents) != 1 {
		t.Fatalf("AgentList error: %v, count: %d", err, len(agents))
	}

	// Update Agent Nickname
	newNick := "Renamed Agent"
	err = s.AgentUpdate(ctx, agentID, userID, &newNick, nil)
	if err != nil {
		t.Fatalf("AgentUpdate error: %v", err)
	}

	// Delete Agent
	err = s.AgentDelete(ctx, agentID, userID)
	if err != nil {
		t.Fatalf("AgentDelete error: %v", err)
	}

	agents, err = s.AgentList(ctx, userID)
	if err != nil || len(agents) != 0 {
		t.Fatalf("expected 0 agents after delete, got %d", len(agents))
	}
}

func TestAgentStoreOperations_MultiDisk(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	userID := "usr_multidisk_owner"
	_ = s.CreateUser(ctx, userID, "multidisk_owner", "hash", true)

	agentID := "ag_multidisk_001"
	token := "tok_multidisk_123"
	err := s.AgentProvision(ctx, agentID, userID, token, "MultiDisk Host", "beszel")
	if err != nil {
		t.Fatalf("AgentProvision error: %v", err)
	}

	// Increment traffic for multiple disk mount paths
	multiDisks := []store.DiskDelta{
		{Path: "/", ReadBytes: 1000, WriteBytes: 2000},
		{Path: "/home", ReadBytes: 3000, WriteBytes: 4000},
		{Path: "/mnt/storage", ReadBytes: 5000, WriteBytes: 6000},
	}

	err = s.AgentIncrementTraffic(ctx, agentID, userID, 10000, 20000, multiDisks)
	if err != nil {
		t.Fatalf("AgentIncrementTraffic error: %v", err)
	}

	// List agents and check disk odometers
	agents, err := s.AgentList(ctx, userID)
	if err != nil || len(agents) != 1 {
		t.Fatalf("AgentList error: %v, count: %d", err, len(agents))
	}

	disks := agents[0].Disks
	if len(disks) != 3 {
		t.Fatalf("expected 3 disk odometers, got %d", len(disks))
	}

	diskMap := make(map[string]b.DiskOdometer)
	for _, d := range disks {
		diskMap[d.Path] = d
	}

	if rootDisk, ok := diskMap["/"]; !ok || rootDisk.ReadBytes != 1000 || rootDisk.WriteBytes != 2000 {
		t.Errorf("unexpected root disk odometer: %+v", rootDisk)
	}
	if homeDisk, ok := diskMap["/home"]; !ok || homeDisk.ReadBytes != 3000 || homeDisk.WriteBytes != 4000 {
		t.Errorf("unexpected /home disk odometer: %+v", homeDisk)
	}
	if storageDisk, ok := diskMap["/mnt/storage"]; !ok || storageDisk.ReadBytes != 5000 || storageDisk.WriteBytes != 6000 {
		t.Errorf("unexpected /mnt/storage disk odometer: %+v", storageDisk)
	}
}

func TestAgentDiskOdometer_TotalBytes_SQLCondition(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	userID := "usr_totalbytes_owner"
	_ = s.CreateUser(ctx, userID, "tb_owner", "hash", true)

	agentID := "ag_tb_001"
	token := "tok_tb_123"
	err := s.AgentProvision(ctx, agentID, userID, token, "Disk Resize Host", "beszel")
	if err != nil {
		t.Fatalf("AgentProvision error: %v", err)
	}

	const initial50GB uint64 = 50 * 1024 * 1024 * 1024
	const resized100GB uint64 = 100 * 1024 * 1024 * 1024

	// Step 1: Initial Insert with 50 GB
	step1Disks := []store.DiskDelta{
		{Path: "/", TotalBytes: initial50GB, ReadBytes: 1000, WriteBytes: 2000},
		{Path: "/data", TotalBytes: initial50GB * 2, ReadBytes: 3000, WriteBytes: 4000},
	}
	if err := s.AgentIncrementTraffic(ctx, agentID, userID, 100, 200, step1Disks); err != nil {
		t.Fatalf("step 1 AgentIncrementTraffic error: %v", err)
	}

	ag, err := s.AgentGetByID(ctx, agentID, userID)
	if err != nil {
		t.Fatalf("AgentGetByID error: %v", err)
	}
	if len(ag.Disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(ag.Disks))
	}
	for _, d := range ag.Disks {
		if d.Path == "/" && (d.TotalBytes != initial50GB || d.ReadBytes != 1000 || d.WriteBytes != 2000) {
			t.Errorf("step 1 root disk mismatch: %+v", d)
		}
		if d.Path == "/data" && (d.TotalBytes != initial50GB*2 || d.ReadBytes != 3000 || d.WriteBytes != 4000) {
			t.Errorf("step 1 /data disk mismatch: %+v", d)
		}
	}

	// Step 2: Incremental tick with same total_bytes (Should NOT overwrite total_bytes, accumulates IO)
	step2Disks := []store.DiskDelta{
		{Path: "/", TotalBytes: initial50GB, ReadBytes: 500, WriteBytes: 500},
		{Path: "/data", TotalBytes: initial50GB * 2, ReadBytes: 500, WriteBytes: 500},
	}
	if err := s.AgentIncrementTraffic(ctx, agentID, userID, 50, 50, step2Disks); err != nil {
		t.Fatalf("step 2 AgentIncrementTraffic error: %v", err)
	}

	ag, _ = s.AgentGetByID(ctx, agentID, userID)
	for _, d := range ag.Disks {
		if d.Path == "/" && (d.TotalBytes != initial50GB || d.ReadBytes != 1500 || d.WriteBytes != 2500) {
			t.Errorf("step 2 root disk mismatch: %+v", d)
		}
	}

	// Step 3: Incremental tick with total_bytes = 0 (Must NOT wipe total_bytes, preserves existing)
	step3Disks := []store.DiskDelta{
		{Path: "/", TotalBytes: 0, ReadBytes: 100, WriteBytes: 100},
	}
	if err := s.AgentIncrementTraffic(ctx, agentID, userID, 10, 10, step3Disks); err != nil {
		t.Fatalf("step 3 AgentIncrementTraffic error: %v", err)
	}

	ag, _ = s.AgentGetByID(ctx, agentID, userID)
	for _, d := range ag.Disks {
		if d.Path == "/" && (d.TotalBytes != initial50GB || d.ReadBytes != 1600 || d.WriteBytes != 2600) {
			t.Errorf("step 3 zero-total root disk wipe protection failed: %+v", d)
		}
	}

	// Step 4: Disk Resize Event: total_bytes changes from 50 GB to 100 GB
	step4Disks := []store.DiskDelta{
		{Path: "/", TotalBytes: resized100GB, ReadBytes: 200, WriteBytes: 200},
	}
	if err := s.AgentIncrementTraffic(ctx, agentID, userID, 20, 20, step4Disks); err != nil {
		t.Fatalf("step 4 AgentIncrementTraffic error: %v", err)
	}

	ag, _ = s.AgentGetByID(ctx, agentID, userID)
	for _, d := range ag.Disks {
		if d.Path == "/" && (d.TotalBytes != resized100GB || d.ReadBytes != 1800 || d.WriteBytes != 2800) {
			t.Errorf("step 4 disk resize update to 100GB failed: %+v", d)
		}
	}
}

func TestDashboardStoreOperations(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	userID := "usr_dash_owner"
	_ = s.CreateUser(ctx, userID, "dash_owner", "hash", true)

	// Create Dashboard
	dashID := "dash_001"
	slug := "main-ops"
	title := "Main Operations"

	err := s.DashboardCreate(ctx, store.Dashboard{
		DashboardID: dashID,
		UserID:      userID,
		Slug:        slug,
		Title:       title,
		AccessRules: accessrules.AccessRules{},
	})
	if err != nil {
		t.Fatalf("DashboardCreate error: %v", err)
	}

	// Get Dashboard Info
	d, err := s.DashboardGetInfo(ctx, dashID, userID)
	if err != nil {
		t.Fatalf("DashboardGetInfo error: %v", err)
	}
	if d.Title != title || d.Slug != slug {
		t.Fatalf("dashboard mismatch: %+v", d)
	}

	// Delete Dashboard
	err = s.DashboardDelete(ctx, dashID, userID)
	if err != nil {
		t.Fatalf("DashboardDelete error: %v", err)
	}
}
