package main

import (
	"testing"

	"github.com/bottlerocketlabs/pair/pkg/env"
	"github.com/bottlerocketlabs/pair/pkg/session"
)

func TestEnviron(t *testing.T) {
	environ := []string{
		"TERM=screen-256color",
		"TMUX=something",
	}
	m := env.Map(environ)
	if len(m) != 2 {
		t.Errorf("should be 2 entries: %d", len(m))
	}
	if m["TMUX"] != "something" {
		t.Logf("%v", m)
		t.Errorf("m[TMUX]: %s", m["TMUX"])
	}
}

func TestEncodeDecodeSD(t *testing.T) {
	sdp := "something"
	uri := "http://localhost"
	offer := session.SessionDescription{
		SDP:    sdp,
		SDPURI: uri,
	}
	s, err := offer.Encode()
	if err != nil {
		t.Errorf("unexpected error encoding offer: %w", err)
	}
	var decoded session.SessionDescription
	err = decoded.Decode(s)
	if err != nil {
		t.Errorf("unexpected error decoding offer: %w", err)
	}
	if decoded.SDP != offer.SDP {
		t.Errorf("should match: \n%q\n%q\n", decoded.SDP, offer.SDP)
	}
	if decoded.SDPURI != offer.SDPURI {
		t.Errorf("should match: \n%q\n%q\n", decoded.SDPURI, offer.SDPURI)
	}
}
