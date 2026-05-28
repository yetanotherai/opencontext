//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const systemdServiceName = ServiceName + ".service"

type systemdManager struct {
	system bool
}

func newPlatformManager() (Manager, error) {
	if _, err := exec.LookPath("systemctl"); err == nil {
		isRoot := os.Getuid() == 0
		m := &systemdManager{system: isRoot}
		if err := checkSystemdRunning(isRoot); err == nil {
			return m, nil
		}
	}
	return newProcessManager(), nil
}

func (m *systemdManager) Platform() string {
	if m.system {
		return "systemd (system)"
	}
	return "systemd (user)"
}

func (m *systemdManager) Install(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(m.unitPath()), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(m.unitPath(), []byte(m.buildUnit(cfg)), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		m.sysArgs("daemon-reload"),
		m.sysArgs("enable", systemdServiceName),
		m.sysArgs("restart", systemdServiceName),
	} {
		if out, err := runSystemctl(args...); err != nil {
			return fmt.Errorf("systemctl %s: %s (%w)", strings.Join(args, " "), out, err)
		}
	}
	return nil
}

func (m *systemdManager) Uninstall() error {
	_, _ = runSystemctl(m.sysArgs("disable", "--now", systemdServiceName)...)
	if err := os.Remove(m.unitPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = runSystemctl(m.sysArgs("daemon-reload")...)
	return nil
}

func (m *systemdManager) Start() error {
	out, err := runSystemctl(m.sysArgs("start", systemdServiceName)...)
	if err != nil {
		return fmt.Errorf("start: %s (%w)", out, err)
	}
	return nil
}

func (m *systemdManager) Stop() error {
	out, err := runSystemctl(m.sysArgs("stop", systemdServiceName)...)
	if err != nil {
		return fmt.Errorf("stop: %s (%w)", out, err)
	}
	return nil
}

func (m *systemdManager) Restart() error {
	out, err := runSystemctl(m.sysArgs("restart", systemdServiceName)...)
	if err != nil {
		return fmt.Errorf("restart: %s (%w)", out, err)
	}
	return nil
}

func (m *systemdManager) Status() (*Status, error) {
	st := &Status{Platform: m.Platform()}
	if _, err := os.Stat(m.unitPath()); err != nil {
		return st, nil
	}
	st.Installed = true
	out, err := runSystemctl(m.sysArgs("show", systemdServiceName, "--no-page", "--property", "ActiveState,MainPID")...)
	if err != nil {
		return st, nil
	}
	props := parseKeyValue(out)
	st.Running = props["ActiveState"] == "active"
	if pid, err := strconv.Atoi(props["MainPID"]); err == nil && pid > 0 {
		st.PID = pid
	}
	return st, nil
}

func (m *systemdManager) sysArgs(args ...string) []string {
	if m.system {
		return args
	}
	return append([]string{"--user"}, args...)
}

func (m *systemdManager) unitPath() string {
	if m.system {
		return filepath.Join("/etc/systemd/system", systemdServiceName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", systemdServiceName)
}

func (m *systemdManager) buildUnit(cfg Config) string {
	args := strconv.Quote(cfg.BinaryPath) + " daemon run"
	if cfg.ConfigFile != "" {
		args += " --config " + strconv.Quote(cfg.ConfigFile)
	}
	var sb strings.Builder
	sb.WriteString("[Unit]\n")
	sb.WriteString("Description=OpenContext local daemon\n")
	sb.WriteString("After=network-online.target\n\n")
	sb.WriteString("[Service]\n")
	sb.WriteString("Type=simple\n")
	fmt.Fprintf(&sb, "ExecStart=%s\n", args)
	fmt.Fprintf(&sb, "WorkingDirectory=%s\n", cfg.WorkDir)
	sb.WriteString("Restart=on-failure\n")
	sb.WriteString("RestartSec=5\n")
	sb.WriteString("KillSignal=SIGTERM\n")
	sb.WriteString("TimeoutStopSec=15\n")
	fmt.Fprintf(&sb, "Environment=\"OC_LOG_FILE=%s\"\n", cfg.LogFile)
	fmt.Fprintf(&sb, "Environment=\"OC_LOG_MAX_SIZE=%d\"\n", cfg.LogMaxSize)
	if cfg.EnvPATH != "" {
		fmt.Fprintf(&sb, "Environment=\"PATH=%s\"\n", cfg.EnvPATH)
	}
	sb.WriteString("\n[Install]\n")
	if m.system {
		sb.WriteString("WantedBy=multi-user.target\n")
	} else {
		sb.WriteString("WantedBy=default.target\n")
	}
	return sb.String()
}

func runSystemctl(args ...string) (string, error) {
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func checkSystemdRunning(system bool) error {
	args := []string{"--user", "is-system-running"}
	if system {
		args = []string{"is-system-running"}
	}
	out, err := runSystemctl(args...)
	state := strings.ToLower(strings.TrimSpace(out))
	if err == nil || state == "running" || state == "degraded" || state == "starting" || state == "initializing" {
		return nil
	}
	return fmt.Errorf("systemd unavailable: %s", state)
}

func parseKeyValue(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

func CheckLinger() (bool, string) {
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	if os.Getuid() == 0 {
		return true, user
	}
	out, err := exec.Command("loginctl", "show-user", user, "-p", "Linger").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Linger=yes", user
}
