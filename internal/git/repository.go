package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repository represents a git repository
// Repository 表示一个Git仓库
type Repository struct {
	Path   string
	Runner Runner
}

// CommandError 表示Git命令执行错误
type CommandError struct {
	Command string
	Err     error
	Output  []byte
	Stdout  []byte
	Stderr  []byte
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("git command error: %s: %v", e.Command, e.Err)
}

func (e *CommandError) Unwrap() error {
	return e.Err
}

// NewRepository 创建一个新的Git仓库实例
func NewRepository(path string, runner Runner) *Repository {
	return &Repository{
		Path:   path,
		Runner: runner,
	}
}

// RunCommand 执行Git命令并返回结果
func (r *Repository) RunCommand(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, &CommandError{
			Command: fmt.Sprintf("git %v", args),
			Err:     err,
			Output:  output,
		}
	}
	return output, nil
}

// CloneOptions contains options for git clone
type CloneOptions struct {
	Depth  int
	Branch string
}

// FetchOptions contains options for git fetch
type FetchOptions struct {
	Prune bool
	Tags  bool
	Depth int
}

// Exists 检查仓库是否存在
func (r *Repository) Exists() (bool, error) {
	gitDir := filepath.Join(r.Path, ".git")
	_, err := os.Stat(gitDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Clone clones a git repository
func (r *Repository) Clone(url string, opts CloneOptions) error {
	args := []string{"clone"}
	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
	}
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
	}
	args = append(args, url, r.Path)
	_, err := r.Runner.Run(args...)
	return err
}

// Fetch fetches updates from remote
func (r *Repository) Fetch(remote string, opts FetchOptions) error {
	args := []string{"fetch", remote}
	if opts.Prune {
		args = append(args, "--prune")
	}
	if opts.Tags {
		args = append(args, "--tags")
	}
	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
	}
	_, err := r.Runner.RunInDir(r.Path, args...)
	return err
}

// Checkout checks out a specific revision
func (r *Repository) Checkout(revision string) error {
	_, err := r.Runner.RunInDir(r.Path, "checkout", revision)
	return err
}

// Status gets the repository status
func (r *Repository) Status() (string, error) {
	output, err := r.Runner.RunInDir(r.Path, "status", "--porcelain")
	return string(output), err
}

// IsClean checks if the repository is clean
func (r *Repository) IsClean() (bool, error) {
	status, err := r.Status()
	if err != nil {
		return false, err
	}
	return status == "", nil
}

// BranchExists 检查分支是否存在
func (r *Repository) BranchExists(branch string) (bool, error) {
	// 执行git命令检查分支是否存在
	_, err := r.Runner.RunInDir(r.Path, "rev-parse", "--verify", branch)
	if err != nil {
		// 如果命令失败，分支不存在
		return false, nil
	}
	return true, nil
}

// CurrentBranch 获取当前分支名称
func (r *Repository) CurrentBranch() (string, error) {
	// 执行git命令获取当前分支
	output, err := r.Runner.RunInDir(r.Path, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// HasRevision 检查是否有指定的修订版本
func (r *Repository) HasRevision(revision string) (bool, error) {
	_, err := r.Runner.RunInDir(r.Path, "rev-parse", "--verify", revision)
	if err != nil {
		return false, nil
	}
	
	return true, nil
}


// DeleteBranch 删除分支
func (r *Repository) DeleteBranch(branch string, force bool) error {
	args := []string{"branch"}
	
	if force {
		args = append(args, "-D")
	} else {
		args = append(args, "-d")
	}
	
	args = append(args, branch)
	
	_, err := r.Runner.RunInDir(r.Path, args...)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	
	return nil
}




// CreateBranch 创建新分支
func (r *Repository) CreateBranch(branch string, startPoint string) error {
	args := []string{"branch"}
	if startPoint != "" {
		args = append(args, branch, startPoint)
	} else {
		args = append(args, branch)
	}
	
	_, err := r.Runner.RunInDir(r.Path, args...)
	if err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}
	
	return nil
}


// HasChangesToPush 检查是否有需要推送的更改
func (r *Repository) HasChangesToPush(branch string) (bool, error) {
	// 获取远程分支名称
	remoteBranch := "origin/" + branch
	
	// 检查本地分支和远程分支之间的差异
	output, err := r.Runner.RunInDir(r.Path, "rev-list", "--count", branch, "^"+remoteBranch)
	if err != nil {
		return false, fmt.Errorf("failed to check changes to push: %w", err)
	}
	
	// 如果输出不为0，则有更改需要推送
	count := strings.TrimSpace(string(output))
	return count != "0", nil
}

// GetBranchName 获取当前分支名称
func (r *Repository) GetBranchName() (string, error) {
	// 使用 Runner 而不是 runner
	// 使用 Path 而不是 path
	output, err := r.Runner.RunInDir(r.Path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	
	return strings.TrimSpace(string(output)), nil
}