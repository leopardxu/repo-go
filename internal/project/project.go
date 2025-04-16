package project

import (
	"fmt"

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

// 删除这里的SyncOptions定义，使用manager.go中的定义