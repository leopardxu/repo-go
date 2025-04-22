package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// RunCommand 运行任意Git命令
func (r *Repository) RunCommand(args ...string) (string, error) {
	output, err := r.Runner.RunInDir(r.Path, args...)
	if err != nil {
		return "", fmt.Errorf("git command failed: %w", err)
	}
	
	return string(output), nil
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