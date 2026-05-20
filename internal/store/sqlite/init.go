package sqlite

import (
	"log"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (s *Store) bootstrap() error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	log.Println("First run — creating default admin account...")

	const (
		userID   = "usr_admin_01"
		username = "admin"
		password = "changeme123"
	)

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`INSERT INTO users(user_id, username, password_hash, is_admin, created_at)
		 VALUES(?, ?, ?, 1, ?)`,
		userID, username, string(hash), time.Now(),
	)
	if err != nil {
		return err
	}

	log.Println("══════════════════════════════════════════")
	log.Println("  http://localhost:8080")
	log.Printf("  Username: %s  |  Password: %s", username, password)
	log.Println("  Change your password after first login!")
	log.Println("══════════════════════════════════════════")
	return nil
}
