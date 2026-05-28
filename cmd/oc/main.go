// oc is the OpenContext CLI. It communicates with the OpenContext daemon over HTTP and also
// exposes collector subcommands used by shell hooks.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/yetanotherai/opencontext/internal/daemon"
	"github.com/yetanotherai/opencontext/internal/installers"
	"github.com/yetanotherai/opencontext/internal/registry"
	"github.com/yetanotherai/opencontext/internal/service"
	"github.com/yetanotherai/opencontext/pkg/client"
	"github.com/yetanotherai/opencontext/pkg/event"
)

var (
	daemonURL string
	jsonOut   bool
	version   = "0.1.0"
)

func main() {
	root := buildRoot()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildRoot() *cobra.Command {
	root := &cobra.Command{
		Use:     "oc",
		Version: version,
		Short:   "OpenContext CLI — inspect events, trigger compiles, manage collectors",
		Long: `oc is the command-line interface for OpenContext.

Environment variables:
  OC_DAEMON_URL    OpenContext daemon base URL (default: http://localhost:6060)`,
	}

	root.PersistentFlags().StringVar(&daemonURL, "daemon", envOrDefault("OC_DAEMON_URL", "http://localhost:6060"), "OpenContext daemon base URL")
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "output as JSON")

	root.AddCommand(
		buildDaemonCmd(),
		buildStatusCmd(),
		buildEventsCmd(),
		buildCompileCmd(),
		buildCollectorsCmd(),
		buildCollectorCmd(),
		buildInjectCmd(),
	)

	return root
}

// ── oc collectors ────────────────────────────────────────────────────────────

func buildCollectorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collectors",
		Short: "List collector integrations and event schemas",
	}
	cmd.AddCommand(buildCollectorsListCmd())
	cmd.AddCommand(buildCollectorsInfoCmd())
	cmd.AddCommand(buildCollectorsSchemasCmd())
	return cmd
}

func buildCollectorsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List known collector integrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			collectors := registry.AllCollectors()
			if jsonOut {
				return printJSON(withResolvedCollectorVersions(collectors))
			}
			fmt.Printf("%-12s %-18s %-16s %-20s %s\n", "NAME", "KIND", "VERSION", "SOURCES", "INSTALL")
			for _, c := range collectors {
				fmt.Printf("%-12s %-18s %-16s %-20s %s\n",
					c.Name,
					c.Kind,
					resolveCollectorVersion(c.Version),
					strings.Join(c.Sources, ","),
					strings.Join(c.Install, " && "),
				)
			}
			return nil
		},
	}
}

func buildCollectorsInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show collector integration details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, ok := registry.LookupCollector(args[0])
			if !ok {
				return fmt.Errorf("unknown collector %q", args[0])
			}
			c.Version = resolveCollectorVersion(c.Version)
			if jsonOut {
				return printJSON(c)
			}
			fmt.Printf("name:        %s\n", c.Name)
			fmt.Printf("display:     %s\n", c.DisplayName)
			fmt.Printf("version:     %s\n", c.Version)
			fmt.Printf("kind:        %s\n", c.Kind)
			fmt.Printf("platforms:   %s\n", strings.Join(c.Platforms, ", "))
			fmt.Printf("sources:     %s\n", strings.Join(c.Sources, ", "))
			fmt.Printf("description: %s\n", c.Description)
			if len(c.Install) > 0 {
				fmt.Println("install:")
				for _, install := range c.Install {
					fmt.Printf("  %s\n", install)
				}
			}
			if c.Docs != "" {
				fmt.Printf("docs:        %s\n", c.Docs)
			}
			if len(c.Schemas) > 0 {
				fmt.Println("schemas:")
				for _, s := range c.Schemas {
					fmt.Printf("  %s.%s\n", s.Source, s.Type)
				}
			}
			return nil
		},
	}
}

func buildCollectorsSchemasCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schemas",
		Short: "List registered event schemas",
		RunE: func(cmd *cobra.Command, args []string) error {
			schemas := event.AllSchemas()
			sortSchemas(schemas)
			if jsonOut {
				return printJSON(schemas)
			}
			fmt.Printf("%-24s %s\n", "EVENT", "DESCRIPTION")
			for _, s := range schemas {
				fmt.Printf("%-24s %s\n", fmt.Sprintf("%s.%s", s.Source, s.Type), s.Description)
			}
			return nil
		},
	}
}

// ── oc daemon ────────────────────────────────────────────────────────────────

func buildDaemonCmd() *cobra.Command {
	var cfgFile string
	var logLevel string

	cmd := &cobra.Command{
		Use:     "daemon",
		Aliases: []string{"start", "serve"},
		Short:   "Run the OpenContext local daemon",
		Example: `  oc daemon
  oc daemon --log-level debug
  oc start`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonForeground(cfgFile, logLevel)
		},
	}

	cmd.Flags().StringVar(&cfgFile, "config", "", "config file (default: ~/.opencontext/config.yaml)")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "log level: debug|info|warn|error")
	cmd.AddCommand(buildDaemonRunCmd())
	cmd.AddCommand(buildDaemonInstallCmd())
	cmd.AddCommand(buildDaemonUninstallCmd())
	cmd.AddCommand(buildDaemonServiceCmd("start", "Start the installed daemon service", func(m service.Manager) error { return m.Start() }))
	cmd.AddCommand(buildDaemonServiceCmd("stop", "Stop the installed daemon service", func(m service.Manager) error { return m.Stop() }))
	cmd.AddCommand(buildDaemonServiceCmd("restart", "Restart the installed daemon service", func(m service.Manager) error { return m.Restart() }))
	cmd.AddCommand(buildDaemonStatusCmd())
	cmd.AddCommand(buildDaemonLogsCmd())
	return cmd
}

func runDaemonForeground(cfgFile, logLevel string) error {
	return daemon.Run(daemon.Options{
		ConfigFile: cfgFile,
		LogLevel:   logLevel,
		Version:    version,
	})
}

func buildDaemonRunCmd() *cobra.Command {
	var cfgFile string
	var logLevel string
	cmd := &cobra.Command{
		Use:    "run",
		Short:  "Run the daemon in the foreground",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonForeground(cfgFile, logLevel)
		},
	}
	cmd.Flags().StringVar(&cfgFile, "config", "", "config file (default: ~/.opencontext/config.yaml)")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "log level: debug|info|warn|error")
	return cmd
}

func buildDaemonInstallCmd() *cobra.Command {
	var cfg service.Config
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install and start OpenContext as a background service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := service.Resolve(&cfg); err != nil {
				return err
			}
			mgr, err := service.NewManager()
			if err != nil {
				return err
			}
			if st, _ := mgr.Status(); st != nil && st.Installed && !force {
				return fmt.Errorf("daemon service already installed; use --force to reinstall")
			}
			if force {
				_ = mgr.Uninstall()
			}
			if err := mgr.Install(cfg); err != nil {
				return err
			}
			if err := service.SaveMeta(&service.Meta{
				LogFile:     cfg.LogFile,
				LogMaxSize:  cfg.LogMaxSize,
				WorkDir:     cfg.WorkDir,
				ConfigFile:  cfg.ConfigFile,
				BinaryPath:  cfg.BinaryPath,
				Platform:    mgr.Platform(),
				InstalledAt: service.NowISO(),
			}); err != nil {
				return fmt.Errorf("save daemon metadata: %w", err)
			}
			fmt.Println("OpenContext daemon installed and started.")
			fmt.Printf("  platform: %s\n", mgr.Platform())
			fmt.Printf("  binary:   %s\n", cfg.BinaryPath)
			fmt.Printf("  workdir:  %s\n", cfg.WorkDir)
			fmt.Printf("  log:      %s\n", cfg.LogFile)
			if strings.Contains(mgr.Platform(), "user") {
				if enabled, user := service.CheckLinger(); !enabled {
					fmt.Printf("\nWarning: user service may stop after logout. To keep it alive, run: sudo loginctl enable-linger %s\n", user)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.ConfigFile, "config", "", "OpenContext config file (default: ~/.opencontext/config.yaml)")
	cmd.Flags().StringVar(&cfg.WorkDir, "work-dir", "", "working directory (default: current directory)")
	cmd.Flags().StringVar(&cfg.LogFile, "log-file", "", "log file path (default: ~/.opencontext/logs/oc.log)")
	cmd.Flags().Int64Var(&cfg.LogMaxSize, "log-max-size", 10, "max log size in MB")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing service installation")
	cmd.PreRun = func(cmd *cobra.Command, args []string) {
		cfg.LogMaxSize *= 1024 * 1024
	}
	return cmd
}

func buildDaemonUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the installed daemon service",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := service.NewManager()
			if err != nil {
				return err
			}
			if err := mgr.Uninstall(); err != nil {
				return err
			}
			service.RemoveMeta()
			fmt.Println("OpenContext daemon uninstalled.")
			return nil
		},
	}
}

func buildDaemonServiceCmd(use, short string, action func(service.Manager) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := service.NewManager()
			if err != nil {
				return err
			}
			if err := requireServiceInstalled(mgr); err != nil {
				return err
			}
			if err := action(mgr); err != nil {
				return err
			}
			past := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[use]
			fmt.Printf("OpenContext daemon %s.\n", past)
			return nil
		},
	}
}

func buildDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show background daemon service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := service.NewManager()
			if err != nil {
				return err
			}
			st, err := mgr.Status()
			if err != nil {
				return err
			}
			state := "stopped"
			if st.Running {
				state = "running"
			}
			if !st.Installed {
				state = "not installed"
			}
			fmt.Println("OpenContext daemon service")
			fmt.Printf("  status:   %s\n", state)
			fmt.Printf("  platform: %s\n", st.Platform)
			if st.PID > 0 {
				fmt.Printf("  pid:      %d\n", st.PID)
			}
			if meta, err := service.LoadMeta(); err == nil {
				fmt.Printf("  log:      %s\n", meta.LogFile)
				fmt.Printf("  workdir:  %s\n", meta.WorkDir)
			}
			return nil
		},
	}
}

func buildDaemonLogsCmd() *cobra.Command {
	var follow bool
	var lines int
	var logFile string
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show daemon log output",
		RunE: func(cmd *cobra.Command, args []string) error {
			if logFile == "" {
				if meta, err := service.LoadMeta(); err == nil && meta.LogFile != "" {
					logFile = meta.LogFile
				} else {
					logFile = service.DefaultLogFile()
				}
			}
			if err := printLastLines(logFile, lines); err != nil {
				return err
			}
			if follow {
				return followFile(logFile)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "number of lines to show")
	cmd.Flags().StringVar(&logFile, "log-file", "", "custom log file path")
	return cmd
}

func requireServiceInstalled(mgr service.Manager) error {
	st, err := mgr.Status()
	if err != nil {
		return err
	}
	if st == nil || !st.Installed {
		return fmt.Errorf("daemon service is not installed; run: oc daemon install")
	}
	return nil
}

// ── oc status ─────────────────────────────────────────────────────────────────

func buildStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show OpenContext daemon health and statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.New(daemonURL)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			health, err := c.Health(ctx)
			if err != nil {
				return fmt.Errorf("OpenContext daemon unreachable at %s: %w\n\nStart it with: oc daemon", daemonURL, err)
			}

			if jsonOut {
				return printJSON(health)
			}

			fmt.Printf("daemon status:   %s\n", health["status"])
			fmt.Printf("version:         %s\n", health["version"])
			fmt.Printf("uptime:          %ss\n", formatNum(health["uptime_seconds"]))
			fmt.Printf("events stored:   %s\n", formatNum(health["events_stored"]))
			fmt.Printf("daemon URL:      %s\n", daemonURL)
			return nil
		},
	}
}

// ── oc events ─────────────────────────────────────────────────────────────────

func buildEventsCmd() *cobra.Command {
	var (
		source  string
		project string
		since   string
		limit   int
		query   string
	)

	cmd := &cobra.Command{
		Use:   "events",
		Short: "List recent activity events",
		Example: `  oc events
  oc events --source shell --project opencontext --since 2h
  oc events --query "go build" --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.New(daemonURL)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			sinceMs := parseSinceDuration(since)

			q := &event.QueryRequest{
				Project: project,
				Since:   sinceMs,
				Limit:   limit,
				Query:   query,
			}
			if source != "" {
				q.Source = event.Source(source)
			}

			resp, err := c.QueryEvents(ctx, q)
			if err != nil {
				return fmt.Errorf("query events: %w", err)
			}

			if jsonOut {
				return printJSON(resp)
			}

			if len(resp.Events) == 0 {
				fmt.Println("No events found.")
				return nil
			}

			fmt.Printf("%-24s %-8s %-16s %s\n", "TIME", "SOURCE", "TYPE", "SUMMARY")
			fmt.Printf("%-24s %-8s %-16s %s\n", "────────────────────────", "────────", "────────────────", "───────────────────────────────────────")
			for _, e := range resp.Events {
				ts := time.UnixMilli(e.Ts).Format("2006-01-02 15:04:05")
				summary := buildEventSummary(e)
				fmt.Printf("%-24s %-8s %-16s %s\n", ts, e.Source, e.Type, summary)
			}

			if resp.Truncated {
				fmt.Printf("\n(showing %d of %d+, use --limit to see more)\n", len(resp.Events), resp.Total)
			} else {
				fmt.Printf("\n%d event(s)\n", resp.Total)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "filter by source (shell|git|os|browser|ide|im)")
	cmd.Flags().StringVar(&project, "project", "", "filter by project name")
	cmd.Flags().StringVar(&since, "since", "24h", "time window (e.g. 2h, 30m, 7d)")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum events to return")
	cmd.Flags().StringVar(&query, "query", "", "full-text search query")

	// oc events clear
	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Delete stored events",
		Example: `  oc events clear           # delete all events
  oc events clear --source shell  # delete shell events only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.New(daemonURL)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if source != "" {
				if err := c.DeleteEventsBySource(ctx, source); err != nil {
					return fmt.Errorf("delete %s events: %w", source, err)
				}
				fmt.Printf("Deleted all events with source: %s\n", source)
				return nil
			}

			if err := c.DeleteAllEvents(ctx); err != nil {
				return fmt.Errorf("delete events: %w", err)
			}
			fmt.Println("Deleted all events.")
			return nil
		},
	}
	clearCmd.Flags().StringVar(&source, "source", "", "delete events from a specific source (shell|git|os|browser|ide|im)")
	cmd.AddCommand(clearCmd)

	return cmd
}

// ── oc compile ────────────────────────────────────────────────────────────────

func buildCompileCmd() *cobra.Command {
	var subName string

	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Trigger memory compilation for a subscription",
		Example: `  oc compile
  oc compile --subscription opencontext-project`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.New(daemonURL)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := c.TriggerCompile(ctx, subName); err != nil {
				return fmt.Errorf("trigger compile: %w", err)
			}

			if subName == "" {
				fmt.Println("Memory compilation triggered for all subscriptions.")
			} else {
				fmt.Printf("Memory compilation triggered for subscription: %s\n", subName)
			}
			fmt.Println("(Compilation runs asynchronously — check memory.md in a moment)")
			return nil
		},
	}

	cmd.Flags().StringVar(&subName, "subscription", "", "subscription name (default: all)")
	return cmd
}

// ── oc collector ─────────────────────────────────────────────────────────────

func buildCollectorCmd() *cobra.Command {
	collector := &cobra.Command{
		Use:   "collector",
		Short: "Collector management subcommands",
	}
	collector.AddCommand(buildShellCollectorCmd())
	collector.AddCommand(buildClaudeCollectorCmd())
	collector.AddCommand(buildCodexCollectorCmd())
	collector.AddCommand(buildCursorCollectorCmd())
	collector.AddCommand(buildOpenCodeCollectorCmd())
	return collector
}

func buildShellCollectorCmd() *cobra.Command {
	shell := &cobra.Command{
		Use:   "shell",
		Short: "Shell collector commands",
	}
	shell.AddCommand(buildShellPushCmd())
	shell.AddCommand(buildShellInstallCmd())
	return shell
}

func buildShellPushCmd() *cobra.Command {
	var (
		command     string
		exitCode    int
		durationMs  int64
		cwd         string
		sensitivity int
	)

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push a shell command event to the OpenContext daemon",
		Long: `Push is called by shell hook scripts (zsh preexec/precmd) to record
a command execution event. It runs non-blocking and silently ignores
the OpenContext daemon being unavailable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if command == "" {
				return nil // empty commands are silently dropped
			}

			project := detectProject(cwd)

			labels := map[string]string{
				"app":       detectShell(),
				"exit_code": strconv.Itoa(exitCode),
			}
			if cwd != "" {
				labels["cwd"] = cwd
			}
			if project != "" {
				labels["project"] = project
			}

			payload := map[string]any{
				"duration_ms": durationMs,
			}

			sens := event.SensitivityLevel(sensitivity)
			if sens == 0 {
				sens = event.SensitivityL1
			}

			// L1: command name (first word) only. L2: full string.
			if sens >= event.SensitivityL2 {
				payload["command"] = command
			} else {
				payload["command"] = firstWord(command)
			}

			e := &event.ActivityEvent{
				Ts:          time.Now().UnixMilli(),
				Source:      event.SourceShell,
				Type:        event.EventTypeCommand,
				Sensitivity: sens,
				Labels:      labels,
				Payload:     payload,
			}

			c := client.New(daemonURL)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			// Non-blocking: silently ignore errors so the shell is never slowed down.
			_, _ = c.Push(ctx, e)
			return nil
		},
	}

	cmd.Flags().StringVar(&command, "command", "", "command string that was executed")
	cmd.Flags().IntVar(&exitCode, "exit-code", 0, "exit code of the command")
	cmd.Flags().Int64Var(&durationMs, "duration-ms", 0, "execution duration in milliseconds")
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory when command ran")
	cmd.Flags().IntVar(&sensitivity, "sensitivity", 1, "sensitivity level (1=L1, 2=L2)")

	return cmd
}

func buildShellInstallCmd() *cobra.Command {
	var sensitivity int

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install shell hooks for zsh and bash",
		Long: `Install shell hooks that record commands to the OpenContext daemon.

Sensitivity levels:
  1 (L1) — command name only, e.g. "go" instead of "go build ./..."
  2 (L2, default) — full command string including arguments`,
		Example: `  oc collector shell install
  oc collector shell install --sensitivity 2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installers.InstallShell(sensitivity)
		},
	}

	cmd.Flags().IntVar(&sensitivity, "sensitivity", 2, "sensitivity level: 1=command name only, 2=full command with args")
	return cmd
}

// ── claude collector ──────────────────────────────────────────────────────────

func buildClaudeCollectorCmd() *cobra.Command {
	claude := &cobra.Command{
		Use:   "claude",
		Short: "Claude Code hook collector commands",
	}
	claude.AddCommand(buildClaudeInstallCmd())
	return claude
}

func buildClaudeInstallCmd() *cobra.Command {
	var daemonAddr string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install OpenContext HTTP hooks into Claude Code",
		Long: `Adds UserPromptSubmit and SessionStart HTTP hooks to Claude Code.
Claude Code will POST each user message to the OpenContext daemon for recording.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installers.InstallClaude(daemonAddr)
		},
	}

	cmd.Flags().StringVar(&daemonAddr, "daemon", "http://localhost:6060", "OpenContext daemon base URL")
	return cmd
}

// ── Codex CLI collector ───────────────────────────────────────────────────────

func buildCodexCollectorCmd() *cobra.Command {
	codex := &cobra.Command{
		Use:   "codex",
		Short: "OpenAI Codex CLI hook collector commands",
	}
	codex.AddCommand(buildCodexInstallCmd())
	return codex
}

func buildCodexInstallCmd() *cobra.Command {
	var daemonAddr string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install OpenContext hooks into Codex CLI (~/.codex/config.json)",
		Long: `Adds UserPromptSubmit and SessionStart HTTP hooks to Codex CLI.
Codex will POST each user message to the OpenContext daemon for recording.

Requires Codex CLI with hooks support (codex >= 0.1.x).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installers.InstallCodex(daemonAddr)
		},
	}
	cmd.Flags().StringVar(&daemonAddr, "daemon", "http://localhost:6060", "OpenContext daemon base URL")
	return cmd
}

// ── Cursor IDE collector ──────────────────────────────────────────────────────

func buildCursorCollectorCmd() *cobra.Command {
	cursor := &cobra.Command{
		Use:   "cursor",
		Short: "Cursor IDE agent hook collector commands",
	}
	cursor.AddCommand(buildCursorInstallCmd())
	return cursor
}

func buildCursorInstallCmd() *cobra.Command {
	var daemonAddr string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install OpenContext hooks into Cursor IDE (~/.cursor/hooks.json)",
		Long: `Adds beforeSubmitPrompt and sessionStart command hooks to Cursor IDE.
Cursor will execute the hook script on each user prompt submission.

Requires Cursor IDE with hooks support (Cursor >= 1.0).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installers.InstallCursor(daemonAddr)
		},
	}
	cmd.Flags().StringVar(&daemonAddr, "daemon", "http://localhost:6060", "OpenContext daemon base URL")
	return cmd
}

// ── OpenCode collector ────────────────────────────────────────────────────────

func buildOpenCodeCollectorCmd() *cobra.Command {
	opencode := &cobra.Command{
		Use:   "opencode",
		Short: "OpenCode (sst/opencode) hook collector commands",
	}
	opencode.AddCommand(buildOpenCodeInstallCmd())
	return opencode
}

func buildOpenCodeInstallCmd() *cobra.Command {
	var daemonAddr string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install OpenContext hooks into OpenCode (~/.config/opencode/hooks.json)",
		Long: `Adds UserPromptSubmit and SessionStart command hooks to OpenCode.
OpenCode will execute the hook script on each user message submission.

Supports both the native opencode hook format and the Claude-compatible
format (via opencode-claude-hooks npm package).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installers.InstallOpenCode(daemonAddr)
		},
	}
	cmd.Flags().StringVar(&daemonAddr, "daemon", "http://localhost:6060", "OpenContext daemon base URL")
	return cmd
}

// ── shell helpers ─────────────────────────────────────────────────────────────

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}

func detectProject(cwd string) string {
	if cwd == "" {
		return ""
	}
	// Walk up looking for .git
	dir := cwd
	for {
		if _, err := os.Stat(dir + "/.git"); err == nil {
			// Found git root — use directory basename
			for i := len(dir) - 1; i >= 0; i-- {
				if dir[i] == '/' {
					return dir[i+1:]
				}
			}
			return dir
		}
		parent := ""
		for i := len(dir) - 1; i >= 0; i-- {
			if dir[i] == '/' {
				parent = dir[:i]
				break
			}
		}
		if parent == "" || parent == dir {
			break
		}
		dir = parent
	}
	// Fall back to cwd basename
	for i := len(cwd) - 1; i >= 0; i-- {
		if cwd[i] == '/' {
			return cwd[i+1:]
		}
	}
	return cwd
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	for i := len(shell) - 1; i >= 0; i-- {
		if shell[i] == '/' {
			return shell[i+1:]
		}
	}
	if shell != "" {
		return shell
	}
	return "sh"
}

func firstWord(s string) string {
	for i, c := range s {
		if c == ' ' || c == '\t' {
			return s[:i]
		}
	}
	return s
}

// ── oc inject ─────────────────────────────────────────────────────────────────

func buildInjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inject",
		Short: "Inject OpenContext memory into third-party AI agent files",
		Long: `Adds an inject_targets entry to your OpenContext subscription config
so that memory.md is automatically pushed into the target agent's
memory file (Hermes MEMORY.md, OpenClaw MEMORY.md, etc.) on every
refresh cycle.

The injected block is wrapped in HTML comment markers so the agent's
own memory is never overwritten:

  <!-- opencontext:start -->
  ## OpenContext — Recent Activity
  ...generated content...
  <!-- opencontext:end -->`,
	}
	cmd.AddCommand(buildInjectHermesCmd())
	cmd.AddCommand(buildInjectOpenClawCmd())
	return cmd
}

func buildInjectHermesCmd() *cobra.Command {
	var (
		memoryPath string
		header     string
		configFile string
	)

	cmd := &cobra.Command{
		Use:   "hermes",
		Short: "Inject memory into Hermes Agent (~/.hermes/memories/MEMORY.md)",
		Long: `Adds Hermes's MEMORY.md as an inject_target in your OpenContext
subscription config. After the next refresh cycle, OpenContext will
maintain an "OpenContext — Recent Activity" section in that file.

Hermes also reads .hermes.md / AGENTS.md / CLAUDE.md from the project
directory — those files are already populated if you have a project
subscription with claude_md configured.`,
		Example: `  oc inject hermes
  oc inject hermes --memory ~/.hermes/memories/MEMORY.md
  oc inject hermes --header "## Recent Dev Activity"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installInjectTarget("hermes", memoryPath, header, configFile)
		},
	}

	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&memoryPath, "memory", filepath.Join(home, ".hermes", "memories", "MEMORY.md"), "path to Hermes MEMORY.md")
	cmd.Flags().StringVar(&header, "header", "## OpenContext — Recent Activity", "section heading inside the injected block")
	cmd.Flags().StringVar(&configFile, "config", "", "OpenContext config file (default: ~/.opencontext/config.yaml)")
	return cmd
}

func buildInjectOpenClawCmd() *cobra.Command {
	var (
		memoryPath string
		header     string
		configFile string
	)

	cmd := &cobra.Command{
		Use:   "openclaw",
		Short: "Inject memory into OpenClaw workspace (~/.openclaw/workspace/MEMORY.md)",
		Long: `Adds OpenClaw's workspace MEMORY.md as an inject_target in your
OpenContext subscription config. After the next refresh cycle,
OpenContext will maintain an "OpenContext — Recent Activity" section
in that file.

If your OpenClaw agents use a custom workspace path, pass it with --memory.`,
		Example: `  oc inject openclaw
  oc inject openclaw --memory ~/.openclaw/my-agent/MEMORY.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installInjectTarget("openclaw", memoryPath, header, configFile)
		},
	}

	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&memoryPath, "memory", filepath.Join(home, ".openclaw", "workspace", "MEMORY.md"), "path to OpenClaw MEMORY.md")
	cmd.Flags().StringVar(&header, "header", "## OpenContext — Recent Activity", "section heading inside the injected block")
	cmd.Flags().StringVar(&configFile, "config", "", "OpenContext config file (default: ~/.opencontext/config.yaml)")
	return cmd
}

// installInjectTarget patches the first raw_dump subscription in config.yaml
// to add the given path as an inject_target, then writes the file back.
func installInjectTarget(tool, memoryPath, header, configFile string) error {
	if configFile == "" {
		home, _ := os.UserHomeDir()
		configFile = filepath.Join(home, ".opencontext", "config.yaml")
	}

	// Read the raw YAML so we can do a targeted append without losing formatting.
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("read config %s: %w\n\nRun 'oc daemon' first to create the default config.", configFile, err)
	}

	content := string(data)

	// Check if this target is already registered.
	if containsStr(content, memoryPath) {
		fmt.Printf("%s inject target already registered: %s\n", tool, memoryPath)
		return nil
	}

	// Build the YAML snippet to inject.
	// We append under the first subscription's memory block.
	// If inject_targets already exists we add a new entry; otherwise we add the block.
	snippet := fmt.Sprintf("        - path: %s\n          header: \"%s\"\n", memoryPath, header)

	if containsStr(content, "inject_targets:") {
		// inject_targets block already exists — append our entry after the last one.
		idx := strings.LastIndex(content, "inject_targets:")
		insertAt := strings.Index(content[idx:], "\n")
		if insertAt == -1 {
			content += "\n" + snippet
		} else {
			// Find the end of the inject_targets block (next key at same indentation level).
			blockStart := idx + insertAt + 1
			// Append before next top-level memory key.
			content = content[:blockStart] + snippet + content[blockStart:]
		}
	} else {
		// No inject_targets yet — add the block after the first `memory:` occurrence.
		memIdx := strings.Index(content, "    memory:")
		if memIdx == -1 {
			return fmt.Errorf("could not find 'memory:' block in %s\n\nAdd inject_targets manually — see docs/COLLECTORS.md", configFile)
		}
		// Find end of memory block's first line.
		lineEnd := strings.Index(content[memIdx:], "\n")
		if lineEnd == -1 {
			content += "\n      inject_targets:\n" + snippet
		} else {
			insertAt := memIdx + lineEnd + 1
			content = content[:insertAt] +
				"      inject_targets:\n" + snippet +
				content[insertAt:]
		}
	}

	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("%s inject target installed.\n", tool)
	fmt.Printf("  target file: %s\n", memoryPath)
	fmt.Printf("  config:      %s\n", configFile)
	fmt.Println("\nRestart the OpenContext daemon (or run: make restart) for changes to take effect.")
	fmt.Println("The memory section will be injected on the next refresh cycle.")
	return nil
}

// ── output helpers ────────────────────────────────────────────────────────────

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func buildEventSummary(e *event.ActivityEvent) string {
	summary := firstEventString(e,
		"summary",
		"message",
		"command",
		"text",
		"title",
		"url",
		"control_name",
		"window_title",
		"app_name",
		"app",
		"project",
	)
	if summary == "" {
		summary = compactEventFields(e)
	}
	if summary == "" {
		summary = fmt.Sprintf("%s.%s", e.Source, e.Type)
	}
	if project := e.Labels["project"]; project != "" && !strings.Contains(summary, project) {
		summary = "[" + project + "] " + summary
	}
	if exit := e.Labels["exit_code"]; exit != "" && exit != "0" {
		summary += " (exit " + exit + ")"
	}
	return truncateSingleLine(summary, 80)
}

func firstEventString(e *event.ActivityEvent, keys ...string) string {
	for _, key := range keys {
		if v := valueAsString(e.Payload[key]); v != "" {
			return v
		}
		if v := e.Labels[key]; v != "" {
			return v
		}
	}
	return ""
}

func valueAsString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}

func compactEventFields(e *event.ActivityEvent) string {
	parts := []string{}
	for _, key := range sortedStringKeys(e.Labels) {
		if key == "project" || key == "exit_code" {
			continue
		}
		parts = append(parts, key+"="+e.Labels[key])
		if len(parts) >= 3 {
			return strings.Join(parts, " ")
		}
	}
	payload := map[string]string{}
	for key, val := range e.Payload {
		if s := valueAsString(val); s != "" {
			payload[key] = s
		}
	}
	for _, key := range sortedStringKeys(payload) {
		parts = append(parts, key+"="+payload[key])
		if len(parts) >= 3 {
			break
		}
	}
	return strings.Join(parts, " ")
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func truncateSingleLine(s string, max int) string {
	for _, nl := range []string{"\r\n", "\n", "\r", "\t"} {
		s = strings.ReplaceAll(s, nl, " ")
	}
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func withResolvedCollectorVersions(in []registry.CollectorManifest) []registry.CollectorManifest {
	out := make([]registry.CollectorManifest, len(in))
	copy(out, in)
	for i := range out {
		out[i].Version = resolveCollectorVersion(out[i].Version)
	}
	return out
}

func resolveCollectorVersion(v string) string {
	if v == "bundled" {
		return version
	}
	return v
}

func sortSchemas(schemas []*event.EventTypeSchema) {
	sort.Slice(schemas, func(i, j int) bool {
		if schemas[i].Source == schemas[j].Source {
			return schemas[i].Type < schemas[j].Type
		}
		return schemas[i].Source < schemas[j].Source
	})
}

func parseSinceDuration(s string) int64 {
	if s == "" {
		return time.Now().Add(-24 * time.Hour).UnixMilli()
	}
	// Try duration format: 2h, 30m, 7d
	if len(s) > 0 {
		unit := s[len(s)-1]
		val, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err == nil {
			switch unit {
			case 'h':
				return time.Now().Add(-time.Duration(val * float64(time.Hour))).UnixMilli()
			case 'm':
				return time.Now().Add(-time.Duration(val * float64(time.Minute))).UnixMilli()
			case 'd':
				return time.Now().Add(-time.Duration(val * float64(24*time.Hour))).UnixMilli()
			}
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d).UnixMilli()
	}
	return time.Now().Add(-24 * time.Hour).UnixMilli()
}

func formatNum(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case int64:
		return strconv.FormatInt(n, 10)
	case int:
		return strconv.Itoa(n)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func printLastLines(path string, n int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read log %s: %w", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := 0
	if n > 0 && len(lines) > n {
		start = len(lines) - n
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}
	return nil
}

func followFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err == io.EOF {
			time.Sleep(300 * time.Millisecond)
			reader.Reset(f)
			continue
		}
		if err != nil {
			return err
		}
	}
}
