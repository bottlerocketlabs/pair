package tmux

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/bottlerocketlabs/pair/pkg/env"
)

func GetCurrentSession() (string, error) {
	b, err := exec.Command("tmux", "display-message", "-p", "-F", "'#S'").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get session name: %w", err)
	}
	return strings.Trim(string(b), "'\n"), nil
}

func GetClientsInSession(session string) ([]string, error) {
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

func MoveClientToSession(client, session string) error {
	cmd := exec.Command("tmux", "switch-client", "-c", client, "-t", session)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not switch client: %s to session: %s: [%s] %w", client, session, b, err)
	}
	return nil
}

func EnsureSession(session string) error {
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

func HasBinary() bool {
	_, err := exec.LookPath("tmux")
	if err != nil {
		return false
	}
	return true
}

func IsWithin(environ []string) bool {
	e := env.Map(environ)
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
