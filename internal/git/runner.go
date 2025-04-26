package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/cix-code/gogo/internal/config"
)

// SetVerbose 设置是否显示详细输出
func (r *defaultRunner) SetVerbose(verbose bool) {
	// This method should belong to CommandRunner, not defaultRunner directly
	// Assuming CommandRunner has Verbose field
	// r.Verbose = verbose
	// For now, let's assume defaultRunner holds the state
	r.Verbose = verbose
}

// SetQuiet 设置是否静默运行
func (r *defaultRunner) SetQuiet(quiet bool) {
	// Similar assumption as SetVerbose
	r.Quiet = quiet
}

// defaultRunner 是默认的Git命令运行器实现
type defaultRunner struct {
	Verbose bool
	Quiet   bool
}

// Run 执行Git命令
func (r *defaultRunner) Run(args ...string) ([]byte, error) {
	return r.runGitCommand("", 0, args...)
}

// RunInDir 在指定目录执行Git命令
func (r *defaultRunner) RunInDir(dir string, args ...string) ([]byte, error) {
	return r.runGitCommand(dir, 0, args...)
}

// RunWithTimeout 在指定目录执行Git命令并设置超时
func (r *defaultRunner) RunWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	// Assuming timeout applies globally for now, not per-directory
	return r.runGitCommand("", timeout, args...)
}

// runGitCommand 是执行 git 命令的内部辅助函数
func (r *defaultRunner) runGitCommand(dir string, timeout time.Duration, args ...string) ([]byte, error) {
	cmdArgs := append([]string{}, args...)

	if r.Verbose {
		log.Infof("Executing: git %v in dir '%s'", cmdArgs, dir)
	}

	var cmd *exec.Cmd
	ctx := context.Background()
	var cancel context.CancelFunc

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd = exec.CommandContext(ctx, "git", cmdArgs...)

	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	stdoutBytes := stdout.Bytes()
	stderrBytes := stderr.Bytes()

	if r.Verbose && len(stdoutBytes) > 0 {
		log.Debugf("Stdout: %s", string(stdoutBytes))
	}
	if len(stderrBytes) > 0 {
		// Always log stderr unless quiet
		if !r.Quiet {
			log.Warnf("Stderr: %s", string(stderrBytes))
		}
	}

	if err != nil {
		// Combine error message with stderr for better context
		errMsg := fmt.Sprintf("git %v failed in dir '%s': %v. Stderr: %s", cmdArgs, dir, err, string(stderrBytes))
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Include exit code in the error message if available
			return stdoutBytes, fmt.Errorf("%s (Exit Code: %d)", errMsg, exitErr.ExitCode())
		}
		return stdoutBytes, fmt.Errorf(errMsg)
	}

	return stdoutBytes, nil
}

// NewRunner 创建一个新的 Git 命令运行器
func NewRunner() Runner {
	return &defaultRunner{}
}

// NewCommandRunnerWithConfig 根据配置创建Git命令运行器
func NewCommandRunnerWithConfig(cfg *config.Config) (Runner, error) {
	runner := &defaultRunner{}
	if cfg != nil {
		runner.Verbose = cfg.Verbose
		runner.Quiet = cfg.Quiet
	}
	return runner, nil
}

// Runner defines the interface for running git commands
type Runner interface {
	Run(args ...string) ([]byte, error)
	RunInDir(dir string, args ...string) ([]byte, error)
	RunWithTimeout(timeout time.Duration, args ...string) ([]byte, error)
	SetVerbose(verbose bool)
	SetQuiet(quiet bool)
}

// CommandRunner 定义了运行Git命令的接口
// Moved from git.go for better organization
type CommandRunner interface {
	Run(args ...string) ([]byte, error)
	RunInDir(dir string, args ...string) ([]byte, error)
	RunWithTimeout(timeout time.Duration, args ...string) ([]byte, error)
	SetVerbose(verbose bool)
	SetQuiet(quiet bool)
}