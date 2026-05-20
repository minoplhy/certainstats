package agentdata

import (
	"crypto/rand"
	"math/big"
)

// Generates a random alphanumeric string of exactly 'length' characters
func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

func GenerateAgentID() string {
	return "agt_" + GenerateRandomString(32)
}

func GenerateDeviceToken(agentType string) string {
	// Exactly 32 characters, leaving room for the null terminator in a [33]byte array
	if agentType == "ltstats" {
		return GenerateRandomString(32)
	}
	return GenerateRandomString(64)
}
