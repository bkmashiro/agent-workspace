package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFireTriggersFiltersByDelivery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AW_STATE_HOME", t.TempDir())
	if err := AddCommand(root, "deferred", Command{Run: "printf deferred"}); err != nil {
		t.Fatal(err)
	}
	if err := AddCommand(root, "immediate", Command{Run: "printf immediate"}); err != nil {
		t.Fatal(err)
	}
	if err := AddTrigger(root, "deferred", Trigger{Match: "deploy*", Run: "deferred", Delivery: "defer"}); err != nil {
		t.Fatal(err)
	}
	if err := AddTrigger(root, "immediate", Trigger{Match: "deploy*", Run: "immediate", Delivery: "wake"}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	result := FireTriggersForDelivery(context.Background(), root, "deploy now", "session-1", "defer", &stdout, &stderr)
	if result.ExitCode != 0 || result.Matched != 1 || result.Deferred != 1 {
		t.Fatalf("result = %#v, stderr=%s", result, stderr.String())
	}
	events, err := ListInbox(root, "session-1")
	if err != nil || len(events) != 1 || events[0].Command != "deferred" {
		t.Fatalf("events = %#v, err=%v", events, err)
	}
	if strings.Contains(stdout.String(), "immediate") {
		t.Fatalf("wake trigger unexpectedly fired: %s", stdout.String())
	}
}

func TestFireTriggersRejectsMissingDeferredSessionBeforeRunningCommand(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "should-not-run")
	if err := AddCommand(root, "slow", Command{Run: "printf ran > " + marker}); err != nil {
		t.Fatal(err)
	}
	if err := AddTrigger(root, "deferred", Trigger{Match: "git push*", Run: "slow", Delivery: "defer"}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	result := FireTriggers(context.Background(), root, "git push", "", &stdout, &stderr)
	if result.ExitCode != 2 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("deferred command ran before session validation: %v", err)
	}
}

func TestFireTriggersDefersFailureWithoutReturningFailureToCaller(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("AW_STATE_HOME", stateHome)
	root := t.TempDir()
	if err := AddCommand(root, "ci", Command{Run: "printf 'failed details' >&2; exit 7"}); err != nil {
		t.Fatal(err)
	}
	if err := AddTrigger(root, "after-push", Trigger{Match: "git push*", Run: "ci", Delivery: "defer"}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	result := FireTriggers(context.Background(), root, "git push origin main", "session-1", &stdout, &stderr)
	if result.ExitCode != 0 || result.Matched != 1 || result.Deferred != 1 {
		t.Fatalf("result = %#v, stdout=%q stderr=%q", result, stdout.String(), stderr.String())
	}
	events, err := ListInbox(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ExitCode != 7 || events[0].Trigger != "after-push" || !strings.Contains(events[0].Stderr, "failed details") {
		t.Fatalf("events = %#v", events)
	}
}

func TestFireTriggersWakeDeliveryPropagatesCommandFailure(t *testing.T) {
	root := t.TempDir()
	if err := AddCommand(root, "deploy-check", Command{Run: "printf 'not ready' >&2; exit 3"}); err != nil {
		t.Fatal(err)
	}
	if err := AddTrigger(root, "after-deploy", Trigger{Match: "deploy *", Run: "deploy-check", Delivery: "wake"}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	result := FireTriggers(context.Background(), root, "deploy production", "", &stdout, &stderr)
	if result.ExitCode != 3 || result.Matched != 1 || result.Deferred != 0 {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(stderr.String(), "not ready") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAddTriggerPersistsAndMatchesCommand(t *testing.T) {
	root := t.TempDir()
	err := AddTrigger(root, "ci-after-push", Trigger{
		Match:       "git push*",
		Run:         "github:ci",
		Delivery:    "defer",
		Description: "Watch CI after a successful push",
	})
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := TriggerCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if catalog["ci-after-push"].Source != "workspace" {
		t.Fatalf("catalog = %#v", catalog)
	}
	matched, err := MatchTriggers(root, "git push origin main")
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].Name != "ci-after-push" || matched[0].Run != "github:ci" {
		t.Fatalf("matched = %#v", matched)
	}
	unmatched, err := MatchTriggers(root, "git status")
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched) != 0 {
		t.Fatalf("unexpected matches: %#v", unmatched)
	}
}

func TestPackageTriggersAreNamespaced(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "github")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: github\nversion: 0.1.0\ncommands:\n  ci:\n    run: printf ok\ntriggers:\n  after-push:\n    match: git push*\n    run: ci\n    delivery: defer\n"
	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPackage(root, source); err != nil {
		t.Fatal(err)
	}

	matched, err := MatchTriggers(root, "git push")
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 || matched[0].Name != "github:after-push" || matched[0].Run != "github:ci" || matched[0].Source != "package:github" {
		t.Fatalf("matched = %#v", matched)
	}
}

func TestAddTriggerRejectsUnknownDelivery(t *testing.T) {
	err := AddTrigger(t.TempDir(), "bad", Trigger{Match: "*", Run: "test", Delivery: "telepathy"})
	if err == nil {
		t.Fatal("expected unknown delivery to be rejected")
	}
}

func TestTriggerWildcardMatchesAcrossSlashes(t *testing.T) {
	root := t.TempDir()
	if err := AddTrigger(root, "deploy", Trigger{Match: "wrangler pages deploy*", Run: "deploy:watch", Delivery: "wake"}); err != nil {
		t.Fatal(err)
	}
	matched, err := MatchTriggers(root, "wrangler pages deploy ./dist")
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 {
		t.Fatalf("matched = %#v", matched)
	}
}
