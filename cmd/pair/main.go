package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/stuart-warren/pair/pkg/session"
	"github.com/stuart-warren/pair/pkg/tmux"
	"golang.org/x/term"
)

var (
	// should be populated by goreleaser at build time
	version string = "0.0.0"
	commit  string = "deadbeef"
)

func main() {
	showVersion := flag.Bool("version", false, "Display the version")
	verbose := flag.Bool("v", false, "Verbose logging")
	stunServer := flag.String("s", "stun:stun.l.google.com:19302", "The stun server to use if hosting")
	sdpServer := flag.String("sdp", "https://pair-server-sw.herokuapp.com", "The sdp server to use if hosting")
	tmuxSession := flag.String("session", "pair", "The tmux session to create if hosting")

	tmuxAttachCmd := []string{"tmux", "attach-session", "-t", *tmuxSession}

	flag.Parse()
	if *showVersion {
		fmt.Printf("%s %s (%s)\n", filepath.Base(os.Args[0]), version, commit)
		os.Exit(0)
	}
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
	baseSession := session.Session{
		Debug:       debug,
		UserAgent:   fmt.Sprintf("%s/%s (%s)", filepath.Base(os.Args[0]), version, commit),
		Verbose:     *verbose,
		SDPServer:   *sdpServer,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		ErrorChan:   make(chan error, 1),
		IsTerminal:  term.IsTerminal(stdInFD),
		StunServers: []string{*stunServer},
	}
	if len(offerURL) == 0 {
		debug.Println("host session")
		if !tmux.HasBinary() {
			log.Fatalf("please install tmux before continuing")
		}
		if !tmux.IsWithin(os.Environ()) {
			log.Fatalf("please start attach to a tmux session before continuing")
		}
		if err := tmux.EnsureSession(*tmuxSession); err != nil {
			log.Fatalf("failed to create extra session %s: %s", *tmuxSession, err)
		}
		currentSession, err := tmux.GetCurrentSession()
		if err != nil {
			log.Fatalf("failed to get current tmux session: %s", err)
		}
		debug.Printf("current session: %s", currentSession)
		if currentSession == *tmuxSession {
			log.Fatalf("should not already be in this session, please create another and attach to that")
		}
		clients, err := tmux.GetClientsInSession(currentSession)
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
		hs := session.HostSession{
			TmuxClient:  client,
			TmuxSession: *tmuxSession,
			Session:     baseSession,
			Cmd:         tmuxAttachCmd,
		}
		err = hs.Run()
		if err != nil {
			log.Fatalf("could not start host session: %s", err)
		}
	} else {
		debug.Println("client session")
		if tmux.IsWithin(os.Environ()) {
			log.Fatalf("please detach from any tmux sessions before continuing")
		}
		cs := session.ClientSession{
			Session:  baseSession,
			OfferURL: offerURL,
		}
		err := cs.Run()
		if err != nil {
			log.Fatalf("could not start client session: %s", err)
		}
	}
	debug.Printf("kthnxbai")
}
