package sqlite

import (
	"database/sql"
)

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// SQLite is single-writer; WAL allows concurrent reads while writing.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	db.Exec("PRAGMA synchronous=NORMAL;")

	s := &Store{db: db}

	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.bootstrap(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
