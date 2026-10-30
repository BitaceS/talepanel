`go-pki/` holds test-only PEMs (CA, panel client cert, node certs, keys) issued by the Go
panel's `internal/pki` package for the daemon's cross-language interop tests. Regenerate from
`services/api` with
`TALEPANEL_DAEMON_FIXTURES_DIR=$PWD/../daemon/testdata/go-pki go test ./internal/pki -run TestWriteDaemonInteropFixtures`
(certs are valid for ~20 years; the keys protect nothing).
