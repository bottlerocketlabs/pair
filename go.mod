module github.com/stuart-warren/pair

// +heroku install ./cmd/pair-server
go 1.15

require (
	github.com/btcsuite/btcutil v1.0.2
	github.com/creack/pty v1.1.11
	github.com/microsoftarchive/ttlcache v0.0.0-20180801091818-7dbceb0d5094
	github.com/ory/graceful v0.1.1
	github.com/pion/webrtc/v2 v2.2.26
	golang.org/x/term v0.0.0-20201210144234-2321bbc49cbf
)