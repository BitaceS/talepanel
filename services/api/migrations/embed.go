// Package migrations bundles the SQL migration files into the API binary so
// the server can apply schema changes on startup without relying on the
// Postgres docker-entrypoint-initdb mechanism (which only runs on a brand-new
// data directory and never updates an existing one).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
