package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// LocalSandbox implements the Sandbox interface using local process isolation
// This is a fallback option when Docker is not available
// It provides basic isolation through:
// - Command whitelist validation
// - Working directory restriction
// - Timeout enforcement
// - Environment variable filtering
type LocalSandbox struct {
	config *Config
}

// NewLocalSandbox creates a new local process-based sandbox
func NewLocalSandbox(config *Config) *LocalSandbox {
	if config == nil {
		config = DefaultConfig()
	}
	return &LocalSandbox{
		config: config,
	}
}

// Type returns the sandbox type
func (s *LocalSandbox) Type() SandboxType {
	return SandboxTypeLocal
}

// IsAvailable checks if local sandbox is available
func (s *LocalSandbox) IsAvailable(ctx context.Context) bool {
	// Local sandbox is always available
	return true
}

// Execute runs a script locally with basic isolation
func (s *LocalSandbox) Execute(ctx context.Context, config *ExecuteConfig) (*ExecuteResult, error) {
	if config == nil {
		return nil, ErrInvalidScript
	}

	// Validate the script path
	if err := s.validateScript(config.Script); err != nil {
		return nil, err
	}

	// Determine interpreter
	interpreter := s.getInterpreter(config.Script)
	if !s.isAllowedCommand(interpreter) {
		return nil, fmt.Errorf("interpreter not allowed: %s", interpreter)
	}

	// Set default timeout
	timeout := config.Timeout
	if timeout == 0 {
		timeout = s.config.DefaultTimeout
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build command
	args := append([]string{config.Script}, config.Args...)
	cmd := exec.CommandContext(execCtx, interpreter, args...)

	// Set working directory
	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
	} else {
		cmd.Dir = filepath.Dir(config.Script)
	}

	// Setup minimal environment
	cmd.Env = s.buildEnvironment(config.Env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if config.Stdin != "" {
		cmd.Stdin = strings.NewReader(config.Stdin)
	}

	startTime := time.Now()
	stopProcess, cleanupProcess, err := startProcess(cmd)
	if err != nil {
		result := &ExecuteResult{Error: err.Error(), ExitCode: -1}
		return result, nil
	}
	defer cleanupProcess()
	processDone := make(chan struct{})
	defer close(processDone)
	go func() {
		select {
		case <-execCtx.Done():
			stopProcess()
		case <-processDone:
		}
	}()
	err = cmd.Wait()
	duration := time.Since(startTime)

	result := &ExecuteResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			stopProcess()
			result.Killed = true
			result.Error = ErrTimeout.Error()
			result.ExitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err.Error()
			result.ExitCode = -1
		}
	}

	return result, nil
}

// validateScript checks if the script path is valid and safe
func (s *LocalSandbox) validateScript(scriptPath string) error {
	// Check if script exists
	info, err := os.Stat(scriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrScriptNotFound
		}
		return fmt.Errorf("failed to access script: %w", err)
	}

	if info.IsDir() {
		return ErrInvalidScript
	}

	// Check path is absolute
	if !filepath.IsAbs(scriptPath) {
		return fmt.Errorf("script path must be absolute: %s", scriptPath)
	}

	// Validate against allowed paths if configured
	if len(s.config.AllowedPaths) > 0 {
		allowed := false
		absPath, _ := filepath.Abs(scriptPath)
		for _, allowedPath := range s.config.AllowedPaths {
			absAllowed, _ := filepath.Abs(allowedPath)
			if strings.HasPrefix(absPath, absAllowed) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("script path not in allowed paths: %s", scriptPath)
		}
	}

	return nil
}

// getInterpreter returns the appropriate interpreter for a script
func (s *LocalSandbox) getInterpreter(scriptPath string) string {
	ext := strings.ToLower(filepath.Ext(scriptPath))
	switch ext {
	case ".py":
		if runtime.GOOS == "windows" {
			return "python"
		}
		return "python3"
	case ".sh", ".bash":
		if runtime.GOOS == "windows" {
			if gitPath, err := exec.LookPath("git.exe"); err == nil {
				gitBash := filepath.Join(filepath.Dir(filepath.Dir(gitPath)), "bin", "bash.exe")
				if _, err := os.Stat(gitBash); err == nil {
					return gitBash
				}
			}
		}
		return "bash"
	case ".js":
		return "node"
	case ".rb":
		return "ruby"
	case ".pl":
		return "perl"
	case ".php":
		return "php"
	default:
		return "sh"
	}
}

// isAllowedCommand checks if a command is in the allowed list
func (s *LocalSandbox) isAllowedCommand(cmd string) bool {
	commandName := strings.TrimSuffix(strings.ToLower(filepath.Base(cmd)), ".exe")
	cmd = strings.ToLower(cmd)
	if len(s.config.AllowedCommands) == 0 {
		// Use default allowed commands
		defaults := defaultAllowedCommands()
		for _, allowed := range defaults {
			if cmd == strings.ToLower(allowed) || commandName == strings.TrimSuffix(strings.ToLower(allowed), ".exe") {
				return true
			}
		}
		return false
	}

	for _, allowed := range s.config.AllowedCommands {
		if cmd == strings.ToLower(allowed) || commandName == strings.TrimSuffix(strings.ToLower(allowed), ".exe") {
			return true
		}
	}
	return false
}

// buildEnvironment creates a safe environment for script execution
func (s *LocalSandbox) buildEnvironment(extra map[string]string) []string {
	// Start with minimal environment
	pathValue := "/usr/local/bin:/usr/bin:/bin"
	if runtime.GOOS == "windows" {
		pathValue = os.Getenv("PATH")
	}
	env := []string{"PATH=" + pathValue, "HOME=/tmp", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8"}
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"))
	}

	// Dangerous environment variables to exclude
	dangerous := map[string]bool{
		"LD_PRELOAD":      true,
		"LD_LIBRARY_PATH": true,
		"PYTHONPATH":      true,
		"NODE_OPTIONS":    true,
		"BASH_ENV":        true,
		"ENV":             true,
		"SHELL":           true,
	}

	// Add extra environment variables (filtered)
	for key, value := range extra {
		upperKey := strings.ToUpper(key)
		if dangerous[upperKey] {
			continue
		}
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	return env
}

// Cleanup releases any resources
func (s *LocalSandbox) Cleanup(ctx context.Context) error {
	// Local sandbox doesn't need cleanup
	return nil
}
