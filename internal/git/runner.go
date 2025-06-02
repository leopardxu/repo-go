package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
)

// 包级别的日志记录�?
var log logger.Logger = &logger.DefaultLogger{}

// SetLogger 设置日志记录�?
func SetLogger(logger logger.Logger) {
	log = logger
}

// GitCommandError 表示Git命令执行错误
type GitCommandError struct {
	Command string
	Dir     string
	Err     error
	Stdout  string
	Stderr  string
	ExitCode int
}

func (e *GitCommandError) Error() string {
	if e.ExitCode != 0 {
		return fmt.Sprintf("git command error: '%s' in dir '%s': exit code %d: %v", e.Command, e.Dir, e.ExitCode, e.Err)
	}
	return fmt.Sprintf("git command error: '%s' in dir '%s': %v", e.Command, e.Dir, e.Err)
}

func (e *GitCommandError) Unwrap() error {
	return e.Err
}

// defaultRunner 是默认的Git命令运行器实�?
type defaultRunner struct {
	Verbose     bool
	Quiet       bool
	MaxRetries  int
	RetryDelay  time.Duration
	concurrency int
	semaphore   chan struct{}
	mutex       sync.RWMutex
}

// SetVerbose 设置是否显示详细输出
func (r *defaultRunner) SetVerbose(verbose bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.Verbose = verbose
}

// SetQuiet 设置是否静默运行
func (r *defaultRunner) SetQuiet(quiet bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.Quiet = quiet
}

// SetMaxRetries 设置最大重试次�?
func (r *defaultRunner) SetMaxRetries(retries int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.MaxRetries = retries
}

// SetRetryDelay 设置重试延迟
func (r *defaultRunner) SetRetryDelay(delay time.Duration) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.RetryDelay = delay
}

// SetConcurrency 设置并发�?
func (r *defaultRunner) SetConcurrency(concurrency int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	// 如果已经初始化了信号量，先关闭它
	if r.semaphore != nil {
		close(r.semaphore)
	}
	
	// 设置新的并发�?
	r.concurrency = concurrency
	if concurrency > 0 {
		r.semaphore = make(chan struct{}, concurrency)
	} else {
		r.semaphore = nil
	}
}

// Run 执行Git命令
func (r *defaultRunner) Run(args ...string) ([]byte, error) {
	return r.runGitCommand("", 0, args...)
}

// RunInDir 在指定目录执行Git命令
func (r *defaultRunner) RunInDir(dir string, args ...string) ([]byte, error) {
	return r.runGitCommand(dir, 0, args...)
}

// RunWithTimeout 在指定目录执行Git命令并设置超�?
func (r *defaultRunner) RunWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	return r.runGitCommand("", timeout, args...)
}

// RunInDirWithTimeout 在指定目录执行Git命令并设置超�?
func (r *defaultRunner) RunInDirWithTimeout(dir string, timeout time.Duration, args ...string) ([]byte, error) {
	return r.runGitCommand(dir, timeout, args...)
}

// runGitCommand 是执�?git 命令的内部辅助函�?
func (r *defaultRunner) runGitCommand(dir string, timeout time.Duration, args ...string) ([]byte, error) {
	// 获取并发控制信号�?
	r.mutex.RLock()
	semaphore := r.semaphore
	maxRetries := r.MaxRetries
	retryDelay := r.RetryDelay
	verbose := r.Verbose
	quiet := r.Quiet
	r.mutex.RUnlock()
	
	// 如果设置了并发控�?
	if semaphore != nil {
		semaphore <- struct{}{}
		defer func() { <-semaphore }()
	}

	cmdArgs := append([]string{}, args...)
	cmdStr := fmt.Sprintf("git %s", strings.Join(cmdArgs, " "))

	if verbose {
		log.Info("执行: %s 在目�?'%s'", cmdStr, dir)
	} else {
		log.Debug("执行: %s 在目�?'%s'", cmdStr, dir)
	}

	// 执行命令，支持重�?
	var lastErr error
	var stdoutBytes []byte
	var stderrBytes []byte
	var exitCode int
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Debug("重试Git命令 (尝试 %d/%d): %s", attempt, maxRetries, cmdStr)
			time.Sleep(retryDelay)
		}
		
		// 准备命令
		ctx := context.Background()
		var cancel context.CancelFunc

		if timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		cmd := exec.CommandContext(ctx, "git", cmdArgs...)

		if dir != "" {
			cmd.Dir = dir
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		// 执行命令
		err := cmd.Run()
		stdoutBytes = stdout.Bytes()
		stderrBytes = stderr.Bytes()
		
		// 处理输出
		if verbose && len(stdoutBytes) > 0 {
			log.Debug("标准输出: %s", string(stdoutBytes))
		}
		
		if len(stderrBytes) > 0 && !quiet {
			// 只有在非静默模式下才记录stderr
			if err != nil {
				log.Warn("标准错误: %s", string(stderrBytes))
			} else if verbose {
				// 如果命令成功但有stderr输出，且处于详细模式，则记录为调试信�?
				log.Debug("标准错误: %s", string(stderrBytes))
			}
		}

		// 处理错误
		if err != nil {
			exitCode = 1 // 默认错误�?
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			
			// 构建错误信息
			lastErr = &GitCommandError{
				Command:  cmdStr,
				Dir:      dir,
				Err:      err,
				Stdout:   string(stdoutBytes),
				Stderr:   string(stderrBytes),
				ExitCode: exitCode,
			}
			
			// 检查是否应该重�?
			if !shouldRetry(exitCode, string(stderrBytes)) || attempt >= maxRetries {
				if attempt > 0 {
					log.Warn("Git命令失败，已重试 %d �? %s", attempt, cmdStr)
				}
				break
			}
		} else {
			// 命令成功
			if attempt > 0 {
				log.Info("Git命令在第 %d 次尝试后成功: %s", attempt+1, cmdStr)
			}
			return stdoutBytes, nil
		}
	}

	return stdoutBytes, lastErr
}

// shouldRetry 判断是否应该重试Git命令
func shouldRetry(exitCode int, stderr string) bool {
	// 网络错误通常应该重试
	if strings.Contains(stderr, "Could not resolve host") ||
	   strings.Contains(stderr, "Failed to connect") ||
	   strings.Contains(stderr, "Connection timed out") ||
	   strings.Contains(stderr, "Connection reset by peer") ||
	   strings.Contains(stderr, "Operation timed out") ||
	   strings.Contains(stderr, "Temporary failure in name resolution") {
		return true
	}
	
	// 锁定错误通常应该重试
	if strings.Contains(stderr, "Unable to create") && strings.Contains(stderr, "File exists") ||
	   strings.Contains(stderr, "Unable to lock") ||
	   strings.Contains(stderr, "already exists") && strings.Contains(stderr, "lock") {
		return true
	}
	
	// 其他可能需要重试的情况
	if strings.Contains(stderr, "index.lock") ||
	   strings.Contains(stderr, "fatal: the remote end hung up unexpectedly") {
		return true
	}
	
	// 默认不重�?
	return false
}

// NewRunner 创建一个新�?Git 命令运行�?
func NewRunner() Runner {
	return &defaultRunner{
		MaxRetries:  3,
		RetryDelay:  time.Second * 2,
		concurrency: 5,
		semaphore:   make(chan struct{}, 5),
	}
}

// NewCommandRunnerWithConfig 根据配置创建Git命令运行�?
func NewCommandRunnerWithConfig(cfg *config.Config) (Runner, error) {
	runner := &defaultRunner{
		MaxRetries:  3,
		RetryDelay:  time.Second * 2,
		concurrency: 5,
		semaphore:   make(chan struct{}, 5),
	}
	
	if cfg != nil {
		runner.Verbose = cfg.Verbose
		runner.Quiet = cfg.Quiet
	}
	
	return runner, nil
}

// Runner 定义了运行Git命令的接�?
type Runner interface {
	Run(args ...string) ([]byte, error)
	RunInDir(dir string, args ...string) ([]byte, error)
	RunWithTimeout(timeout time.Duration, args ...string) ([]byte, error)
	RunInDirWithTimeout(dir string, timeout time.Duration, args ...string) ([]byte, error)
	SetVerbose(verbose bool)
	SetQuiet(quiet bool)
	SetMaxRetries(retries int)
	SetRetryDelay(delay time.Duration)
	SetConcurrency(concurrency int)
}

// CommandRunner 是Runner的别名，保持向后兼容�?
type CommandRunner = Runner
