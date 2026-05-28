//go:build darwin

package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const launchdLabel = "ai.opencontext.daemon"

type launchdManager struct{}

func newPlatformManager() (Manager, error) { return &launchdManager{}, nil }

func (*launchdManager) Platform() string { return "launchd" }

func (*launchdManager) Install(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(plistPath()), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
		return err
	}
	bootoutLaunchdTargets()
	if err := os.WriteFile(plistPath(), []byte(buildPlist(cfg)), 0o644); err != nil {
		return err
	}
	domain := preferredLaunchdDomain()
	if out, err := runLaunchctl("bootstrap", domain, plistPath()); err != nil {
		return fmt.Errorf("launchctl bootstrap: %s (%w)", out, err)
	}
	if out, err := runLaunchctl("kickstart", "-kp", launchdTarget(domain)); err != nil {
		return fmt.Errorf("launchctl kickstart: %s (%w)", out, err)
	}
	return nil
}

func (*launchdManager) Uninstall() error {
	bootoutLaunchdTargets()
	if err := os.Remove(plistPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (*launchdManager) Start() error {
	if _, target, _, ok := loadedLaunchdTarget(); ok {
		out, err := runLaunchctl("kickstart", "-kp", target)
		if err != nil {
			return fmt.Errorf("start: %s (%w)", out, err)
		}
		return nil
	}
	domain := preferredLaunchdDomain()
	if out, err := runLaunchctl("bootstrap", domain, plistPath()); err != nil {
		return fmt.Errorf("start: %s (%w)", out, err)
	}
	return nil
}

func (*launchdManager) Stop() error {
	var lastOut string
	var lastErr error
	for _, target := range launchdTargets() {
		out, err := runLaunchctl("bootout", target)
		if err == nil {
			return nil
		}
		lastOut, lastErr = out, err
	}
	if lastErr != nil {
		return fmt.Errorf("stop: %s (%w)", lastOut, lastErr)
	}
	return nil
}

func (m *launchdManager) Restart() error {
	_ = m.Stop()
	time.Sleep(300 * time.Millisecond)
	return m.Start()
}

func (*launchdManager) Status() (*Status, error) {
	st := &Status{Platform: "launchd"}
	if _, err := os.Stat(plistPath()); err != nil {
		return st, nil
	}
	st.Installed = true
	_, _, out, ok := loadedLaunchdTarget()
	if !ok {
		return st, nil
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pid = ") {
			if pid, err := strconv.Atoi(strings.TrimPrefix(line, "pid = ")); err == nil {
				st.PID = pid
				st.Running = pid > 0
			}
		}
		if strings.Contains(line, "state = running") {
			st.Running = true
		}
	}
	return st, nil
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func runLaunchctl(args ...string) (string, error) {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func preferredLaunchdDomain() string {
	gui := fmt.Sprintf("gui/%d", os.Getuid())
	if _, err := runLaunchctl("print", gui); err == nil {
		return gui
	}
	return fmt.Sprintf("user/%d", os.Getuid())
}

func launchdTarget(domain string) string { return domain + "/" + launchdLabel }

func launchdTargets() []string {
	return []string{
		launchdTarget(fmt.Sprintf("gui/%d", os.Getuid())),
		launchdTarget(fmt.Sprintf("user/%d", os.Getuid())),
	}
}

func loadedLaunchdTarget() (string, string, string, bool) {
	for _, domain := range []string{fmt.Sprintf("gui/%d", os.Getuid()), fmt.Sprintf("user/%d", os.Getuid())} {
		target := launchdTarget(domain)
		out, err := runLaunchctl("print", target)
		if err == nil {
			return domain, target, out, true
		}
	}
	return "", "", "", false
}

func bootoutLaunchdTargets() {
	for _, target := range launchdTargets() {
		_, _ = runLaunchctl("bootout", target)
	}
}

func buildPlist(cfg Config) string {
	args := "		<string>" + xmlEscape(cfg.BinaryPath) + "</string>\n		<string>daemon</string>\n		<string>run</string>\n"
	if cfg.ConfigFile != "" {
		args += "		<string>--config</string>\n		<string>" + xmlEscape(cfg.ConfigFile) + "</string>\n"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
	<key>WorkingDirectory</key>
	<string>%s</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict><key>SuccessfulExit</key><false/></dict>
	<key>EnvironmentVariables</key>
	<dict>
		<key>OC_LOG_FILE</key><string>%s</string>
		<key>OC_LOG_MAX_SIZE</key><string>%d</string>
		<key>PATH</key><string>%s</string>
	</dict>
	<key>StandardOutPath</key><string>/dev/null</string>
	<key>StandardErrorPath</key><string>/dev/null</string>
</dict>
</plist>
`, launchdLabel, args, xmlEscape(cfg.WorkDir), xmlEscape(cfg.LogFile), cfg.LogMaxSize, xmlEscape(cfg.EnvPATH))
}

func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

func CheckLinger() (bool, string) { return true, "" }
