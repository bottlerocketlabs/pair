package session

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/btcsuite/btcutil/base58"
	"github.com/pion/webrtc/v2"
	"golang.org/x/term"
)

type SessionDescription struct {
	SDP          string
	SDPURI       string
	SDPAnswerURI string
}

func (sd SessionDescription) Encode() (string, error) {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	_, err := w.Write([]byte(sd.SDP))
	w.Close()
	if err != nil {
		return "", fmt.Errorf("could not write to sdp buffer: %w", err)
	}
	sd.SDP = base58.Encode(b.Bytes())
	offer, err := json.Marshal(&sd)
	if err != nil {
		return "", fmt.Errorf("could not marshal the session description to json: %w", err)
	}
	return base58.Encode(offer), nil
}

func (sd *SessionDescription) Decode(offer string) error {
	decoded := base58.Decode(offer)
	err := json.Unmarshal(decoded, &sd)
	if err != nil {
		return fmt.Errorf("could not unmarshal session description json: %w", err)
	}
	var b bytes.Buffer
	_, err = b.Write(base58.Decode(sd.SDP))
	if err != nil {
		return fmt.Errorf("could not write to buffer decoding sdp: %w", err)
	}
	r, err := zlib.NewReader(&b)
	if err != nil {
		return fmt.Errorf("could not create decompressing reader for sdp: %w", err)
	}
	deflated, err := ioutil.ReadAll(r)
	r.Close()
	if err != nil {
		return fmt.Errorf("could not read sdp through decompressor: %w", err)
	}
	sd.SDP = string(deflated)
	return nil
}

type Session struct {
	Stdin, Stdout, Stderr *os.File
	Verbose               bool
	Debug                 *log.Logger
	UserAgent             string
	SDPServer             string
	IsTerminal            bool
	OldTerminalState      *term.State
	StunServers           []string
	ErrorChan             chan error
	PeerConnection        *webrtc.PeerConnection
	OfferSD               SessionDescription
	AnswerSD              SessionDescription
	DataChannel           *webrtc.DataChannel
}

func (s *Session) init() error {
	s.ErrorChan = make(chan error, 1)
	s.IsTerminal = term.IsTerminal(int(s.Stdin.Fd()))
	if err := s.createPeerConnection(); err != nil {
		return fmt.Errorf("could not create peer connection: %w", err)
	}
	return nil
}

func (s *Session) getSDP(url string) ([]byte, error) {
	var body []byte
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return body, fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("User-Agent", s.UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return body, fmt.Errorf("could not fetch sdp response from %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("unexpected response code from sdp server: [%s] %s", resp.Status, string(body))
	}
	if err != nil {
		return body, fmt.Errorf("could not get body of response for sdp: [%s] %w", resp.Status, err)
	}
	return body, nil
}

func (s *Session) putSDP(url string, body io.Reader) error {
	req, err := http.NewRequest(http.MethodPut, url, body)
	if err != nil {
		return fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("User-Agent", s.UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not put sdp content to %s: %w", url, err)
	}
	defer resp.Body.Close()
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read body of response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response code from sdp server: [%s] %q %s", resp.Status, url, content)
	}
	return nil
}

func (s *Session) cleanup() error {
	if s.DataChannel != nil {
		if err := s.DataChannel.SendText("quit"); err != nil {
			return fmt.Errorf("could not send quit over data channel: %w", err)
		}
	}
	if s.IsTerminal {
		if err := s.restoreTerminalState(); err != nil {
			return fmt.Errorf("could not restore terminal state: %w", err)
		}
	}
	return nil
}

func (s *Session) makeRawTerminal() error {
	var err error
	s.OldTerminalState, err = term.MakeRaw(int(s.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("could not create raw terminal: %w", err)
	}
	return nil
}

func (s *Session) restoreTerminalState() error {
	if s.OldTerminalState != nil {
		err := term.Restore(int(s.Stdin.Fd()), s.OldTerminalState)
		if err != nil {
			return fmt.Errorf("could not restore terminal state: %w", err)
		}
	}
	return nil
}

func (s *Session) createPeerConnection() error {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: s.StunServers,
			},
		},
	}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return fmt.Errorf("could not create peer connection with config: %+v: %w", config, err)
	}
	s.PeerConnection = pc
	return nil
}
