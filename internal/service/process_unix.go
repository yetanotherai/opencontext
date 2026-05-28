//go:build linux

package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type processManager struct{}

func newProcessManager() Manager { return &processManager{} }

func (*processManager) Platform() string { return "process" }

func (m *processManager) Install(cfg Config) error {
	if err := os.MkdirAll(DefaultDataDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
		return err
	}
	return m.startWithConfig(cfg)
}

func (*processManager) Uninstall() error {
	_ = (&processManager{}).Stop()
	_ = os.Remove(pidPath())
	return nil
}

func (*processManager) Start() error {
	meta, err := LoadMeta()
	if err != nil {
		return fmt.Errorf("load service metadata: %w", err)
	}
	cfg := Config{
		BinaryPath: meta.BinaryPath,
		WorkDir:    meta.WorkDir,
		ConfigFile: meta.ConfigFile,
		LogFile:    meta.LogFile,
		LogMaxSize: meta.LogMaxSize,
		EnvPATH:    os.Getenv("PATH"),
	}
	return (&processManager{}).startWithConfig(cfg)
}

func (*processManager) Stop() error {
	pid, err := readPID()
	if err != nil {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	for i := 0; i < 50; i++ {
		if !pidRunning(pid) {
			_ = os.Remove(pidPath())
			return nil
		}
		sleepMillis(100)
	}
	_ = proc.Signal(syscall.SIGKILL)
	_ = os.Remove(pidPath())
	return nil
}

func (m *processManager) Restart() error {
	_ = m.Stop()
	return m.Start()
}

func (*processManager) Status() (*Status, error) {
	st := &Status{Platform: "process"}
	if _, err := os.Stat(metaPath()); err == nil {
		st.Installed = true
	}
	pid, err := readPID()
	if err == nil && pidRunning(pid) {
		st.Running = true
		st.PID = pid
	}
	return st, nil
}

func (*processManager) startWithConfig(cfg Config) error {
	if pid, err := readPID(); err == nil && pidRunning(pid) {
		return nil
	}
	args := []string{"daemon", "run"}
	if cfg.ConfigFile != "" {
		args = append(args, "--config", cfg.ConfigFile)
	}
	args = append(args, "--log-level", "info")
	cmd := exec.Command(cfg.BinaryPath, args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(os.Environ(),
		"OC_LOG_FILE="+cfg.LogFile,
		"OC_LOG_MAX_SIZE="+strconv.FormatInt(cfg.LogMaxSize, 10),
	)
	if cfg.EnvPATH != "" {
		cmd.Env = append(cmd.Env, "PATH="+cfg.EnvPATH)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := os.WriteFile(pidPath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	return cmd.Process.Release()
}

func pidPath() string {
	return filepath.Join(DefaultDataDir(), "daemon.pid")
}

func readPID() (int, error) {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func pidRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func sleepMillis(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
