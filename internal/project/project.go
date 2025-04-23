package project

import (
	"time"
	"fmt"
	"os"

	"github.com/cix-code/gogo/internal/git"
)

// Project 表示一个本地项目
type Project struct {
	Name       string
	Path       string
	RemoteName string
	RemoteURL  string
	Revision   string
	Groups     []string
	GitRepo    *git.Repository
	
	// 添加与 engine.go 兼容的字段
	Relpath    string    // 项目相对路径
	Worktree   string    // 项目工作目录
	Gitdir     string    // Git 目录
	RevisionId string    // 修订ID
	Linkfiles  []LinkFile // 链接文件列表
	Copyfiles  []CopyFile // 复制文件列表
	Objdir     string    // 对象目录
	
	// 添加新的字段
	LastFetch time.Time // 最后一次获取的时间
	Remote     string    // 远程仓库名称
	NeedGC     bool      // 是否需要垃圾回收
}

// LinkFile 表示链接文件
type LinkFile struct {
	Src  string // 源文件路径
	Dest string // 目标文件路径
}

// CopyFile 表示复制文件
type CopyFile struct {
	Src  string // 源文件路径
	Dest string // 目标文件路径
}

// NewProject 创建项目
func NewProject(name, path, remoteName, remoteURL, revision string, groups []string, gitRunner git.Runner) *Project {
	return &Project{
		Name:       name,
		Path:       path,
		RemoteName: remoteName,
		RemoteURL:  remoteURL,
		Revision:   revision,
		Groups:     groups,
		GitRepo:    git.NewRepository(path, gitRunner),
		Relpath:    path,      // 设置相对路径
		Worktree:   path,      // 设置工作目录
		Gitdir:     path + "/.git", // 设置Git目录
		RevisionId: revision,  // 设置修订ID
		Remote:     remoteName, // 设置远程仓库名称
		NeedGC:     false,     // 默认不需要垃圾回收
	}
}

// IsInGroup 检查项目是否在指定组中
func (p *Project) IsInGroup(group string) bool {
	if group == "" {
		return true
	}
	
	for _, g := range p.Groups {
		if g == group {
			return true
		}
	}
	
	return false
}

// IsInAnyGroup 检查项目是否在任意指定组中
func (p *Project) IsInAnyGroup(groups []string) bool {
	if len(groups) == 0 {
		return true
	}
	
	for _, group := range groups {
		if p.IsInGroup(group) {
			return true
		}
	}
	
	return false
}

// 删除 SyncOptions 结构体定义，使用 manager.go 中的定义

// Sync 同步项目
func (p *Project) Sync(opts SyncOptions) error {
	// 检查项目目录是否存在
	exists, err := p.GitRepo.Exists()
	if err != nil {
		return fmt.Errorf("failed to check if project exists: %w", err)
	}
	
	// 如果不存在，克隆仓库
	if !exists {
		if err := p.GitRepo.Clone(p.RemoteURL, git.CloneOptions{
			Depth:  opts.Depth,
			Branch: p.Revision,
		}); err != nil {
			return fmt.Errorf("failed to clone project: %w", err)
		}
		return nil
	}
	
	// 如果存在，获取更新
	if !opts.LocalOnly {
		if err := p.GitRepo.Fetch(p.RemoteName, git.FetchOptions{
			Prune: opts.Prune,
			Tags:  opts.Tags,
			Depth: opts.Depth,
		}); err != nil {
			return fmt.Errorf("failed to fetch updates: %w", err)
		}
	}
	
	// 如果不是只获取，更新工作区
	if !opts.NetworkOnly {
		// 检查是否有本地修改
		clean, err := p.GitRepo.IsClean()
		if err != nil {
			return fmt.Errorf("failed to check if working tree is clean: %w", err)
		}
		
		// 如果有本地修改且不强制同步，报错
		if !clean && !opts.Force {
			return fmt.Errorf("working tree is not clean, use --force-sync to overwrite local changes")
		}
		
		// 检出指定版本
		if err := p.GitRepo.Checkout(p.Revision); err != nil {
			return fmt.Errorf("failed to checkout revision: %w", err)
		}
	}
	
	return nil
}

// GetStatus 获取项目状态
func (p *Project) GetStatus() (string, error) {
	status, err := p.GitRepo.Status()
	if err != nil {
		return "", fmt.Errorf("failed to get project status: %w", err)
	}
	return string(status), nil
}

// DeleteWorktree 删除工作树
func (p *Project) DeleteWorktree(quiet bool, forceRemoveDirty bool) error {
	// 检查工作树是否存在
	if _, err := os.Stat(p.Worktree); os.IsNotExist(err) {
		return nil
	}
	
	// 检查是否有本地修改
	if !forceRemoveDirty {
		clean, err := p.GitRepo.IsClean()
		if err != nil {
			return fmt.Errorf("failed to check if working tree is clean: %w", err)
		}
		
		if !clean {
			return fmt.Errorf("working tree is not clean, use --force-remove-dirty to remove anyway")
		}
	}
	
	// 删除工作树
	if !quiet {
		fmt.Printf("删除工作树 %s\n", p.Worktree)
	}
	
	return os.RemoveAll(p.Worktree)
}

// SyncNetworkHalf 执行网络同步
func (p *Project) SyncNetworkHalf(quiet bool, currentBranch bool, forceSync bool, noCloneBundle bool, 
	tags bool, isArchive bool, optimizedFetch bool, retryFetches int, prune bool, 
	sshProxy interface{}, cloneFilter string, partialCloneExclude string) bool {
	
	// 这里实现网络同步逻辑
	// 由于原始代码中没有详细实现，这里提供一个简单的实现
	
	if !quiet {
		fmt.Printf("正在同步 %s\n", p.Name)
	}
	
	// 检查项目目录是否存在
	exists, err := p.GitRepo.Exists()
	if err != nil {
		return false
	}
	
	// 如果不存在，克隆仓库
	if !exists {
		if err := p.GitRepo.Clone(p.RemoteURL, git.CloneOptions{
			Branch: p.Revision,
		}); err != nil {
			return false
		}
		return true
	}
	
	// 如果存在，获取更新
	if err := p.GitRepo.Fetch(p.RemoteName, git.FetchOptions{
		Prune: prune,
		Tags:  tags,
	}); err != nil {
		return false
	}
	
	return true
}

// SyncLocalHalf 执行本地同步
func (p *Project) SyncLocalHalf(detach bool, forceSync bool, forceOverwrite bool) bool {
	// 这里实现本地同步逻辑
	// 由于原始代码中没有详细实现，这里提供一个简单的实现
	
	// 检查是否有本地修改
	clean, err := p.GitRepo.IsClean()
	if err != nil {
		return false
	}
	
	// 如果有本地修改且不强制同步，报错
	if !clean && !forceSync {
		fmt.Printf("工作树不干净，使用 --force-sync 覆盖本地修改\n")
		return false
	}
	
	// 检出指定版本
	if err := p.GitRepo.Checkout(p.Revision); err != nil {
		return false
	}
	
	return true
}

// GetBranch 获取当前分支
func (p *Project) GetCurrentBranch() (string, error) {
    return p.GitRepo.CurrentBranch()
}

func (p *Project) DeleteBranch(branch string) error {
    _, err := p.GitRepo.RunCommand("branch", "-D", branch)
    return err
}

// GC 执行垃圾回收
func (p *Project) GC() error {
	// 执行 git gc 命令
	_, err := p.GitRepo.RunCommand("gc", "--auto")
	if err != nil {
		return fmt.Errorf("执行垃圾回收失败: %w", err)
	}
	return nil
}