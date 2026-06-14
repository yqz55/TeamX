// Package store provides the persistence layer for the TeamX server.
//
// It defines a Store interface backed by SQLite (via modernc.org/sqlite, a
// pure-Go driver). WAL mode is enabled for concurrent read+write. The interface
// abstracts the storage engine so a future PostgreSQL migration only requires a
// new implementation — no callers change.
package store
