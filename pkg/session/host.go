package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"github.com/kr/pty"
	"github.com/pion/webrtc/v2"
	"github.com/stuart-warren/pair/pkg/handlers"
	"github.com/stuart-warren/pair/pkg/tmux"
)

type HostSession struct {
	Session
	TmuxSession string
	TmuxClient  string
	Cmd         []string
	Pty         *os.File
	PtyReady    bool
}

func (hs *HostSession) Run() error {
	err := hs.init()
	if err != nil {
		return fmt.Errorf("could not init host session: %w", err)
	}
	hs.Debug.Printf("setting up connection")
	if err := hs.createOffer(); err != nil {
		return fmt.Errorf("could not create offer: %w", err)
	}
	hs.Debug.Printf("connection ready: %s", hs.OfferSD.SDPURI)
	hs.Debug.Printf("offer: %+v", hs.OfferSD)
	offer, err := SessionDescription.Encode(hs.OfferSD)
	if err != nil {
		return fmt.Errorf("could not encode offer: %w", err)
	}
	verbose := ""
	if hs.Verbose {
		verbose = "-v"
	}
	_, err = fmt.Fprintf(hs.Stdout, "Share this command with your guest:\n  pair %s %s\n\n", verbose, hs.OfferSD.SDPURI)
	if err != nil {
		return fmt.Errorf("could not write sdp uri to stdout: %w", err)
	}
	_, _ = fmt.Fprint(hs.Stdout, "\nPlease press return key within 20 seconds of your pair starting their session\n")
	_, _ = bufio.NewReader(hs.Stdin).ReadBytes('\n')
	hs.Debug.Printf("uploading offer")
	if err := putSDP(hs.OfferSD.SDPURI, bytes.NewBuffer([]byte(offer))); err != nil {
		return fmt.Errorf("could not upload SDP offer: %w", err)
	}
	hs.Debug.Printf("waiting for response")
	answer, err := getSDP(hs.OfferSD.SDPAnswerURI)
	if err != nil {
		return fmt.Errorf("could not get SDP answer: %w", err)
	}
	hs.Debug.Printf("got response")
	var answerSD SessionDescription
	err = answerSD.Decode(string(answer))
	if err != nil {
		return fmt.Errorf("could not decode sdp answer: %w", err)
	}
	hs.Debug.Printf("decoded response")
	hs.AnswerSD = answerSD
	err = hs.setHostRemoteDescriptionAndWait()
	if err != nil {
		return fmt.Errorf("could not set host remote description: %w", err)
	}
	return nil
}

func (hs *HostSession) setHostRemoteDescriptionAndWait() error {
	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  hs.AnswerSD.SDP,
	}
	if err := hs.PeerConnection.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("could not set remote description: %w", err)
	}
	if hs.TmuxClient != "" {
		err := tmux.MoveClientToSession(hs.TmuxClient, hs.TmuxSession)
		if err != nil {
			return fmt.Errorf("cannot move client: %w", err)
		}
	}
	// wait here to quit
	err := <-hs.ErrorChan
	if err != nil {
		return fmt.Errorf("recieved error from error channel: %w", err)
	}
	err = hs.cleanup()
	if err != nil {
		return fmt.Errorf("could not clean up: %w", err)
	}
	return nil
}

func (hs *HostSession) dataChannelOnOpen() func() {
	return func() {
		hs.Debug.Printf("session started")
		cmd := exec.Command(hs.Cmd[0], hs.Cmd[1:]...)
		var err error
		hs.Pty, err = pty.Start(cmd)
		if err != nil {
			hs.ErrorChan <- fmt.Errorf("could not start pty: %w", err)
			return
		}
		hs.PtyReady = true
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			for range c {
				hs.Debug.Printf("recieved interrupt\n")
				hs.ErrorChan <- fmt.Errorf("sigint")
				return
			}
		}()
		buf := make([]byte, 1024)
		for {
			nr, err := hs.Pty.Read(buf)
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				hs.ErrorChan <- err
				return
			}
			if err = hs.DataChannel.Send(buf[0:nr]); err != nil {
				hs.ErrorChan <- fmt.Errorf("could not send to data channel: %w", err)
				return
			}
		}
	}
}

func (hs *HostSession) dataChannelOnMessage() func(msg webrtc.DataChannelMessage) {
	return func(p webrtc.DataChannelMessage) {
		// wait for pty to be ready
		for hs.PtyReady != true {
			time.Sleep(5 * time.Millisecond)
		}
		if p.IsString {
			if len(p.Data) > 2 && p.Data[0] == '[' && p.Data[1] == '"' {
				var msg []string
				err := json.Unmarshal(p.Data, &msg)
				if len(msg) == 0 {
					hs.ErrorChan <- fmt.Errorf("could not unmarshal json message: %w", err)
					return
				}
				if msg[0] == "stdin" {
					toWrite := []byte(msg[1])
					if len(toWrite) == 0 {
						// shrug
						return
					}
					_, err := hs.Pty.Write(toWrite)
					if err != nil {
						hs.ErrorChan <- fmt.Errorf("could not write to pty: %w", err)
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
					ws, err := pty.GetsizeFull(hs.Pty)
					if err != nil {
						hs.ErrorChan <- fmt.Errorf("could not get size of terminal: %w", err)
						return
					}
					ws.Rows = uint16(size[1])
					ws.Cols = uint16(size[2])

					if len(size) >= 5 {
						ws.X = uint16(size[3])
						ws.Y = uint16(size[4])
					}
					hs.Debug.Printf("changing size of terminal %+v\n", ws)
					if err := pty.Setsize(hs.Pty, ws); err != nil {
						hs.ErrorChan <- fmt.Errorf("could not set terminal size: %w", err)
					}
					return
				}
				if string(p.Data) == "quit" {
					hs.ErrorChan <- nil
					return
				}
				hs.ErrorChan <- fmt.Errorf("unexpected string message: %s", string(p.Data))
			}
		} else {
			_, err := hs.Pty.Write(p.Data)
			if err != nil {
				hs.ErrorChan <- fmt.Errorf("could not write to pty: %w", err)
				return
			}
		}
	}
}

func (hs *HostSession) dataChannelOnClose() func() {
	return func() {
		_ = hs.Pty.Close()
		hs.Debug.Printf("data channel closed")
	}
}

func (hs *HostSession) dataChannelOnError() func(err error) {
	return func(err error) {
		hs.Debug.Printf("error from datachannel: %s", err)
	}
}

func (hs *HostSession) onDataChannel() func(*webrtc.DataChannel) {
	return func(dc *webrtc.DataChannel) {
		dc.OnOpen(hs.dataChannelOnOpen())
		dc.OnMessage(hs.dataChannelOnMessage())
		dc.OnClose(hs.dataChannelOnClose())
		dc.OnError(hs.dataChannelOnError())
		hs.DataChannel = dc
	}
}

func (hs *HostSession) createOffer() error {
	hs.PeerConnection.OnDataChannel(hs.onDataChannel())
	offer, err := hs.PeerConnection.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("could not create offer: %w", err)
	}
	err = hs.PeerConnection.SetLocalDescription(offer)
	if err != nil {
		return fmt.Errorf("could not set local peer connection description: %w", err)
	}
	hs.OfferSD = SessionDescription{
		SDP:          offer.SDP,
		SDPURI:       handlers.GenSDPURL(hs.SDPServer),
		SDPAnswerURI: handlers.GenSDPURL(hs.SDPServer),
	}
	return nil
}
