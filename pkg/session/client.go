package session

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kr/pty"
	"github.com/pion/webrtc/v2"
	"golang.org/x/term"
)

type ClientSession struct {
	Session
	OfferURL string
}

func (cs *ClientSession) Run() error {
	err := cs.init()
	if err != nil {
		return fmt.Errorf("could not init client session: %w", err)
	}
	maxPacketLifeTime := uint16(1000) // arbitrary
	ordered := true
	cs.Debug.Printf("creating data channel")
	if cs.DataChannel, err = cs.PeerConnection.CreateDataChannel("data", &webrtc.DataChannelInit{
		Ordered:           &ordered,
		MaxPacketLifeTime: &maxPacketLifeTime,
	}); err != nil {
		return fmt.Errorf("could not create client data channel: %w", err)
	}
	cs.DataChannel.OnOpen(cs.dataChannelOnOpen())
	cs.DataChannel.OnMessage(cs.dataChannelOnMessage())
	cs.DataChannel.OnError(cs.dataChannelOnError())
	cs.DataChannel.OnClose(cs.dataChannelOnClose())
	cs.Debug.Printf("data channel setup")

	body, err := getSDP(cs.OfferURL)
	if err != nil {
		return fmt.Errorf("could not get sdp from server: %w", err)
	}
	cs.Debug.Printf("recieved offer")
	cs.Debug.Printf("got body: %s", body)
	var offerSD SessionDescription
	err = offerSD.Decode(string(body))
	if err != nil {
		return fmt.Errorf("could not decode sdp answer: %w", err)
	}
	cs.Debug.Printf("decoded offer: %+v", offerSD)
	cs.OfferSD = offerSD
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  cs.OfferSD.SDP,
	}
	if err := cs.PeerConnection.SetRemoteDescription(offer); err != nil {
		return fmt.Errorf("could not set remote description: %w", err)
	}
	cs.Debug.Printf("remote connection set")
	answer, err := cs.PeerConnection.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("could not create answer: %w", err)
	}
	cs.Debug.Printf("answer created")
	err = cs.PeerConnection.SetLocalDescription(answer)
	if err != nil {
		return fmt.Errorf("could not set local description: %w", err)
	}
	cs.Debug.Printf("local description set")
	answerSD := SessionDescription{
		SDP: answer.SDP,
	}
	encodedAnswer, err := answerSD.Encode()
	if err != nil {
		return fmt.Errorf("could not encode answer: %w", err)
	}
	cs.Debug.Printf("answer encoded")
	if cs.OfferSD.SDPAnswerURI == "" {
		return fmt.Errorf("no uri provided to upload answer")
	}
	if err := putSDP(cs.OfferSD.SDPAnswerURI, bytes.NewBuffer([]byte(encodedAnswer))); err != nil {
		return fmt.Errorf("could not upload SDP answer: %w", err)
	}
	cs.Debug.Printf("answer uploaded, waiting for connection")
	// wait here to quit
	err = <-cs.ErrorChan
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

func (cs *ClientSession) dataChannelOnOpen() func() {
	return func() {
		cs.Debug.Printf("Data channel '%s'-'%d'='%d' open.\n", cs.DataChannel.Label(), cs.DataChannel.ID(), cs.DataChannel.MaxPacketLifeTime())
		cs.Debug.Println("Terminal session started")

		if err := cs.makeRawTerminal(); err != nil {
			cs.ErrorChan <- fmt.Errorf("could not make raw terminal: %w", err)
			return
		}

		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGWINCH)
		go func() {
			for range ch {
				err := sendTermSize(cs.Stdin, cs.DataChannel.SendText)
				if err != nil {
					cs.ErrorChan <- fmt.Errorf("could not send terminal size: %w", err)
					return
				}
			}
		}()
		ch <- syscall.SIGWINCH // initial resize
		buf := make([]byte, 1024)
		for {
			nr, err := cs.Stdin.Read(buf)
			if err != nil {
				cs.ErrorChan <- fmt.Errorf("could not read stdin: %w", err)
				return
			}
			err = cs.DataChannel.Send(buf[0:nr])
			if err != nil {
				cs.ErrorChan <- fmt.Errorf("could not send buffer over data channel: %w", err)
				return
			}
		}
	}
}

func (cs *ClientSession) dataChannelOnMessage() func(msg webrtc.DataChannelMessage) {
	return func(p webrtc.DataChannelMessage) {
		if p.IsString {
			if string(p.Data) == "quit" {
				if cs.IsTerminal {
					term.Restore(int(cs.Stdin.Fd()), cs.OldTerminalState)
				}
				cs.ErrorChan <- nil
				return
			}
			cs.ErrorChan <- fmt.Errorf("unexpected string message: %s", string(p.Data))
		} else {
			f := bufio.NewWriter(cs.Stdout)
			f.Write(p.Data)
			f.Flush()
		}
	}
}

func (cs *ClientSession) dataChannelOnClose() func() {
	return func() {
		cs.Debug.Printf("data channel closed")
	}
}

func (cs *ClientSession) dataChannelOnError() func(err error) {
	return func(err error) {
		cs.Debug.Printf("error from datachannel: %s", err)
	}
}
