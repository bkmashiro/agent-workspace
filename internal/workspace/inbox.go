package workspace

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const InboxOutputLimit = 8192

type InboxEvent struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	Source      string    `json:"source,omitempty"`
	Trigger     string    `json:"trigger,omitempty"`
	Command     string    `json:"command,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	ExitCode    int       `json:"exit_code"`
	Stdout      string    `json:"stdout,omitempty"`
	Stderr      string    `json:"stderr,omitempty"`
}

func EnqueueEvent(root, session string, event InboxEvent) (InboxEvent, error) {
	if strings.TrimSpace(session) == "" {
		return InboxEvent{}, errors.New("inbox session is required")
	}
	if event.ID == "" {
		id, err := eventID()
		if err != nil {
			return InboxEvent{}, err
		}
		event.ID = id
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.Stdout = boundInboxOutput(event.Stdout)
	event.Stderr = boundInboxOutput(event.Stderr)
	data, err := json.Marshal(event)
	if err != nil {
		return InboxEvent{}, err
	}
	directory, err := inboxDirectory(root, session)
	if err != nil {
		return InboxEvent{}, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return InboxEvent{}, err
	}
	path := filepath.Join(directory, event.ID+".json")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return InboxEvent{}, err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return InboxEvent{}, err
	}
	return event, nil
}

func ListInbox(root, session string) ([]InboxEvent, error) {
	if strings.TrimSpace(session) == "" {
		return nil, errors.New("inbox session is required")
	}
	directory, err := inboxDirectory(root, session)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []InboxEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	events := make([]InboxEvent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		var event InboxEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, fmt.Errorf("parse inbox event %s: %w", entry.Name(), err)
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	return events, nil
}

func DrainInbox(root, session string) ([]InboxEvent, error) {
	events, err := ListInbox(root, session)
	if err != nil {
		return nil, err
	}
	directory, err := inboxDirectory(root, session)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if err := os.Remove(filepath.Join(directory, event.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return events, nil
}

func inboxDirectory(root, session string) (string, error) {
	stateHome, err := awStateHome()
	if err != nil {
		return "", err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absoluteRoot); err == nil {
		absoluteRoot = resolved
	}
	workspaceHash := sha256.Sum256([]byte(absoluteRoot))
	sessionHash := sha256.Sum256([]byte(session))
	return filepath.Join(stateHome, hex.EncodeToString(workspaceHash[:16]), hex.EncodeToString(sessionHash[:16]), "pending"), nil
}

func awStateHome() (string, error) {
	if configured := os.Getenv("AW_STATE_HOME"); configured != "" {
		return configured, nil
	}
	if configured := os.Getenv("XDG_STATE_HOME"); configured != "" {
		return filepath.Join(configured, "aw"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "aw"), nil
}

func eventID() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return fmt.Sprintf("%020d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(random)), nil
}

func boundInboxOutput(output string) string {
	if len(output) <= InboxOutputLimit {
		return output
	}
	prefix := "[truncated to bounded tail]\n"
	remaining := InboxOutputLimit - len(prefix)
	tail := strings.ToValidUTF8(output[len(output)-remaining:], "")
	return prefix + tail
}
