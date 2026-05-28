//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	windowsTaskName   = ServiceName
	windowsScriptName = "opencontext-daemon.ps1"
)

type schtasksManager struct{}

func newPlatformManager() (Manager, error) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		return nil, fmt.Errorf("powershell.exe not found")
	}
	return &schtasksManager{}, nil
}

func (*schtasksManager) Platform() string { return "schtasks" }

func (m *schtasksManager) Install(cfg Config) error {
	if err := os.MkdirAll(DefaultDataDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
		return err
	}
	scriptPath := windowsTaskScriptPath()
	if err := os.WriteFile(scriptPath, []byte(buildWindowsTaskScript(cfg)), 0o644); err != nil {
		return err
	}
	_ = stopWindowsTask()
	_ = deleteWindowsTask()
	if err := createWindowsTask(scriptPath); err != nil {
		return err
	}
	return m.Start()
}

func (*schtasksManager) Uninstall() error {
	_ = stopWindowsTask()
	if err := deleteWindowsTask(); err != nil {
		return err
	}
	if err := os.Remove(windowsTaskScriptPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (*schtasksManager) Start() error { return startWindowsTask() }

func (*schtasksManager) Stop() error { return stopWindowsTask() }

func (m *schtasksManager) Restart() error {
	_ = m.Stop()
	return m.Start()
}

func (*schtasksManager) Status() (*Status, error) {
	st := &Status{Platform: "schtasks"}
	out, err := runPowerShell(fmt.Sprintf(`
$task = Get-ScheduledTask -TaskName %s -ErrorAction SilentlyContinue
if ($null -eq $task) { exit 1 }
Write-Output $task.State
`, powerShellLiteral(windowsTaskName)))
	if err != nil {
		return st, nil
	}
	st.Installed = true
	st.Running = strings.EqualFold(strings.TrimSpace(out), "Running")
	return st, nil
}

func windowsTaskScriptPath() string {
	return filepath.Join(DefaultDataDir(), windowsScriptName)
}

func buildWindowsTaskScript(cfg Config) string {
	args := []string{"daemon", "run"}
	if cfg.ConfigFile != "" {
		args = append(args, "--config", cfg.ConfigFile)
	}
	var sb strings.Builder
	sb.WriteString("$ErrorActionPreference = 'Stop'\r\n")
	writePowerShellEnv(&sb, "OC_LOG_FILE", cfg.LogFile)
	writePowerShellEnv(&sb, "OC_LOG_MAX_SIZE", strconv.FormatInt(cfg.LogMaxSize, 10))
	if cfg.EnvPATH != "" {
		writePowerShellEnv(&sb, "PATH", cfg.EnvPATH)
	}
	fmt.Fprintf(&sb, "Set-Location -LiteralPath %s\r\n", powerShellLiteral(cfg.WorkDir))
	sb.WriteString("while ($true) {\r\n")
	fmt.Fprintf(&sb, "  & %s", powerShellLiteral(cfg.BinaryPath))
	for _, arg := range args {
		fmt.Fprintf(&sb, " %s", powerShellLiteral(arg))
	}
	sb.WriteString("\r\n")
	sb.WriteString("  $exitCode = $LASTEXITCODE\r\n")
	sb.WriteString("  if ($exitCode -eq 0) { exit 0 }\r\n")
	sb.WriteString("  Start-Sleep -Seconds 5\r\n")
	sb.WriteString("}\r\n")
	return sb.String()
}

func createWindowsTask(scriptPath string) error {
	out, err := runPowerShell(fmt.Sprintf(`
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument %s
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited
Register-ScheduledTask -TaskName %s -Action $action -Trigger $trigger -Principal $principal -Force | Out-Null
`, powerShellLiteral(`-WindowStyle Hidden -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "`+scriptPath+`"`), powerShellLiteral(windowsTaskName)))
	if err != nil {
		return fmt.Errorf("register scheduled task: %s (%w)", out, err)
	}
	return nil
}

func startWindowsTask() error {
	out, err := runPowerShell(fmt.Sprintf(`Start-ScheduledTask -TaskName %s`, powerShellLiteral(windowsTaskName)))
	if err != nil {
		return fmt.Errorf("start scheduled task: %s (%w)", out, err)
	}
	return nil
}

func stopWindowsTask() error {
	out, err := runPowerShell(fmt.Sprintf(`
$task = Get-ScheduledTask -TaskName %s -ErrorAction SilentlyContinue
if ($null -eq $task) { exit 0 }
if ($task.State -eq 'Running') { Stop-ScheduledTask -TaskName %s }
`, powerShellLiteral(windowsTaskName), powerShellLiteral(windowsTaskName)))
	if err != nil {
		return fmt.Errorf("stop scheduled task: %s (%w)", out, err)
	}
	return nil
}

func deleteWindowsTask() error {
	out, err := runPowerShell(fmt.Sprintf(`
$task = Get-ScheduledTask -TaskName %s -ErrorAction SilentlyContinue
if ($null -eq $task) { exit 0 }
Unregister-ScheduledTask -TaskName %s -Confirm:$false
`, powerShellLiteral(windowsTaskName), powerShellLiteral(windowsTaskName)))
	if err != nil {
		return fmt.Errorf("delete scheduled task: %s (%w)", out, err)
	}
	return nil
}

func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference = 'Stop'\n"+script)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func writePowerShellEnv(sb *strings.Builder, key, value string) {
	fmt.Fprintf(sb, "$env:%s = %s\r\n", key, powerShellLiteral(value))
}

func powerShellLiteral(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func CheckLinger() (bool, string) { return true, "" }
