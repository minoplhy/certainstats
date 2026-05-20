package store

type Dialect int

const (
	DialectSQLite Dialect = iota
	DialectPostgres
)
