package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/btcsuite/btcutil/base58"
	"github.com/creack/pty"
	"github.com/pion/webrtc/v2"
	"golang.org/x/term"
)

func envMap(env []string) map[string]string {
	m := make(map[string]string)
	for _, e := range env {
		kv := strings.SplitN(e, "=", 2)
		m[kv[0]] = kv[1]
	}
	return m
}

func getTMUXSession() (string, error) {
	b, err := exec.Command("tmux", "display-message", "-p", "-F", "'#S'").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get session name: %w", err)
	}
	return strings.Trim(string(b), "'\n"), nil
}

func getTMUXClientsInSession(session string) ([]string, error) {
	clients := []string{}
	b, err := exec.Command("tmux", "list-clients", "-F", "'#{client_name}'", "-t", session).Output()
	if err != nil {
		eerr := err.(*exec.ExitError)
		return clients, fmt.Errorf("could not list clients in session: %s: %s: %w", session, eerr.Stderr, err)
	}
	clients = strings.Split(string(b), "\n")
	for i, c := range clients {
		clients[i] = strings.Trim(c, "'")
	}
	return clients, nil
}

func moveClientToTMUXSession(client, session string) error {
	cmd := exec.Command("tmux", "switch-client", "-c", client, "-t", session)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not switch client: %s to session: %s: [%s] %w", client, session, b, err)
	}
	return nil
}

func ensureTMUXSession(session string) error {
	err := exec.Command("tmux", "has-session", "-t", session).Run()
	if err != nil {
		cmd := []string{"tmux", "new-session", "-d", "-t", session}
		err = exec.Command(cmd[0], cmd[1:]...).Run()
		if err != nil {
			return fmt.Errorf("failed to create new session: %v: %w", cmd, err)
		}
		err = exec.Command("tmux", "has-session", "-t", session).Run()
		if err != nil {
			return fmt.Errorf("faild to find session after creating: %s: %w", session, err)
		}
	}
	return nil
}

func hasTMUX() bool {
	_, err := exec.LookPath("tmux")
	if err != nil {
		return false
	}
	return true
}

func isInTMUX(env []string) bool {
	e := envMap(env)
	tmux, hasTMUXVar := e["TMUX"]
	term, hasTERMVar := e["TERM"]
	if !hasTERMVar || !hasTMUXVar {
		return false
	}
	if tmux == "" || !strings.HasPrefix(term, "screen") {
		return false
	}
	return true
}

func main() {
	verbose := flag.Bool("v", false, "Verbose logging")
	stunServer := flag.String("s", "stun:stun.l.google.com:19302", "The stun server to use if hosting")
	sdpServer := flag.String("sdp", "https://pair-server-sw.herokuapp.com", "The sdp server to use if hosting")
	tmuxSession := flag.String("session", "pair", "The tmux session to create if hosting")

	tmuxAttachCmd := []string{"tmux", "attach-session", "-t", *tmuxSession}

	flag.Parse()
	logFlags := 0
	logOut := ioutil.Discard
	if *verbose {
		logFlags = log.LstdFlags | log.Lshortfile
		logOut = os.Stderr
	}
	debug := log.New(logOut, "[debug] ", logFlags)
	args := flag.Args()
	offerURL := ""
	if len(args) > 0 {
		offerURL = args[len(args)-1]
	}
	stdInFD := int(os.Stdin.Fd())
	session := session{
		debug:       debug,
		verbose:     *verbose,
		sdpServer:   *sdpServer,
		stdin:       os.Stdin,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		errorChan:   make(chan error, 1),
		isTerminal:  term.IsTerminal(stdInFD),
		stunServers: []string{*stunServer},
	}
	if len(offerURL) == 0 {
		debug.Println("host session")
		if !hasTMUX() {
			log.Fatalf("please install tmux before continuing")
		}
		if !isInTMUX(os.Environ()) {
			log.Fatalf("please start attach to a tmux session before continuing")
		}
		if err := ensureTMUXSession(*tmuxSession); err != nil {
			log.Fatalf("failed to create extra session %s: %s", *tmuxSession, err)
		}
		currentSession, err := getTMUXSession()
		if err != nil {
			log.Fatalf("failed to get current tmux session: %s", err)
		}
		debug.Printf("current session: %s", currentSession)
		if currentSession == *tmuxSession {
			log.Fatalf("should not already be in this session, please create another and attach to that")
		}
		clients, err := getTMUXClientsInSession(currentSession)
		if err != nil {
			log.Fatalf("could not get tmux clients: %s", err)
		}
		client := ""
		for _, c := range clients {
			if c == "" {
				continue
			}
			client = c
		}
		hs := hostSession{
			tmuxClient:  client,
			tmuxSession: *tmuxSession,
			session:     session,
			cmd:         tmuxAttachCmd,
		}
		err = hs.run()
		if err != nil {
			log.Fatalf("could not start host session: %s", err)
		}
	} else {
		debug.Println("client session")
		if isInTMUX(os.Environ()) {
			log.Fatalf("please detach from any tmux sessions before continuing")
		}
		cs := clientSession{
			session:  session,
			offerURL: offerURL,
		}
		err := cs.run()
		if err != nil {
			log.Fatalf("could not start client session: %s", err)
		}
	}
	debug.Printf("kthnxbai")
}

type sessionDescription struct {
	SDP          string
	SDPURI       string
	SDPAnswerURI string
}

func (sd sessionDescription) Encode() (string, error) {
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

func (sd *sessionDescription) Decode(offer string) error {
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

func randomBytes(len int) ([]byte, error) {
	r := make([]byte, len)
	if _, err := io.ReadFull(rand.Reader, r); err != nil {
		return r, fmt.Errorf("could not read random data into buffer: %w", err)
	}
	return r, nil
}

func randomString(len int) (string, error) {
	b, err := randomBytes(len)
	if err != nil {
		return "", err
	}
	r := base64.URLEncoding.EncodeToString(b)
	return r, nil
}

type session struct {
	stdin, stdout, stderr *os.File
	verbose               bool
	debug                 *log.Logger
	sdpServer             string
	isTerminal            bool
	oldTerminalState      *term.State
	stunServers           []string
	errorChan             chan error
	peerConnection        *webrtc.PeerConnection
	offerSD               sessionDescription
	answerSD              sessionDescription
	dataChannel           *webrtc.DataChannel
}

func (s *session) init() error {
	s.errorChan = make(chan error, 1)
	s.isTerminal = term.IsTerminal(int(s.stdin.Fd()))
	if err := s.createPeerConnection(); err != nil {
		return fmt.Errorf("could not create peer connection: %w", err)
	}
	return nil
}

func (s *session) cleanup() error {
	if s.dataChannel != nil {
		if err := s.dataChannel.SendText("quit"); err != nil {
			return fmt.Errorf("could not send quit over data channel: %w", err)
		}
	}
	if s.isTerminal {
		if err := s.restoreTerminalState(); err != nil {
			return fmt.Errorf("could not restore terminal state: %w", err)
		}
	}
	return nil
}

func (s *session) makeRawTerminal() error {
	var err error
	s.oldTerminalState, err = term.MakeRaw(int(s.stdin.Fd()))
	if err != nil {
		return fmt.Errorf("could not create raw terminal: %w", err)
	}
	return nil
}

func (s *session) restoreTerminalState() error {
	if s.oldTerminalState != nil {
		err := term.Restore(int(s.stdin.Fd()), s.oldTerminalState)
		if err != nil {
			return fmt.Errorf("could not restore terminal state: %w", err)
		}
	}
	return nil
}

func (s *session) createPeerConnection() error {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: s.stunServers,
			},
		},
	}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return fmt.Errorf("could not create peer connection with config: %+v: %w", config, err)
	}
	s.peerConnection = pc
	return nil
}

func genSDPURL(host string) string {
	// FIXME move to server package
	u, err := url.Parse(host)
	if err != nil {
		panic(fmt.Sprintf("badly formed host provided: %s", host))
	}
	randPath, _ := randomString(32)
	u.Path = path.Join("/p", randPath)
	return u.String()
}

func getSDP(url string) ([]byte, error) {
	var body []byte
	resp, err := http.Get(url)
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

func putSDP(url string, body io.Reader) error {
	req, err := http.NewRequest(http.MethodPut, url, body)
	if err != nil {
		return fmt.Errorf("could not build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
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

type hostSession struct {
	session
	tmuxSession string
	tmuxClient  string
	cmd         []string
	pty         *os.File
	ptyReady    bool
}

func (hs *hostSession) run() error {
	err := hs.init()
	if err != nil {
		return fmt.Errorf("could not init host session: %w", err)
	}
	hs.debug.Printf("setting up connection")
	if err := hs.createOffer(); err != nil {
		return fmt.Errorf("could not create offer: %w", err)
	}
	hs.debug.Printf("connection ready: %s", hs.offerSD.SDPURI)
	hs.debug.Printf("offer: %+v", hs.offerSD)
	offer, err := sessionDescription.Encode(hs.offerSD)
	if err != nil {
		return fmt.Errorf("could not encode offer: %w", err)
	}
	verbose := ""
	if hs.verbose {
		verbose = "-v"
	}
	_, err = fmt.Fprintf(hs.stdout, "Share this command with your guest:\n  pair %s %s\n\n", verbose, hs.offerSD.SDPURI)
	if err != nil {
		return fmt.Errorf("could not write sdp uri to stdout: %w", err)
	}
	_, _ = fmt.Fprint(hs.stdout, "\nPlease press return key within 20 seconds of your pair starting their session\n")
	_, _ = bufio.NewReader(hs.stdin).ReadBytes('\n')
	hs.debug.Printf("uploading offer")
	if err := putSDP(hs.offerSD.SDPURI, bytes.NewBuffer([]byte(offer))); err != nil {
		return fmt.Errorf("could not upload SDP offer: %w", err)
	}
	hs.debug.Printf("waiting for response")
	answer, err := getSDP(hs.offerSD.SDPAnswerURI)
	if err != nil {
		return fmt.Errorf("could not get SDP answer: %w", err)
	}
	hs.debug.Printf("got response")
	var answerSD sessionDescription
	err = answerSD.Decode(string(answer))
	if err != nil {
		return fmt.Errorf("could not decode sdp answer: %w", err)
	}
	hs.debug.Printf("decoded response")
	hs.answerSD = answerSD
	err = hs.setHostRemoteDescriptionAndWait()
	if err != nil {
		return fmt.Errorf("could not set host remote description: %w", err)
	}
	return nil
}

func (hs *hostSession) setHostRemoteDescriptionAndWait() error {
	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  hs.answerSD.SDP,
	}
	if err := hs.peerConnection.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("could not set remote description: %w", err)
	}
	if hs.tmuxClient != "" {
		err := moveClientToTMUXSession(hs.tmuxClient, hs.tmuxSession)
		if err != nil {
			return fmt.Errorf("cannot move client: %w", err)
		}
	}
	// wait here to quit
	err := <-hs.errorChan
	if err != nil {
		return fmt.Errorf("recieved error from error channel: %w", err)
	}
	err = hs.cleanup()
	if err != nil {
		return fmt.Errorf("could not clean up: %w", err)
	}
	return nil
}

func (hs *hostSession) dataChannelOnOpen() func() {
	return func() {
		hs.debug.Printf("session started")
		cmd := exec.Command(hs.cmd[0], hs.cmd[1:]...)
		var err error
		hs.pty, err = pty.Start(cmd)
		if err != nil {
			hs.errorChan <- fmt.Errorf("could not start pty: %w", err)
			return
		}
		hs.ptyReady = true
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			for range c {
				hs.debug.Printf("recieved interrupt\n")
				hs.errorChan <- fmt.Errorf("sigint")
				return
			}
		}()
		buf := make([]byte, 1024)
		for {
			nr, err := hs.pty.Read(buf)
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				hs.errorChan <- err
				return
			}
			if err = hs.dataChannel.Send(buf[0:nr]); err != nil {
				hs.errorChan <- fmt.Errorf("could not send to data channel: %w", err)
				return
			}
		}
	}
}

func (hs *hostSession) dataChannelOnMessage() func(msg webrtc.DataChannelMessage) {
	return func(p webrtc.DataChannelMessage) {
		// wait for pty to be ready
		for hs.ptyReady != true {
			time.Sleep(5 * time.Millisecond)
		}
		if p.IsString {
			if len(p.Data) > 2 && p.Data[0] == '[' && p.Data[1] == '"' {
				var msg []string
				err := json.Unmarshal(p.Data, &msg)
				if len(msg) == 0 {
					hs.errorChan <- fmt.Errorf("could not unmarshal json message: %w", err)
					return
				}
				if msg[0] == "stdin" {
					toWrite := []byte(msg[1])
					if len(toWrite) == 0 {
						// shrug
						return
					}
					_, err := hs.pty.Write(toWrite)
					if err != nil {
						hs.errorChan <- fmt.Errorf("could not write to pty: %w", err)
					}
					return
				}
				if msg[0] == "set_size" {
					var size []int
					_ = json.Unmarshal(p.Data, &size) // FIXME: seems wrong
					// if err != nil {
					// 	hs.errorChan <- fmt.Errorf("could not unmarshal json 'set_size' message: %w", err)
					// 	return
					// }
					ws, err := pty.GetsizeFull(hs.pty)
					if err != nil {
						hs.errorChan <- fmt.Errorf("could not get size of terminal: %w", err)
						return
					}
					ws.Rows = uint16(size[1])
					ws.Cols = uint16(size[2])

					if len(size) >= 5 {
						ws.X = uint16(size[3])
						ws.Y = uint16(size[4])
					}
					hs.debug.Printf("changing size of terminal %+v\n", ws)
					if err := pty.Setsize(hs.pty, ws); err != nil {
						hs.errorChan <- fmt.Errorf("could not set terminal size: %w", err)
					}
					return
				}
				if string(p.Data) == "quit" {
					hs.errorChan <- nil
					return
				}
				hs.errorChan <- fmt.Errorf("unexpected string message: %s", string(p.Data))
			}
		} else {
			_, err := hs.pty.Write(p.Data)
			if err != nil {
				hs.errorChan <- fmt.Errorf("could not write to pty: %w", err)
				return
			}
		}
	}
}

func (hs *hostSession) dataChannelOnClose() func() {
	return func() {
		_ = hs.pty.Close()
		hs.debug.Printf("data channel closed")
	}
}

func (hs *hostSession) dataChannelOnError() func(err error) {
	return func(err error) {
		hs.debug.Printf("error from datachannel: %s", err)
	}
}

func (hs *hostSession) onDataChannel() func(*webrtc.DataChannel) {
	return func(dc *webrtc.DataChannel) {
		dc.OnOpen(hs.dataChannelOnOpen())
		dc.OnMessage(hs.dataChannelOnMessage())
		dc.OnClose(hs.dataChannelOnClose())
		dc.OnError(hs.dataChannelOnError())
		hs.dataChannel = dc
	}
}

func (hs *hostSession) createOffer() error {
	hs.peerConnection.OnDataChannel(hs.onDataChannel())
	offer, err := hs.peerConnection.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("could not create offer: %w", err)
	}
	err = hs.peerConnection.SetLocalDescription(offer)
	if err != nil {
		return fmt.Errorf("could not set local peer connection description: %w", err)
	}
	hs.offerSD = sessionDescription{
		SDP:          offer.SDP,
		SDPURI:       genSDPURL(hs.sdpServer),
		SDPAnswerURI: genSDPURL(hs.sdpServer),
	}
	return nil
}

type clientSession struct {
	session
	offerURL string
}

func (cs *clientSession) run() error {
	err := cs.init()
	if err != nil {
		return fmt.Errorf("could not init client session: %w", err)
	}
	maxPacketLifeTime := uint16(1000) // arbitrary
	ordered := true
	cs.debug.Printf("creating data channel")
	if cs.dataChannel, err = cs.peerConnection.CreateDataChannel("data", &webrtc.DataChannelInit{
		Ordered:           &ordered,
		MaxPacketLifeTime: &maxPacketLifeTime,
	}); err != nil {
		return fmt.Errorf("could not create client data channel: %w", err)
	}
	cs.dataChannel.OnOpen(cs.dataChannelOnOpen())
	cs.dataChannel.OnMessage(cs.dataChannelOnMessage())
	cs.dataChannel.OnError(cs.dataChannelOnError())
	cs.dataChannel.OnClose(cs.dataChannelOnClose())
	cs.debug.Printf("data channel setup")

	body, err := getSDP(cs.offerURL)
	if err != nil {
		return fmt.Errorf("could not get sdp from server: %w", err)
	}
	cs.debug.Printf("recieved offer")
	cs.debug.Printf("got body: %s", body)
	var offerSD sessionDescription
	err = offerSD.Decode(string(body))
	if err != nil {
		return fmt.Errorf("could not decode sdp answer: %w", err)
	}
	cs.debug.Printf("decoded offer: %+v", offerSD)
	cs.offerSD = offerSD
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  cs.offerSD.SDP,
	}
	if err := cs.peerConnection.SetRemoteDescription(offer); err != nil {
		return fmt.Errorf("could not set remote description: %w", err)
	}
	cs.debug.Printf("remote connection set")
	answer, err := cs.peerConnection.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("could not create answer: %w", err)
	}
	cs.debug.Printf("answer created")
	err = cs.peerConnection.SetLocalDescription(answer)
	if err != nil {
		return fmt.Errorf("could not set local description: %w", err)
	}
	cs.debug.Printf("local description set")
	answerSD := sessionDescription{
		SDP: answer.SDP,
	}
	encodedAnswer, err := answerSD.Encode()
	if err != nil {
		return fmt.Errorf("could not encode answer: %w", err)
	}
	cs.debug.Printf("answer encoded")
	if cs.offerSD.SDPAnswerURI == "" {
		return fmt.Errorf("no uri provided to upload answer")
	}
	if err := putSDP(cs.offerSD.SDPAnswerURI, bytes.NewBuffer([]byte(encodedAnswer))); err != nil {
		return fmt.Errorf("could not upload SDP answer: %w", err)
	}
	cs.debug.Printf("answer uploaded, waiting for connection")
	// wait here to quit
	err = <-cs.errorChan
	if err != nil {
		return fmt.Errorf("recieved error from error channel: %w", err)
	}
	err = cs.cleanup()
	if err != nil {
		return fmt.Errorf("could not clean up: %w", err)
	}
	return nil
}

func sendTermSize(term *os.File, dcSend func(s string) error) error {
	winSize, err := pty.GetsizeFull(term)
	if err != nil {
		return fmt.Errorf("could not get terminal size: %w", err)
	}
	size := fmt.Sprintf(`["set_size",%d,%d,%d,%d]`, winSize.Rows, winSize.Cols, winSize.X, winSize.Y)
	err = dcSend(size)
	if err != nil {
		return fmt.Errorf("could not send terminal size: %w", err)
	}
	return nil
}

func (cs *clientSession) dataChannelOnOpen() func() {
	return func() {
		cs.debug.Printf("Data channel '%s'-'%d'='%d' open.\n", cs.dataChannel.Label(), cs.dataChannel.ID(), cs.dataChannel.MaxPacketLifeTime())
		cs.debug.Println("Terminal session started")

		if err := cs.makeRawTerminal(); err != nil {
			cs.errorChan <- fmt.Errorf("could not make raw terminal: %w", err)
			return
		}

		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGWINCH)
		go func() {
			for range ch {
				err := sendTermSize(cs.stdin, cs.dataChannel.SendText)
				if err != nil {
					cs.errorChan <- fmt.Errorf("could not send terminal size: %w", err)
					return
				}
			}
		}()
		ch <- syscall.SIGWINCH // initial resize
		buf := make([]byte, 1024)
		for {
			nr, err := cs.stdin.Read(buf)
			if err != nil {
				cs.errorChan <- fmt.Errorf("could not read stdin: %w", err)
				return
			}
			err = cs.dataChannel.Send(buf[0:nr])
			if err != nil {
				cs.errorChan <- fmt.Errorf("could not send buffer over data channel: %w", err)
				return
			}
		}
	}
}

func (cs *clientSession) dataChannelOnMessage() func(msg webrtc.DataChannelMessage) {
	return func(p webrtc.DataChannelMessage) {
		if p.IsString {
			if string(p.Data) == "quit" {
				if cs.isTerminal {
					term.Restore(int(cs.stdin.Fd()), cs.oldTerminalState)
				}
				cs.errorChan <- nil
				return
			}
			cs.errorChan <- fmt.Errorf("unexpected string message: %s", string(p.Data))
		} else {
			f := bufio.NewWriter(cs.stdout)
			f.Write(p.Data)
			f.Flush()
		}
	}
}

func (cs *clientSession) dataChannelOnClose() func() {
	return func() {
		cs.debug.Printf("data channel closed")
	}
}

func (cs *clientSession) dataChannelOnError() func(err error) {
	return func(err error) {
		cs.debug.Printf("error from datachannel: %s", err)
	}
}
