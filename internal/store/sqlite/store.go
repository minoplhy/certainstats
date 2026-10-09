package sqlite

import (
	"database/sql"
	_ "modernc.org/sqlite"
)

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// SQLite is single-writer; WAL allows concurrent reads while writing.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA synchronous=FULL"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}

	s := &Store{db: db}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
