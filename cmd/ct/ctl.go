package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/KCaverly/caretaker/internal/ctl"
)

const ctlUsage = `usage: ct ctl <command> [flags]

Talk to the running ct. Worktrees are named "repo/name".

  list [--json] [--feature REPO/NAME]   every worktree (or one feature's), with open agents and status
  summary <repo/name> [--json] [-n N]   commits, diffstat, queue, and the agent's last message
  spawn <repo> <name> [--base REF] [--provider claude|codex] [--feature NAME] [--prompt TEXT | -]
                                        create a worktree and start an agent on the prompt
  send <repo/name> [TEXT | -]           message the worktree's agent (queued until it is idle)
  interrupt <repo/name>                 press Escape in the worktree's agent
  stop <repo/name> [--force]            close the worktree's sessions (keeps the worktree)
  open <repo/name>                      start a closed worktree's sessions, resuming its agents
  reopen <repo/name> [--force]          stop and open again (restarts its agents on the same conversations)
  remove <repo/name>                    ask the user to confirm removing the worktree

  feature new <repo> <name> --manager <repo/name> [--notes PATH]
                                        start a feature, led by the manager's session
  feature add <repo/feature> <repo/name>
                                        add an existing worktree to a feature
  feature list [--json]                 every feature, with its manager, worktrees and stacks
  feature drop <repo/feature>           forget a feature (its worktrees stay)

"-" reads the text from stdin. Set CT_SOCKET to choose between several running ct instances.`

// runCtl dispatches `ct ctl …` and returns the process exit code. It runs
// headless: parse, one socket round trip, print.
func runCtl(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, ctlUsage)
		return 2
	}
	req, asJSON, err := parseCtlArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ct ctl:", err)
		fmt.Fprintln(os.Stderr, ctlUsage)
		return 2
	}
	path, err := ctl.FindSocket()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ct ctl:", err)
		return 1
	}
	resp, err := ctl.Call(path, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ct ctl:", err)
		return 1
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp)
		return 0
	}
	printCtl(req, resp)
	return 0
}

// parseCtlArgs turns the argv of one subcommand into a Request. Flags may sit
// anywhere after the command name.
func parseCtlArgs(args []string) (req ctl.Request, asJSON bool, err error) {
	cmd, rest := args[0], args[1:]
	var positional []string
	var text *string
	readStdin := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		next := func(flag string) (string, error) {
			if i+1 >= len(rest) {
				return "", fmt.Errorf("%s requires a value", flag)
			}
			i++
			return rest[i], nil
		}
		switch a {
		case "--json":
			asJSON = true
		case "--force":
			req.Force = true
		case "--base":
			if req.Base, err = next(a); err != nil {
				return req, false, err
			}
		case "--provider":
			if req.Provider, err = next(a); err != nil {
				return req, false, err
			}
		case "--feature":
			if req.Feature, err = next(a); err != nil {
				return req, false, err
			}
		case "--manager":
			if req.Manager, err = next(a); err != nil {
				return req, false, err
			}
		case "--notes":
			if req.Notes, err = next(a); err != nil {
				return req, false, err
			}
		case "--prompt":
			v, err := next(a)
			if err != nil {
				return req, false, err
			}
			// "--prompt -" reads stdin, like a bare "-".
			if v == "-" {
				readStdin = true
				continue
			}
			text = &v
		case "-n", "--lines":
			v, err := next(a)
			if err != nil {
				return req, false, err
			}
			if req.Lines, err = strconv.Atoi(v); err != nil || req.Lines < 0 {
				return req, false, fmt.Errorf("%s wants a positive number, got %q", a, v)
			}
		case "-":
			readStdin = true
		default:
			if strings.HasPrefix(a, "-") {
				return req, false, fmt.Errorf("unknown flag %q", a)
			}
			positional = append(positional, a)
		}
	}
	if readStdin {
		data, rerr := io.ReadAll(os.Stdin)
		if rerr != nil {
			return req, false, rerr
		}
		s := strings.TrimRight(string(data), "\n")
		text = &s
	}

	switch cmd {
	case "list":
		req.Op = ctl.OpList
	case "summary":
		req.Op = ctl.OpSummary
		if len(positional) != 1 {
			return req, false, fmt.Errorf("summary takes one worktree")
		}
		req.Worktree = positional[0]
	case "spawn":
		req.Op = ctl.OpSpawn
		if len(positional) < 2 {
			return req, false, fmt.Errorf("spawn takes a repo and a worktree name")
		}
		req.Repo, req.Name = positional[0], positional[1]
		if text != nil {
			req.Text = *text
		} else {
			req.Text = strings.Join(positional[2:], " ")
		}
	case "send":
		req.Op = ctl.OpSend
		if len(positional) < 1 {
			return req, false, fmt.Errorf("send takes a worktree and the text")
		}
		req.Worktree = positional[0]
		if text != nil {
			req.Text = *text
		} else {
			req.Text = strings.Join(positional[1:], " ")
		}
		if strings.TrimSpace(req.Text) == "" {
			return req, false, fmt.Errorf("send needs text (as arguments, or - for stdin)")
		}
	case "interrupt", "stop", "remove", "open", "reopen":
		req.Op = ctl.Op(cmd)
		if len(positional) != 1 {
			return req, false, fmt.Errorf("%s takes one worktree", cmd)
		}
		req.Worktree = positional[0]
	case "feature":
		if err := parseFeatureArgs(&req, positional); err != nil {
			return req, false, err
		}
	default:
		return req, false, fmt.Errorf("unknown command %q", cmd)
	}
	if req.Feature != "" && req.Op != ctl.OpSpawn && !strings.Contains(req.Feature, "/") {
		return req, false, fmt.Errorf("feature %q should be written as repo/name", req.Feature)
	}
	if req.Worktree != "" && !strings.Contains(req.Worktree, "/") {
		return req, false, fmt.Errorf("worktree %q should be written as repo/name", req.Worktree)
	}
	return req, asJSON, nil
}

// parseFeatureArgs fills req for `ct ctl feature <sub> …`.
func parseFeatureArgs(req *ctl.Request, positional []string) error {
	if len(positional) == 0 {
		return fmt.Errorf("feature takes new, add, list or drop")
	}
	sub, args := positional[0], positional[1:]

	switch sub {
	case "new":
		req.Op = ctl.OpFeatureNew
		if len(args) != 2 || req.Manager == "" {
			return fmt.Errorf("feature new takes a repo, a name and --manager <repo/name>")
		}
		req.Feature = args[0] + "/" + args[1]
	case "add":
		req.Op = ctl.OpFeatureAdd
		if len(args) != 2 {
			return fmt.Errorf("feature add takes a feature and a worktree")
		}
		req.Feature, req.Worktree = args[0], args[1]
	case "list":
		req.Op = ctl.OpFeatureList
	case "drop":
		req.Op = ctl.OpFeatureDrop
		if len(args) != 1 {
			return fmt.Errorf("feature drop takes one feature")
		}
		req.Feature = args[0]
	default:
		return fmt.Errorf("unknown feature command %q (new, add, list or drop)", sub)
	}
	return nil
}

// printCtl renders a response for people (and for an agent reading a terminal):
// one line per worktree, nothing the reader has to parse.
func printCtl(req ctl.Request, resp ctl.Response) {
	switch req.Op {
	case ctl.OpList:
		if len(resp.Worktrees) == 0 {
			fmt.Println("no worktrees")
			return
		}
		for _, wt := range resp.Worktrees {
			fmt.Println(formatWorktree(wt))
		}
	case ctl.OpFeatureList:
		if len(resp.Features) == 0 {
			fmt.Println("no features")
			return
		}
		for _, f := range resp.Features {
			fmt.Println(formatFeature(f))
		}
	case ctl.OpSummary:
		s := resp.Summary
		if s == nil {
			fmt.Println(resp.Message)
			return
		}
		fmt.Println(formatWorktree(s.WorktreeInfo))
		if len(s.Commits) > 0 {
			fmt.Println("commits:")
			for _, c := range s.Commits {
				fmt.Println("  " + c)
			}
		}
		printFiles("files vs base:", s.Files)
		printFiles("uncommitted:", s.Uncommitted)
		if len(s.Queue) > 0 {
			fmt.Println("queued messages:")
			for _, q := range s.Queue {
				fmt.Println("  - " + firstLine(q))
			}
		}
		if s.LastMessage != "" {
			fmt.Println("last agent message:")
			for _, line := range strings.Split(s.LastMessage, "\n") {
				fmt.Println("  " + line)
			}
		}
	default:
		if resp.Message != "" {
			fmt.Println(resp.Message)
		} else {
			fmt.Println("ok")
		}
	}
}

func formatWorktree(wt ctl.WorktreeInfo) string {
	var b strings.Builder
	b.WriteString(wt.Key)
	if wt.Branch != "" && wt.Branch != wt.Name {
		b.WriteString("  branch=" + wt.Branch)
	}
	if wt.Feature != "" {
		_, name, _ := strings.Cut(wt.Feature, "/")
		b.WriteString("  feature=" + name)
		if wt.Manager {
			b.WriteString(" manager")
		}
	}
	if wt.Open {
		b.WriteString("  open")
	} else {
		b.WriteString("  closed")
	}
	for _, a := range wt.Agents {
		b.WriteString("  " + a.Provider + ":" + a.Label)
		if a.Status != "" {
			b.WriteString("=" + a.Status)
		}
		if a.WaitingFor != "" {
			b.WriteString("(" + a.WaitingFor + ")")
		}
	}
	if wt.Queued > 0 {
		b.WriteString(fmt.Sprintf("  queued=%d", wt.Queued))
	}
	if wt.StackTotal > 0 {
		b.WriteString("  stack=")
		if wt.Stack > 0 {
			b.WriteString(fmt.Sprintf("#%d ", wt.Stack))
		}
		b.WriteString(fmt.Sprintf("%d/%d", wt.StackIndex, wt.StackTotal))
	}
	if wt.PR > 0 {
		b.WriteString(fmt.Sprintf("  pr=#%d", wt.PR))
	}
	if wt.Base != "" {
		b.WriteString(fmt.Sprintf("  ahead=%d behind=%d (%s)", wt.Ahead, wt.Behind, wt.Base))
	}
	if wt.Dirty {
		b.WriteString(fmt.Sprintf("  dirty(+%d/-%d lines)", wt.Added, wt.Deleted))
	}
	if wt.Subject != "" {
		b.WriteString("  " + strconv.Quote(wt.Subject))
	}
	return b.String()
}

func formatFeature(f ctl.FeatureInfo) string {
	var b strings.Builder
	b.WriteString(f.Key)
	if f.Manager == "" {
		b.WriteString("  manager=(gone)")
	} else {
		b.WriteString("  manager=" + f.Manager)
	}
	if len(f.Worktrees) > 0 {
		b.WriteString("  worktrees=" + strings.Join(f.Worktrees, ","))
	}
	if len(f.Stacks) > 0 {
		b.WriteString("  stacks=" + strings.Join(f.Stacks, ","))
	}
	if f.Notes != "" {
		b.WriteString("  notes=" + f.Notes)
	}
	return b.String()
}

func printFiles(title string, files []ctl.FileStat) {
	if len(files) == 0 {
		return
	}
	fmt.Println(title)
	for _, f := range files {
		fmt.Printf("  +%d/-%d %s\n", f.Added, f.Deleted, f.Path)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
