package git

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/logger"
)

// 使用包级别的日志记录器
var repoLog logger.Logger = &logger.DefaultLogger{}

// SetRepositoryLogger 设置仓库操作的日志记录器
func SetRepositoryLogger(logger logger.Logger) {
	repoLog = logger
}

// 缓存相关变量
var (
	urlCache     = make(map[string]string)
	urlCacheMutex sync.RWMutex
)

// Repository 表示一个Git仓库
type Repository struct {
	Path   string
	Runner Runner
	
	// 缓存
	statusCache      string
	statusCacheTime  time.Time
	branchCache      string
	branchCacheTime  time.Time
	cacheMutex       sync.RWMutex
	cacheExpiration  time.Duration
}

// RepositoryError 表示仓库操作错误
type RepositoryError struct {
	Op      string // 操作名称
	Path    string // 仓库路径
	Command string // Git命令
	Err     error  // 原始错误
}

func (e *RepositoryError) Error() string {
	if e.Path != "" && e.Command != "" {
		return fmt.Sprintf("git repository error: %s failed in '%s': %s: %v", e.Op, e.Path, e.Command, e.Err)
	}
	if e.Path != "" {
		return fmt.Sprintf("git repository error: %s failed in '%s': %v", e.Op, e.Path, e.Err)
	}
	if e.Command != "" {
		return fmt.Sprintf("git repository error: %s failed: %s: %v", e.Op, e.Command, e.Err)
	}
	return fmt.Sprintf("git repository error: %s failed: %v", e.Op, e.Err)
}

func (e *RepositoryError) Unwrap() error {
	return e.Err
}

// NewRepository 创建一个新的Git仓库实例
func NewRepository(path string, runner Runner) *Repository {
	return &Repository{
		Path:           path,
		Runner:         runner,
		cacheExpiration: time.Minute * 5, // 默认缓存过期时间为5分钟
	}
}

// SetCacheExpiration 设置缓存过期时间
func (r *Repository) SetCacheExpiration(duration time.Duration) {
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()
	r.cacheExpiration = duration
}

// ClearCache 清除缓存
func (r *Repository) ClearCache() {
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()
	r.statusCache = ""
	r.branchCache = ""
}

// RunCommand 执行Git命令并返回结果
func (r *Repository) RunCommand(args ...string) ([]byte, error) {
	repoLog.Debug("在仓库 '%s' 执行命令: git %s", r.Path, strings.Join(args, " "))
	
	output, err := r.Runner.RunInDir(r.Path, args...)
	if err != nil {
		repoLog.Error("命令执行失败: git %s: %v", strings.Join(args, " "), err)
		return nil, &RepositoryError{
			Op:      "run_command",
			Path:    r.Path,
			Command: fmt.Sprintf("git %s", strings.Join(args, " ")),
			Err:     err,
		}
	}
	
	repoLog.Debug("命令执行成功: git %s", strings.Join(args, " "))
	return output, nil
}

// CloneOptions contains options for git clone
// CloneOptions 包含克隆选项
type CloneOptions struct {
	Depth  int
	Branch string
	Config *config.Config // 添加Config字段
}

// FetchOptions contains options for git fetch
type FetchOptions struct {
    Prune  bool
    Tags   bool
    Depth  int
    Config *config.Config // 添加Config字段
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

// Clone 克隆一个Git仓库
func (r *Repository) Clone(repoURL string, opts CloneOptions) error {
	repoLog.Info("克隆仓库: %s 到 %s", repoURL, r.Path)
	
	// 构建克隆参数
	args := []string{"clone"}
	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
		repoLog.Debug("使用深度: %d", opts.Depth)
	}
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
		repoLog.Debug("使用分支: %s", opts.Branch)
	}
	
	// 处理URL
	resolvedURL, err := resolveRepositoryURL(repoURL, opts.Config)
	if err != nil {
		repoLog.Error("解析仓库URL失败: %v", err)
		return &RepositoryError{
			Op:   "clone",
			Path: r.Path,
			Err:  fmt.Errorf("failed to resolve repository URL: %w", err),
		}
	}
	
	repoLog.Debug("解析后的URL: %s", resolvedURL)
	
	// 执行克隆命令
	args = append(args, resolvedURL, r.Path)
	_, err = r.Runner.Run(args...)
	if err != nil {
		repoLog.Error("克隆失败: %v", err)
		return &RepositoryError{
			Op:      "clone",
			Path:    r.Path,
			Command: fmt.Sprintf("git clone %s", resolvedURL),
			Err:     err,
		}
	}
	
	repoLog.Info("仓库克隆成功: %s", r.Path)
	return nil
}

// Fetch 从远程获取更新
func (r *Repository) Fetch(remote string, opts FetchOptions) error {
    repoLog.Info("从远程 '%s' 获取更新到 '%s'", remote, r.Path)
    
    // 解析远程URL
    resolvedRemote := remote
    if strings.HasPrefix(remote, "../") || !strings.Contains(remote, "://") {
        var err error
        resolvedRemote, err = resolveRepositoryURL(remote, opts.Config)
        if err != nil {
            repoLog.Error("解析远程URL失败: %v", err)
            return &RepositoryError{
                Op:   "fetch",
                Path: r.Path,
                Err:  fmt.Errorf("failed to resolve remote URL: %w", err),
            }
        }
        repoLog.Debug("解析后的远程URL: %s", resolvedRemote)
    }
    
    // 构建fetch参数
    args := []string{"fetch", resolvedRemote}
    if opts.Prune {
        args = append(args, "--prune")
        repoLog.Debug("使用修剪选项")
    }
    if opts.Tags {
        args = append(args, "--tags")
        repoLog.Debug("获取所有标签")
    }
    if opts.Depth > 0 {
        args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
        repoLog.Debug("使用深度: %d", opts.Depth)
    }
    
    // 执行fetch命令
    _, err := r.Runner.RunInDir(r.Path, args...)
    if err != nil {
        repoLog.Error("获取更新失败: %v", err)
        return &RepositoryError{
            Op:      "fetch",
            Path:    r.Path,
            Command: fmt.Sprintf("git fetch %s", resolvedRemote),
            Err:     err,
        }
    }
    
    // 清除缓存，因为fetch可能改变仓库状态
    r.ClearCache()
    
    repoLog.Info("成功从远程 '%s' 获取更新", resolvedRemote)
    return nil
}

// Checkout checks out a specific revision
func (r *Repository) Checkout(revision string) error {
	_, err := r.Runner.RunInDir(r.Path, "checkout", revision)
	return err
}

// Status 获取仓库状态
func (r *Repository) Status() (string, error) {
	// 检查缓存
	r.cacheMutex.RLock()
	if r.statusCache != "" && time.Since(r.statusCacheTime) < r.cacheExpiration {
		status := r.statusCache
		r.cacheMutex.RUnlock()
		repoLog.Debug("使用缓存的仓库状态")
		return status, nil
	}
	r.cacheMutex.RUnlock()
	
	// 获取状态
	repoLog.Debug("获取仓库 '%s' 的状态", r.Path)
	output, err := r.Runner.RunInDir(r.Path, "status", "--porcelain")
	if err != nil {
		repoLog.Error("获取仓库状态失败: %v", err)
		return "", &RepositoryError{
			Op:      "status",
			Path:    r.Path,
			Command: "git status --porcelain",
			Err:     err,
		}
	}
	
	// 更新缓存
	status := string(output)
	r.cacheMutex.Lock()
	r.statusCache = status
	r.statusCacheTime = time.Now()
	r.cacheMutex.Unlock()
	
	return status, nil
}

// IsClean 检查仓库是否干净（没有未提交的更改）
func (r *Repository) IsClean() (bool, error) {
	repoLog.Debug("检查仓库 '%s' 是否干净", r.Path)
	status, err := r.Status()
	if err != nil {
		return false, err
	}
	
	isClean := status == ""
	repoLog.Debug("仓库 '%s' %s", r.Path, map[bool]string{true: "干净", false: "有未提交的更改"}[isClean])
	return isClean, nil
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
	// 检查缓存
	r.cacheMutex.RLock()
	if r.branchCache != "" && time.Since(r.branchCacheTime) < r.cacheExpiration {
		branch := r.branchCache
		r.cacheMutex.RUnlock()
		repoLog.Debug("使用缓存的分支名称")
		return branch, nil
	}
	r.cacheMutex.RUnlock()
	
	// 获取当前分支
	repoLog.Debug("获取仓库 '%s' 的当前分支", r.Path)
	output, err := r.Runner.RunInDir(r.Path, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		// 可能处于分离头指针状态
		output, err = r.Runner.RunInDir(r.Path, "rev-parse", "--short", "HEAD")
		if err != nil {
			repoLog.Error("获取当前分支失败: %v", err)
			return "", &RepositoryError{
				Op:      "current_branch",
				Path:    r.Path,
				Command: "git symbolic-ref --short HEAD",
				Err:     err,
			}
		}
		// 处于分离头指针状态
		branch := "HEAD detached at " + strings.TrimSpace(string(output))
		
		// 更新缓存
		r.cacheMutex.Lock()
		r.branchCache = branch
		r.branchCacheTime = time.Now()
		r.cacheMutex.Unlock()
		
		repoLog.Debug("仓库 '%s' 处于分离头指针状态: %s", r.Path, branch)
		return branch, nil
	}
	
	// 更新缓存
	branch := strings.TrimSpace(string(output))
	r.cacheMutex.Lock()
	r.branchCache = branch
	r.branchCacheTime = time.Now()
	r.cacheMutex.Unlock()
	
	repoLog.Debug("仓库 '%s' 当前分支: %s", r.Path, branch)
	return branch, nil
}

// HasRevision 检查是否有指定的修订版本
func (r *Repository) HasRevision(revision string) (bool, error) {
	repoLog.Debug("检查仓库 '%s' 是否有修订版本: %s", r.Path, revision)
	_, err := r.Runner.RunInDir(r.Path, "rev-parse", "--verify", revision)
	if err != nil {
		repoLog.Debug("仓库 '%s' 没有修订版本: %s", r.Path, revision)
		return false, nil
	}
	
	repoLog.Debug("仓库 '%s' 有修订版本: %s", r.Path, revision)
	return true, nil
}

// resolveRepositoryURL 解析仓库URL，处理相对路径和特殊格式
func resolveRepositoryURL(repoURL string, cfg *config.Config) (string, error) {
	// 检查缓存
	urlCacheMutex.RLock()
	if cachedURL, ok := urlCache[repoURL]; ok {
		urlCacheMutex.RUnlock()
		return cachedURL, nil
	}
	urlCacheMutex.RUnlock()
	
	// 处理相对路径
	if strings.Contains(repoURL, "..") || strings.HasPrefix(repoURL, "../") {
		// 尝试从配置中获取基础URL
		baseURL := ""
		if cfg != nil {
			baseURL = cfg.ExtractBaseURLFromManifestURL(cfg.ManifestURL)
		}
		
		if baseURL == "" {
			// 如果没有配置或无法获取基础URL，使用默认值
			baseURL = "ssh://git@gitmirror.cixtech.com"
		}
		
		// 确保baseURL不以/结尾
		baseURL = strings.TrimSuffix(baseURL, "/")
		
		// 处理不同格式的相对路径
		var resolvedURL string
		if strings.HasPrefix(repoURL, "../") {
			// 移除相对路径前缀
			relPath := strings.TrimPrefix(repoURL, "../")
			resolvedURL = baseURL + "/" + relPath
		} else {
			// 替换..为baseURL
			resolvedURL = strings.Replace(repoURL, "..", baseURL, -1)
		}
		
		// 更新缓存
		urlCacheMutex.Lock()
		urlCache[repoURL] = resolvedURL
		urlCacheMutex.Unlock()
		
		return resolvedURL, nil
	}
	
	// 处理URL格式
	if !strings.Contains(repoURL, "://") && !strings.Contains(repoURL, "@") {
		// 可能是简单的路径，尝试解析为有效URL
		if strings.HasPrefix(repoURL, "/") {
			// 绝对路径，使用file协议
			resolvedURL := "file://" + repoURL
			
			// 更新缓存
			urlCacheMutex.Lock()
			urlCache[repoURL] = resolvedURL
			urlCacheMutex.Unlock()
			
			return resolvedURL, nil
		}
		
		// 尝试解析为HTTP/HTTPS URL
		if _, err := url.Parse("https://" + repoURL); err == nil {
			// 看起来是有效的主机名，使用HTTPS
			resolvedURL := "https://" + repoURL
			
			// 更新缓存
			urlCacheMutex.Lock()
			urlCache[repoURL] = resolvedURL
			urlCacheMutex.Unlock()
			
			return resolvedURL, nil
		}
	}
	
	// URL已经是完整格式或无法解析，直接返回
	return repoURL, nil
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