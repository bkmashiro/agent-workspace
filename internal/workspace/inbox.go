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
	Workspace   string    `json:"workspace,omitempty"`
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
	canonicalRoot, err := canonicalWorkspaceRoot(root)
	if err != nil {
		return InboxEvent{}, err
	}
	event.Workspace = canonicalRoot
	event.Stdout = boundInboxOutput(event.Stdout)
	event.Stderr = boundInboxOutput(event.Stderr)
	data, err := json.Marshal(event)
	if err != nil {
		return InboxEvent{}, err
	}
	directory, err := inboxDirectory(root, session, "pending")
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
	directory, err := inboxDirectory(root, session, "pending")
	if err != nil {
		return nil, err
	}
	return readInboxDirectory(directory)
}

func ListAllInbox(session string) ([]InboxEvent, error) {
	directories, err := allInboxDirectories(session, "pending")
	if err != nil {
		return nil, err
	}
	return readInboxDirectories(directories)
}

func ClaimInbox(root, session string) ([]InboxEvent, error) {
	pending, err := inboxDirectory(root, session, "pending")
	if err != nil {
		return nil, err
	}
	return claimInboxDirectory(pending)
}

func ClaimAllInbox(session string) ([]InboxEvent, error) {
	pendingDirectories, err := allInboxDirectories(session, "pending")
	if err != nil {
		return nil, err
	}
	for _, pending := range pendingDirectories {
		if _, err := claimInboxDirectory(pending); err != nil {
			return nil, err
		}
	}
	leasedDirectories, err := allInboxDirectories(session, "leased")
	if err != nil {
		return nil, err
	}
	return readInboxDirectories(leasedDirectories)
}

func AckInbox(root, session string) (int, error) {
	leased, err := inboxDirectory(root, session, "leased")
	if err != nil {
		return 0, err
	}
	return removeInboxDirectoryEvents(leased)
}

func AckAllInbox(session string) (int, error) {
	directories, err := allInboxDirectories(session, "leased")
	if err != nil {
		return 0, err
	}
	total := 0
	for _, directory := range directories {
		count, err := removeInboxDirectoryEvents(directory)
		if err != nil {
			return total, err
		}
		total += count
	}
	return total, nil
}

func DrainInbox(root, session string) ([]InboxEvent, error) {
	events, err := ClaimInbox(root, session)
	if err != nil {
		return nil, err
	}
	if _, err := AckInbox(root, session); err != nil {
		return nil, err
	}
	return events, nil
}

func DrainAllInbox(session string) ([]InboxEvent, error) {
	events, err := ClaimAllInbox(session)
	if err != nil {
		return nil, err
	}
	if _, err := AckAllInbox(session); err != nil {
		return nil, err
	}
	return events, nil
}

func claimInboxDirectory(pending string) ([]InboxEvent, error) {
	leased := filepath.Join(filepath.Dir(pending), "leased")
	entries, err := os.ReadDir(pending)
	if errors.Is(err, os.ErrNotExist) {
		return readInboxDirectory(leased)
	}
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(leased, 0o700); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		source := filepath.Join(pending, entry.Name())
		destination := filepath.Join(leased, entry.Name())
		if err := os.Rename(source, destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return readInboxDirectory(leased)
}

func readInboxDirectories(directories []string) ([]InboxEvent, error) {
	events := make([]InboxEvent, 0)
	for _, directory := range directories {
		items, err := readInboxDirectory(directory)
		if err != nil {
			return nil, err
		}
		events = append(events, items...)
	}
	sortInboxEvents(events)
	return events, nil
}

func readInboxDirectory(directory string) ([]InboxEvent, error) {
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
	sortInboxEvents(events)
	return events, nil
}

func removeInboxDirectoryEvents(directory string) (int, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func sortInboxEvents(events []InboxEvent) {
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
}

func inboxDirectory(root, session, state string) (string, error) {
	if strings.TrimSpace(session) == "" {
		return "", errors.New("inbox session is required")
	}
	stateHome, err := awStateHome()
	if err != nil {
		return "", err
	}
	canonicalRoot, err := canonicalWorkspaceRoot(root)
	if err != nil {
		return "", err
	}
	workspaceHash := sha256.Sum256([]byte(canonicalRoot))
	sessionDigest := sha256.Sum256([]byte(session))
	return filepath.Join(stateHome, hex.EncodeToString(workspaceHash[:16]), hex.EncodeToString(sessionDigest[:16]), state), nil
}

func allInboxDirectories(session, state string) ([]string, error) {
	if strings.TrimSpace(session) == "" {
		return nil, errors.New("inbox session is required")
	}
	stateHome, err := awStateHome()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(stateHome)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	sessionDigest := sha256.Sum256([]byte(session))
	sessionName := hex.EncodeToString(sessionDigest[:16])
	var directories []string
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, filepath.Join(stateHome, entry.Name(), sessionName, state))
		}
	}
	sort.Strings(directories)
	return directories, nil
}

func canonicalWorkspaceRoot(root string) (string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absoluteRoot); err == nil {
		absoluteRoot = resolved
	}
	return absoluteRoot, nil
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
