package git

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Runner 定义Git命令执行接口
type Runner interface {
	Run(args ...string) ([]byte, error)
	RunInDir(dir string, args ...string) ([]byte, error)
	RunWithTimeout(timeout time.Duration, args ...string) ([]byte, error)
}

// CommandRunner 通过命令行执行Git命令
// CommandRunner 实现了Runner接口，用于执行Git命令
type CommandRunner struct {
	// 现有字段
	GitPath string
	
	// 添加新字段
	Verbose bool // 是否显示详细输出
	Quiet   bool // 是否静默运行
}

// NewCommandRunner 创建命令执行器
func NewCommandRunner() *CommandRunner {
	return &CommandRunner{
		GitPath: "git", // 默认使用PATH中的git
	}
}

// Run 执行Git命令
func (r *CommandRunner) Run(args ...string) ([]byte, error) {
	cmd := exec.Command(r.GitPath, args...)
	return cmd.CombinedOutput()
}

// RunInDir 在指定目录执行Git命令
func (r *CommandRunner) RunInDir(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command(r.GitPath, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// RunWithTimeout 带超时执行Git命令
func (r *CommandRunner) RunWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, r.GitPath, args...)
	return cmd.CombinedOutput()
}

// CloneOptions 克隆选项
type CloneOptions struct {
	Depth  int
	Branch string
	Mirror bool
}

// FetchOptions 获取选项
type FetchOptions struct {
	Prune bool
	Tags  bool
	Depth int
}

// Repository 表示一个Git仓库
type Repository struct {
	Path   string
	Runner Runner
}

// NewRepository 创建仓库对象
func NewRepository(path string, runner Runner) *Repository {
	return &Repository{
		Path:   path,
		Runner: runner,
	}
}

// Clone 克隆仓库
func (r *Repository) Clone(url string, opts CloneOptions) error {
	args := []string{"clone"}
	
	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
	}
	
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
	}
	
	if opts.Mirror {
		args = append(args, "--mirror")
	}
	
	args = append(args, url, r.Path)
	
	_, err := r.Runner.Run(args...)
	return err
}

// Fetch 获取更新
func (r *Repository) Fetch(remote string, opts FetchOptions) error {
	args := []string{"fetch"}
	
	if opts.Prune {
		args = append(args, "--prune")
	}
	
	if opts.Tags {
		args = append(args, "--tags")
	}
	
	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
	}
	
	args = append(args, remote)
	
	_, err := r.Runner.RunInDir(r.Path, args...)
	return err
}

// Checkout 切换分支
func (r *Repository) Checkout(revision string) error {
	_, err := r.Runner.RunInDir(r.Path, "checkout", revision)
	return err
}

// Status 获取状态
func (r *Repository) Status() ([]byte, error) {
	return r.Runner.RunInDir(r.Path, "status", "--porcelain")
}

// IsClean 检查工作区是否干净
func (r *Repository) IsClean() (bool, error) {
	status, err := r.Status()
	if err != nil {
		return false, err
	}
	
	return len(status) == 0, nil
}