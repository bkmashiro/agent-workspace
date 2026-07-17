package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bkmashiro/agent-workspace/internal/workspace"
)

const version = "0.4.1"

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(runCLI(context.Background(), cwd, os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(ctx context.Context, cwd string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(stdout, version)
		return 0
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage(stdout)
		return 0
	}
	if args[0] == "init" {
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: aw init")
			return 2
		}
		if err := workspace.Init(cwd); err != nil {
			return printError(stderr, err)
		}
		fmt.Fprintf(stdout, "Initialized agent workspace at %s\n", cwd)
		return 0
	}

	if args[0] == "inbox" && slicesContain(args[1:], "--all") {
		return inboxCommand("", args[1:], stdout, stderr)
	}

	root, err := workspace.FindRoot(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "aw: %v\n", err)
		return 2
	}

	switch args[0] {
	case "inspect":
		return inspectCommand(root, args[1:], stdout, stderr)
	case "list":
		return listCommand(root, args[1:], stdout, stderr)
	case "add":
		return addCommand(root, args[1:], stdout, stderr)
	case "run":
		return runCommand(ctx, root, args[1:], stdout, stderr)
	case "install":
		return installCommand(root, args[1:], stdout, stderr)
	case "trigger":
		return triggerCommand(ctx, root, args[1:], stdout, stderr)
	case "inbox":
		return inboxCommand(root, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "aw: unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func inspectCommand(root string, args []string, stdout, stderr io.Writer) int {
	jsonOutput, ok := onlyJSONFlag(args)
	if !ok {
		fmt.Fprintln(stderr, "usage: aw inspect [--json]")
		return 2
	}
	inspection, err := workspace.Inspect(root)
	if err != nil {
		return printError(stderr, err)
	}
	if jsonOutput {
		return writeJSON(stdout, stderr, inspection)
	}
	fmt.Fprintf(stdout, "Workspace: %s\nRoot: %s\nGit: %t\nDetected: %s\nCommands: %d\n",
		inspection.Name, inspection.Root, inspection.Git, strings.Join(inspection.Detected, ", "), len(inspection.Commands))
	return 0
}

func listCommand(root string, args []string, stdout, stderr io.Writer) int {
	jsonOutput, ok := onlyJSONFlag(args)
	if !ok {
		fmt.Fprintln(stderr, "usage: aw list [--json]")
		return 2
	}
	catalog, err := workspace.Catalog(root)
	if err != nil {
		return printError(stderr, err)
	}
	sorted := workspace.SortedCatalog(catalog)
	if jsonOutput {
		return writeJSON(stdout, stderr, sorted)
	}
	if len(sorted) == 0 {
		fmt.Fprintln(stdout, "No workspace commands found.")
		return 0
	}
	for _, item := range sorted {
		description := item.Description
		if description == "" {
			description = item.Run
		}
		fmt.Fprintf(stdout, "%-24s %s [%s]\n", item.Name, description, item.Source)
	}
	return 0
}

func addCommand(root string, args []string, stdout, stderr io.Writer) int {
	if len(args) < 3 {
		fmt.Fprintln(stderr, "usage: aw add <name> [--description <text>] [--snapshot git] -- <command>")
		return 2
	}
	name := args[0]
	command := workspace.Command{}
	separator := -1
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--":
			separator = index
			index = len(args)
		case "--description":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "aw: --description requires a value")
				return 2
			}
			command.Description = args[index+1]
			index++
		case "--snapshot":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "aw: --snapshot requires a value")
				return 2
			}
			command.Snapshot = args[index+1]
			index++
		default:
			fmt.Fprintf(stderr, "aw: unknown add option %q\n", args[index])
			return 2
		}
	}
	if separator < 0 || separator+1 >= len(args) {
		fmt.Fprintln(stderr, "usage: aw add <name> [--description <text>] [--snapshot git] -- <command>")
		return 2
	}
	command.Run = strings.Join(args[separator+1:], " ")
	if err := workspace.AddCommand(root, name, command); err != nil {
		return printError(stderr, err)
	}
	fmt.Fprintf(stdout, "Added workspace command %s\n", name)
	return 0
}

func runCommand(ctx context.Context, root string, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: aw run <name> [-- <args...>]")
		return 2
	}
	name := args[0]
	extra := args[1:]
	if len(extra) > 0 && extra[0] == "--" {
		extra = extra[1:]
	}
	result, err := workspace.Run(ctx, root, name, extra, stdout, stderr)
	if err != nil {
		return printError(stderr, err)
	}
	if result.Stale {
		fmt.Fprintf(stderr, "\naw: result is stale; tested %s, current %s\n", result.TestedState, result.CurrentState)
	}
	return result.ExitCode
}

func installCommand(root string, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: aw install <source> [--ref <git-ref>] [--subdir <path>] [--json]")
		return 2
	}
	source := args[0]
	ref := ""
	subdir := ""
	jsonOutput := false
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonOutput = true
		case "--ref":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "aw: --ref requires a value")
				return 2
			}
			ref = args[index+1]
			index++
		case "--subdir":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "aw: --subdir requires a value")
				return 2
			}
			subdir = args[index+1]
			index++
		default:
			fmt.Fprintf(stderr, "aw: unknown install option %q\n", args[index])
			return 2
		}
	}
	var installed workspace.InstalledPackage
	var err error
	if ref != "" {
		installed, err = workspace.InstallGitPackage(root, source, ref, subdir)
	} else {
		if subdir != "" {
			fmt.Fprintln(stderr, "aw: --subdir requires --ref")
			return 2
		}
		installed, err = workspace.InstallPackage(root, source)
	}
	if err != nil {
		return printError(stderr, err)
	}
	if jsonOutput {
		return writeJSON(stdout, stderr, installed)
	}
	fmt.Fprintf(stdout, "Installed %s@%s (%s)\n", installed.Name, installed.Version, installed.Digest)
	return 0
}

func triggerCommand(ctx context.Context, root string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: aw trigger <add|list|match|fire> ...")
		return 2
	}
	switch args[0] {
	case "add":
		return triggerAddCommand(root, args[1:], stdout, stderr)
	case "list":
		jsonOutput, ok := onlyJSONFlag(args[1:])
		if !ok {
			fmt.Fprintln(stderr, "usage: aw trigger list [--json]")
			return 2
		}
		catalog, err := workspace.TriggerCatalog(root)
		if err != nil {
			return printError(stderr, err)
		}
		triggers := workspace.SortedTriggers(catalog)
		if jsonOutput {
			return writeJSON(stdout, stderr, triggers)
		}
		for _, trigger := range triggers {
			fmt.Fprintf(stdout, "%-24s %-6s %-24s %s\n", trigger.Name, trigger.Delivery, trigger.Run, trigger.Match)
		}
		return 0
	case "match":
		jsonOutput, command, ok := parseObservedCommand(args[1:])
		if !ok {
			fmt.Fprintln(stderr, "usage: aw trigger match [--json] -- <observed-command>")
			return 2
		}
		matched, err := workspace.MatchTriggers(root, command)
		if err != nil {
			return printError(stderr, err)
		}
		if jsonOutput {
			return writeJSON(stdout, stderr, matched)
		}
		for _, trigger := range matched {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", trigger.Name, trigger.Delivery, trigger.Run)
		}
		return 0
	case "fire":
		command, session, delivery, ok := parseFireCommand(args[1:])
		if !ok {
			fmt.Fprintln(stderr, "usage: aw trigger fire [--session <key>] [--delivery defer|wake] -- <observed-command>")
			return 2
		}
		result := workspace.FireTriggersForDelivery(ctx, root, command, session, delivery, stdout, stderr)
		return result.ExitCode
	default:
		fmt.Fprintf(stderr, "aw: unknown trigger command %q\n", args[0])
		return 2
	}
}

func triggerAddCommand(root string, args []string, stdout, stderr io.Writer) int {
	if len(args) < 5 {
		fmt.Fprintln(stderr, "usage: aw trigger add <name> --match <glob> --run <command> [--delivery defer|wake] [--description <text>]")
		return 2
	}
	name := args[0]
	trigger := workspace.Trigger{Delivery: "defer"}
	for index := 1; index < len(args); index++ {
		if index+1 >= len(args) {
			fmt.Fprintf(stderr, "aw: %s requires a value\n", args[index])
			return 2
		}
		value := args[index+1]
		switch args[index] {
		case "--match":
			trigger.Match = value
		case "--run":
			trigger.Run = value
		case "--delivery":
			trigger.Delivery = value
		case "--description":
			trigger.Description = value
		default:
			fmt.Fprintf(stderr, "aw: unknown trigger add option %q\n", args[index])
			return 2
		}
		index++
	}
	if err := workspace.AddTrigger(root, name, trigger); err != nil {
		return printError(stderr, err)
	}
	fmt.Fprintf(stdout, "Added workspace trigger %s\n", name)
	return 0
}

func parseObservedCommand(args []string) (bool, string, bool) {
	jsonOutput := false
	separator := -1
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
		if arg != "--json" {
			return false, "", false
		}
		jsonOutput = true
	}
	if separator < 0 || separator+1 >= len(args) {
		return false, "", false
	}
	return jsonOutput, strings.Join(args[separator+1:], " "), true
}

func parseFireCommand(args []string) (string, string, string, bool) {
	session := defaultSession()
	delivery := ""
	separator := -1
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--":
			separator = index
			index = len(args)
		case "--session":
			if index+1 >= len(args) {
				return "", "", "", false
			}
			session = args[index+1]
			index++
		case "--delivery":
			if index+1 >= len(args) {
				return "", "", "", false
			}
			delivery = args[index+1]
			index++
		default:
			return "", "", "", false
		}
	}
	if separator < 0 || separator+1 >= len(args) {
		return "", "", "", false
	}
	return strings.Join(args[separator+1:], " "), session, delivery, true
}

func inboxCommand(root string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "list" && args[0] != "drain" && args[0] != "claim" && args[0] != "ack") {
		fmt.Fprintln(stderr, "usage: aw inbox <list|claim|ack|drain> [--all] [--session <key>] [--json]")
		return 2
	}
	action := args[0]
	session := defaultSession()
	jsonOutput := false
	allWorkspaces := false
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonOutput = true
		case "--all":
			allWorkspaces = true
		case "--session":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "aw: --session requires a value")
				return 2
			}
			session = args[index+1]
			index++
		default:
			fmt.Fprintf(stderr, "aw: unknown inbox option %q\n", args[index])
			return 2
		}
	}
	if session == "" {
		fmt.Fprintln(stderr, "aw: inbox session is required; pass --session or set AW_SESSION_ID")
		return 2
	}
	if action == "ack" {
		var acknowledged int
		var err error
		if allWorkspaces {
			acknowledged, err = workspace.AckAllInbox(session)
		} else {
			acknowledged, err = workspace.AckInbox(root, session)
		}
		if err != nil {
			return printError(stderr, err)
		}
		result := map[string]int{"acked": acknowledged}
		if jsonOutput {
			return writeJSON(stdout, stderr, result)
		}
		fmt.Fprintf(stdout, "Acknowledged %d inbox event(s)\n", acknowledged)
		return 0
	}

	var events []workspace.InboxEvent
	var err error
	switch action {
	case "claim":
		if allWorkspaces {
			events, err = workspace.ClaimAllInbox(session)
		} else {
			events, err = workspace.ClaimInbox(root, session)
		}
	case "drain":
		if allWorkspaces {
			events, err = workspace.DrainAllInbox(session)
		} else {
			events, err = workspace.DrainInbox(root, session)
		}
	default:
		if allWorkspaces {
			events, err = workspace.ListAllInbox(session)
		} else {
			events, err = workspace.ListInbox(root, session)
		}
	}
	if err != nil {
		return printError(stderr, err)
	}
	if jsonOutput {
		return writeJSON(stdout, stderr, events)
	}
	for _, event := range events {
		fmt.Fprintf(stdout, "[%s] %s exit=%d command=%s\n", event.CreatedAt.Format("2006-01-02T15:04:05Z"), event.Source, event.ExitCode, event.Command)
		if event.Stdout != "" {
			fmt.Fprintln(stdout, event.Stdout)
		}
		if event.Stderr != "" {
			fmt.Fprintln(stdout, event.Stderr)
		}
	}
	return 0
}

func defaultSession() string {
	for _, name := range []string{"AW_SESSION_ID", "HERMES_SESSION_ID"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func onlyJSONFlag(args []string) (bool, bool) {
	if len(args) == 0 {
		return false, true
	}
	if len(args) == 1 && args[0] == "--json" {
		return true, true
	}
	return false, false
}

func writeJSON(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return printError(stderr, err)
	}
	return 0
}

func printError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "aw: %v\n", err)
	return 1
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `Usage: aw <command>

Commands:
  init                                     Initialize .agent/workspace.yaml
  inspect [--json]                         Detect the current workspace
  list [--json]                            List discovered commands
  add <name> [options] -- <command>        Add a workspace-local command
  run <name> [-- <args...>]                Run a command at the workspace root
  install <source> [--ref R] [--subdir P]   Install a local or Git package
  trigger <add|list|match|fire> ...          Manage and fire command triggers
  inbox <list|claim|ack|drain> [options]     Read deferred trigger results
  version                                    Print the version`)
}
