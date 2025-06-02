package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/leopardxu/repo-go/internal/git"
	"github.com/leopardxu/repo-go/internal/logger"
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

	// 添加与engine.go 兼容的字段
	Relpath    string     // 项目相对路径
	Worktree   string     // 项目工作目录
	Gitdir     string     // Git 目录
	RevisionId string     // 修订ID
	Linkfiles  []LinkFile // 链接文件列表
	Copyfiles  []CopyFile // 复制文件列表
	Objdir     string     // 对象目录

	// 添加新的字段
	LastFetch  time.Time // 最后一次获取的时间
	Remote     string    // 远程仓库名称
	References string    // 引用配置(remote:refs格式)
	NeedGC     bool      // 是否需要垃圾回�?

	// 添加锁，保护并发访问
	mu sync.RWMutex
}

// LinkFile 表示链接文件
type LinkFile struct {
	Src  string // 源文件路�?
	Dest string // 目标文件路径
}

// CopyFile 表示复制文件
type CopyFile struct {
	Src  string // 源文件路�?
	Dest string // 目标文件路径
}

// NewProject 创建项目
func NewProject(name, path, remoteName, remoteURL, revision string, groups []string, gitRunner git.Runner) *Project {
	// 确保路径使用正确的分隔符
	path = filepath.Clean(path)

	return &Project{
		Name:       name,
		Path:       path,
		RemoteName: remoteName,
		RemoteURL:  remoteURL,
		Revision:   revision,
		Groups:     groups,
		GitRepo:    git.NewRepository(path, gitRunner),
		Relpath:    path,                        // 设置相对路径
		Worktree:   path,                        // 设置工作目录
		Gitdir:     filepath.Join(path, ".git"), // 设置Git目录
		RevisionId: revision,                    // 设置修订ID
		Remote:     remoteName,                  // 设置远程仓库名称
		NeedGC:     false,                       // 默认不需要垃圾回�?
	}
}

// IsInGroup 检查项目是否在指定组中
func (p *Project) IsInGroup(group string) bool {
	if group == "" {
		return true
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, g := range p.Groups {
		if g == group {
			return true
		}
	}

	return false
}

// IsInAnyGroup 检查项目是否在任意指定组中
// 注意：当指定多个组时，项目必须至少属于其中一个组才会被包�?
func (p *Project) IsInAnyGroup(groups []string) bool {
	if len(groups) == 0 {
		return true
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	// 检查项目是否属于任意一个指定的�?
	for _, group := range groups {
		if group == "" {
			continue // 跳过空组�?
		}

		for _, projectGroup := range p.Groups {
			if projectGroup == group {
				return true
			}
		}
	}

	return false
}

// Sync 同步项目
func (p *Project) Sync(opts SyncOptions) error {
	// 使用结构化日志，减少冗余信息
	logger.Debug("同步项目 [%s]", p.Name)

	// 检查项目目录是否存在
	exists, err := p.GitRepo.Exists()
	if err != nil {
		logger.Error("项目 [%s] 检查失�? %v", p.Name, err)
		return fmt.Errorf("检查项目是否存在失�? %w", err)
	}

	// 如果不存在，克隆仓库
	if !exists {
		// 确保使用完整的远程URL进行克隆
		cloneURL := p.RemoteURL
		if cloneURL == "" {
			logger.Error("项目 [%s] 远程URL为空", p.Name)
			return fmt.Errorf("无法克隆项目 %s: 远程URL为空", p.Name)
		}

		// 只在非静默模式下输出信息日志
		if !opts.Quiet {
			logger.Info("克隆 [%s] <- %s", p.Name, cloneURL)
		}

		// 创建父目�?
		parentDir := filepath.Dir(p.Path)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			logger.Error("项目 [%s] 创建目录失败: %v", p.Name, err)
			return fmt.Errorf("创建目录失败: %w", err)
		}

		// 克隆仓库
		if err := p.GitRepo.Clone(cloneURL, git.CloneOptions{
			Depth:  opts.Depth,
			Branch: p.Revision,
		}); err != nil {
			logger.Error("项目 [%s] 克隆失败: %v", p.Name, err)
			return fmt.Errorf("克隆项目失败: %w", err)
		}

		if !opts.Quiet {
			logger.Info("项目 [%s] 克隆完成", p.Name)
		}
		return nil
	}

	// 如果存在，获取更新
	if !opts.LocalOnly {
		if !opts.Quiet {
			logger.Debug("获取项目 [%s] 更新", p.Name)
		}

		// 更新最后获取时间
		p.mu.Lock()
		p.LastFetch = time.Now()
		p.mu.Unlock()

		if err := p.GitRepo.Fetch(p.RemoteName, git.FetchOptions{
			Prune: opts.Prune,
			Tags:  opts.Tags,
			Depth: opts.Depth,
		}); err != nil {
			logger.Error("项目 [%s] 更新失败: %v", p.Name, err)
			return fmt.Errorf("获取更新失败: %w", err)
		}

		// 设置需要垃圾回收标�?
		p.mu.Lock()
		p.NeedGC = true
		p.mu.Unlock()
	}

	// 如果不是只获取，更新工作区
	if !opts.NetworkOnly {
		// 减少日志输出
		if !opts.Quiet {
			logger.Debug("更新项目 [%s] 工作区", p.Name)
		}

		// 检查是否有本地修改
		clean, err := p.GitRepo.IsClean()
		if err != nil {
			logger.Error("项目 [%s] 工作区检查失�? %v", p.Name, err)
			return fmt.Errorf("检查工作区是否干净失败: %w", err)
		}

		// 如果有本地修改且不强制同步，报错
		if !clean && !opts.Force {
			logger.Warn("项目 [%s] 工作区不干净，需要使�?--force-sync 覆盖", p.Name)
			return fmt.Errorf("工作区不干净，使�?--force-sync 覆盖本地修改")
		}

		// 检出指定版�?
		if err := p.GitRepo.Checkout(p.Revision); err != nil {
			logger.Error("项目 [%s] 检�?%s 失败: %v", p.Name, p.Revision, err)
			return fmt.Errorf("检出修订版本失�? %w", err)
		}

		if !opts.Quiet {
			logger.Info("项目 [%s] 更新完成", p.Name)
		}
	}

	return nil
}

// GC 执行垃圾回收
func (p *Project) GC() error {
	// 检查是否需要垃圾回�?
	p.mu.RLock()
	needGC := p.NeedGC
	p.mu.RUnlock()

	if !needGC {
		return nil
	}

	logger.Debug("项目 [%s] 执行垃圾回收", p.Name)

	// 执行 git gc 命令
	_, err := p.GitRepo.RunCommand("gc", "--auto")
	if err != nil {
		logger.Error("项目 [%s] 垃圾回收失败: %v", p.Name, err)
		return fmt.Errorf("执行垃圾回收失败: %w", err)
	}

	// 重置垃圾回收标志
	p.mu.Lock()
	p.NeedGC = false
	p.mu.Unlock()

	return nil
}

// SyncNetworkHalf 执行网络同步
func (p *Project) SyncNetworkHalf(quiet bool, currentBranch bool, forceSync bool, noCloneBundle bool,
	tags bool, isArchive bool, optimizedFetch bool, retryFetches int, prune bool,
	sshProxy interface{}, cloneFilter string, partialCloneExclude string) bool {

	logger.Debug("开始执行项目%s 的网络同步", p.Name)

	// 检查项目目录是否存在
	exists, err := p.GitRepo.Exists()
	if err != nil {
		logger.Error("检查项目%s 是否存在失败: %v", p.Name, err)
		return false
	}

	// 如果不存在，克隆仓库
	if !exists {
		if !quiet {
			logger.Info("克隆项目 %s �?%s", p.Name, p.RemoteURL)
		}

		// 创建父目�?
		parentDir := filepath.Dir(p.Path)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			logger.Error("为项�?%s 创建目录 %s 失败: %v", p.Name, parentDir, err)
			return false
		}

		// 克隆选项
		options := git.CloneOptions{
			Branch: p.Revision,
		}

		// 如果指定了深度，设置深度
		if retryFetches > 0 {
			options.Depth = retryFetches
		}

		// 克隆仓库
		if err := p.GitRepo.Clone(p.RemoteURL, options); err != nil {
			logger.Error("克隆项目 %s 失败: %v", p.Name, err)
			return false
		}

		logger.Debug("项目 %s 克隆完成", p.Name)
		return true
	}

	// 如果存在，获取更新
	if !quiet {
		logger.Info("获取项目 %s 的更新", p.Name)
	}

	// 更新最后获取时间
	p.mu.Lock()
	p.LastFetch = time.Now()
	p.mu.Unlock()

	// 获取选项
	fetchOpts := git.FetchOptions{
		Prune: prune,
		Tags:  tags,
	}

	// 如果指定了深度，设置深度
	if retryFetches > 0 {
		fetchOpts.Depth = retryFetches
	}

	// 执行获取，支持重�?
	var fetchErr error
	for i := 0; i <= retryFetches; i++ {
		fetchErr = p.GitRepo.Fetch(p.RemoteName, fetchOpts)
		if fetchErr == nil {
			break
		}

		if i < retryFetches {
			logger.Warn("获取项目 %s 更新失败，将重试 (%d/%d): %v", p.Name, i+1, retryFetches, fetchErr)
			time.Sleep(time.Second * time.Duration(i+1)) // 指数退�?
		}
	}

	if fetchErr != nil {
		logger.Error("获取项目 %s 更新失败: %v", p.Name, fetchErr)
		return false
	}

	logger.Debug("项目 %s 网络同步完成", p.Name)
	return true
}

// SyncLocalHalf 执行本地同步
func (p *Project) SyncLocalHalf(detach bool, forceSync bool, forceOverwrite bool) bool {
	logger.Debug("开始执行项目%s 的本地同步", p.Name)

	// 检查是否有本地修改
	clean, err := p.GitRepo.IsClean()
	if err != nil {
		logger.Error("检查项目%s 工作区是否干净失败: %v", p.Name, err)
		return false
	}

	// 如果有本地修改且不强制同步，报错
	if !clean && !forceSync && !forceOverwrite {
		logger.Warn("项目 %s 工作区不干净，需要使用--force-sync 覆盖本地修改", p.Name)
		return false
	}

	// 获取当前分支
	currentBranch, err := p.GitRepo.CurrentBranch()
	if err != nil {
		logger.Warn("获取项目 %s 当前分支失败: %v", p.Name, err)
		// 继续执行，不影响检出操�?
	}

	// 如果当前分支与目标分支不同，或者强制检�?
	if currentBranch != p.Revision || forceSync || forceOverwrite {
		logger.Debug("检出项�?%s 的修订版�?%s", p.Name, p.Revision)

		// 检出指定版�?
		if err := p.GitRepo.Checkout(p.Revision); err != nil {
			logger.Error("检出项�?%s 的修订版�?%s 失败: %v", p.Name, p.Revision, err)
			return false
		}
	} else {
		logger.Debug("项目 %s 已经在正确的修订版本 %s 上", p.Name, p.Revision)
	}

	logger.Debug("项目 %s 本地同步完成", p.Name)
	return true
}

// GetStatus 获取项目状态
func (p *Project) GetStatus() (string, error) {
	logger.Debug("获取项目 %s 的状态", p.Name)

	status, err := p.GitRepo.Status()
	if err != nil {
		logger.Error("获取项目 %s 状态失败: %v", p.Name, err)
		return "", fmt.Errorf("获取项目状态失败: %w", err)
	}

	return string(status), nil
}

// DeleteWorktree 删除工作�?
func (p *Project) DeleteWorktree(quiet bool, forceRemoveDirty bool) error {
	logger.Debug("准备删除项目 %s 的工作树", p.Name)

	// 检查工作树是否存在
	if _, err := os.Stat(p.Worktree); os.IsNotExist(err) {
		logger.Debug("项目 %s 的工作树不存在，无需删除", p.Name)
		return nil
	}

	// 检查是否有本地修改
	if !forceRemoveDirty {
		clean, err := p.GitRepo.IsClean()
		if err != nil {
			logger.Error("检查项目%s 工作区是否干净失败: %v", p.Name, err)
			return fmt.Errorf("检查工作区是否干净失败: %w", err)
		}

		if !clean {
			logger.Warn("项目 %s 工作区不干净，需要使用--force-remove-dirty 强制删除", p.Name)
			return fmt.Errorf("工作区不干净，使用--force-remove-dirty 强制删除")
		}
	}

	// 删除工作�?
	if !quiet {
		logger.Info("删除项目 %s 的工作树 %s", p.Name, p.Worktree)
	}

	if err := os.RemoveAll(p.Worktree); err != nil {
		logger.Error("删除项目 %s 的工作树失败: %v", p.Name, err)
		return fmt.Errorf("删除工作树失败: %w", err)
	}

	logger.Debug("项目 %s 的工作树已删除", p.Name)
	return nil
}

// GetCurrentBranch 获取当前分支
func (p *Project) GetCurrentBranch() (string, error) {
	logger.Debug("获取项目 %s 的当前分支", p.Name)

	branch, err := p.GitRepo.CurrentBranch()
	if err != nil {
		logger.Error("获取项目 %s 当前分支失败: %v", p.Name, err)
		return "", fmt.Errorf("获取当前分支失败: %w", err)
	}

	return branch, nil
}

// HasBranch 检查分支是否存在
func (p *Project) HasBranch(branch string) (bool, error) {
	logger.Debug("检查项目%s 是否有分支%s", p.Name, branch)

	output, err := p.GitRepo.RunCommand("branch", "--list", branch)
	if err != nil {
		logger.Error("列出项目 %s 的分支失败: %v", p.Name, err)
		return false, fmt.Errorf("列出分支失败: %w", err)
	}

	return strings.TrimSpace(string(output)) != "", nil
}

// DeleteBranch 删除指定的分支
func (p *Project) DeleteBranch(branch string) error {
	logger.Debug("准备删除项目 %s 的分支%s", p.Name, branch)

	if branch == "" {
		logger.Error("尝试删除项目 %s 的空分支名", p.Name)
		return fmt.Errorf("分支名为空")
	}

	// 检查分支是否存在
	exists, err := p.HasBranch(branch)
	if err != nil {
		return err
	}
	if !exists {
		logger.Warn("项目 %s 中不存在分支 %s", p.Name, branch)
		return fmt.Errorf("分支 '%s' 不存在", branch)
	}

	// 删除分支
	output, err := p.GitRepo.RunCommand("branch", "-D", branch)
	if err != nil {
		logger.Error("删除项目 %s 的分支%s 失败: %v\n%s", p.Name, branch, err, output)
		return fmt.Errorf("删除分支失败: %w\n%s", err, output)
	}

	logger.Debug("已删除项目%s 的分支%s", p.Name, branch)
	return nil
}

// SetNeedGC 设置是否需要垃圾回收
func (p *Project) SetNeedGC(need bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.NeedGC = need
}

// GetRemoteURL 获取远程URL
func (p *Project) GetRemoteURL() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.RemoteURL
}

// SetRemoteURL 设置远程URL
func (p *Project) SetRemoteURL(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.RemoteURL = url
}

// GetRevision 获取修订版本
func (p *Project) GetRevision() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Revision
}

// SetRevision 设置修订版本
func (p *Project) SetRevision(revision string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Revision = revision
	p.RevisionId = revision
}
