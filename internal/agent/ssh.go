package agent

import (
	"certainstats/internal/store"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
	"strings"
)

// GenerateAndSaveSSH returns instructions only after the matching key persists.
func GenerateAndSaveSSH(ctx context.Context, agents store.AgentStore, agentID, userID string) (string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", err
	}
	public, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))
	err = agents.BeszelSSHSave(ctx, store.BeszelSSH{AgentID: agentID, PublicKey: key, PrivateKey: string(pem.EncodeToMemory(block))}, userID)
	return key, err
}
