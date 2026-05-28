package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultLogMaxSize = 10 * 1024 * 1024
	ServiceName       = "opencontext"
)

type Config struct {
	BinaryPath string
	WorkDir    string
	ConfigFile string
	LogFile    string
	LogMaxSize int64
	EnvPATH    string
}

type Status struct {
	Installed bool
	Running   bool
	PID       int
	Platform  string
}

type Manager interface {
	Install(Config) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (*Status, error)
	Platform() string
}

func NewManager() (Manager, error) {
	return newPlatformManager()
}

func DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".opencontext")
}

func DefaultLogFile() string {
	return filepath.Join(DefaultDataDir(), "logs", "oc.log")
}

type Meta struct {
	LogFile     string `json:"log_file"`
	LogMaxSize  int64  `json:"log_max_size"`
	WorkDir     string `json:"work_dir"`
	ConfigFile  string `json:"config_file"`
	BinaryPath  string `json:"binary_path"`
	Platform    string `json:"platform"`
	InstalledAt string `json:"installed_at"`
}

func Resolve(cfg *Config) error {
	if cfg.BinaryPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		cfg.BinaryPath = exe
	}
	if cfg.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working directory: %w", err)
		}
		cfg.WorkDir = wd
	}
	cfg.WorkDir = expandHome(cfg.WorkDir)
	cfg.ConfigFile = expandHome(cfg.ConfigFile)
	if cfg.LogFile == "" {
		cfg.LogFile = DefaultLogFile()
	}
	cfg.LogFile = expandHome(cfg.LogFile)
	if cfg.LogMaxSize <= 0 {
		cfg.LogMaxSize = DefaultLogMaxSize
	}
	if cfg.EnvPATH == "" {
		cfg.EnvPATH = os.Getenv("PATH")
	}
	return nil
}

func expandHome(path string) string {
	if path == "" || path == "~" {
		if path == "~" {
			home, _ := os.UserHomeDir()
			return home
		}
		return path
	}
	if len(path) > 2 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func SaveMeta(m *Meta) error {
	if err := os.MkdirAll(DefaultDataDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(), data, 0o644)
}

func LoadMeta() (*Meta, error) {
	data, err := os.ReadFile(metaPath())
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func RemoveMeta() {
	_ = os.Remove(metaPath())
}

func NowISO() string {
	return time.Now().Format(time.RFC3339)
}

func metaPath() string {
	return filepath.Join(DefaultDataDir(), "daemon.json")
}
