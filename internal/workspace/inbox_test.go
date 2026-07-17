package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaimAllInboxRetriesLeasesAndAckDoesNotDeleteNewPendingEvents(t *testing.T) {
	t.Setenv("AW_STATE_HOME", t.TempDir())
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	if _, err := EnqueueEvent(firstRoot, "shared-session", InboxEvent{Source: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueEvent(secondRoot, "shared-session", InboxEvent{Source: "second"}); err != nil {
		t.Fatal(err)
	}

	claimed, err := ClaimAllInbox("shared-session")
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 2 || claimed[0].Workspace == "" || claimed[1].Workspace == "" {
		t.Fatalf("claimed = %#v", claimed)
	}
	retried, err := ClaimAllInbox("shared-session")
	if err != nil || len(retried) != 2 {
		t.Fatalf("retried = %#v, err=%v", retried, err)
	}
	if _, err := EnqueueEvent(firstRoot, "shared-session", InboxEvent{Source: "arrived-during-turn"}); err != nil {
		t.Fatal(err)
	}
	acked, err := AckAllInbox("shared-session")
	if err != nil || acked != 2 {
		t.Fatalf("acked=%d err=%v", acked, err)
	}
	pending, err := ListAllInbox("shared-session")
	if err != nil || len(pending) != 1 || pending[0].Source != "arrived-during-turn" {
		t.Fatalf("pending = %#v, err=%v", pending, err)
	}
	claimed, err = ClaimAllInbox("shared-session")
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed second turn = %#v, err=%v", claimed, err)
	}
	acked, err = AckAllInbox("shared-session")
	if err != nil || acked != 1 {
		t.Fatalf("second ack=%d err=%v", acked, err)
	}
}

func TestInboxIsSessionIsolatedAndDrainConsumesOnlySelectedSession(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("AW_STATE_HOME", stateHome)
	root := t.TempDir()

	first, err := EnqueueEvent(root, "session-a", InboxEvent{Source: "trigger:ci", ExitCode: 0, Stdout: "passed"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() {
		t.Fatalf("event metadata missing: %#v", first)
	}
	if _, err := EnqueueEvent(root, "session-a", InboxEvent{Source: "trigger:review", ExitCode: 1, Stderr: "changes requested"}); err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueEvent(root, "session-b", InboxEvent{Source: "trigger:deploy", ExitCode: 0, Stdout: "ready"}); err != nil {
		t.Fatal(err)
	}

	events, err := ListInbox(root, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Source != "trigger:ci" || events[1].Source != "trigger:review" {
		t.Fatalf("session-a events = %#v", events)
	}
	drained, err := DrainInbox(root, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(drained) != 2 {
		t.Fatalf("drained = %#v", drained)
	}
	remainingA, err := ListInbox(root, "session-a")
	if err != nil || len(remainingA) != 0 {
		t.Fatalf("remaining session-a = %#v, err=%v", remainingA, err)
	}
	remainingB, err := ListInbox(root, "session-b")
	if err != nil || len(remainingB) != 1 {
		t.Fatalf("remaining session-b = %#v, err=%v", remainingB, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent", "state")); !os.IsNotExist(err) {
		t.Fatalf("runtime inbox state leaked into workspace: %v", err)
	}
}

func TestInboxBoundsCommandOutput(t *testing.T) {
	t.Setenv("AW_STATE_HOME", t.TempDir())
	event, err := EnqueueEvent(t.TempDir(), "session", InboxEvent{
		Stdout: strings.Repeat("o", 20_000),
		Stderr: strings.Repeat("e", 20_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Stdout) > InboxOutputLimit || len(event.Stderr) > InboxOutputLimit {
		t.Fatalf("output was not bounded: stdout=%d stderr=%d", len(event.Stdout), len(event.Stderr))
	}
	if !strings.HasPrefix(event.Stdout, "[truncated") || !strings.HasSuffix(event.Stdout, "oooo") {
		t.Fatalf("unexpected truncation shape: %q", event.Stdout[:64])
	}
}

func TestInboxRejectsEmptySession(t *testing.T) {
	t.Setenv("AW_STATE_HOME", t.TempDir())
	if _, err := EnqueueEvent(t.TempDir(), "", InboxEvent{}); err == nil {
		t.Fatal("expected empty session to fail")
	}
}
