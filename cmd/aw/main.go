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

const version = "0.1.0"

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
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(stderr, "usage: aw install <local-package-directory> [--json]")
		return 2
	}
	jsonOutput := false
	if len(args) == 2 {
		if args[1] != "--json" {
			fmt.Fprintf(stderr, "aw: unknown install option %q\n", args[1])
			return 2
		}
		jsonOutput = true
	}
	installed, err := workspace.InstallPackage(root, args[0])
	if err != nil {
		return printError(stderr, err)
	}
	if jsonOutput {
		return writeJSON(stdout, stderr, installed)
	}
	fmt.Fprintf(stdout, "Installed %s@%s (%s)\n", installed.Name, installed.Version, installed.Digest)
	return 0
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
  install <local-directory> [--json]       Install a workspace-local package
  version                                  Print the version`)
}
