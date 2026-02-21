package repo_sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/git"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"golang.org/x/sync/errgroup"
)

// SyncAll 执行仓库同步
func (e *Engine) SyncAll() error {
	// 加载清单但不打印日志
	if err := e.loadManifestSilently(); err != nil {
		return err
	}

	// 根据verbose选项控制警告日志输出
	e.SetSilentMode(!e.options.Verbose)

	// 初始化错误结果列表
	e.errResults = []string{}

	// 使用goroutine池控制并发（errgroup 内建等待机制，无需额外 WaitGroup）
	g, ctx := errgroup.WithContext(e.ctx)
	g.SetLimit(e.options.Jobs)

	for _, p := range e.projects {
		p := p
		g.Go(func() error {
			// 如果设置了FailFast选项，检查 context 是否已经取消
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			err := e.syncProject(p)

			// 即使有错误也继续同步其他项目，不中断整个过程
			return err
		})
	}

	err := g.Wait()

	// 显示错误摘要
	if len(e.errResults) > 0 {
		e.displayErrorSummary()
		return fmt.Errorf("同步失败: 同步过程中发生了 %d 个错误", len(e.errResults))
	}

	return err
}

// displayErrorSummary 显示错误摘要（从 SyncAll 提取，消除 fmt.Printf 混用）
func (e *Engine) displayErrorSummary() {
	// 对错误进行分类统计
	errorTypes := make(map[string]int)
	for _, errMsg := range e.errResults {
		if strings.Contains(errMsg, "exit status 128") {
			errorTypes["exit status 128"] += 1
		} else if strings.Contains(errMsg, "network error") || strings.Contains(errMsg, "timed out") {
			errorTypes["网络错误"] += 1
		} else if strings.Contains(errMsg, "authentication failed") || strings.Contains(errMsg, "permission denied") {
			errorTypes["认证错误"] += 1
		} else if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "does not exist") {
			errorTypes["资源不存在"] += 1
		} else {
			errorTypes["其他错误"] += 1
		}
	}

	// 使用 logger 替代 fmt.Printf
	e.logger.Error("同步过程中发生了 %d 个错误", len(e.errResults))

	// 打印错误类型统计
	e.logger.Info("错误类型统计:")
	for errType, count := range errorTypes {
		e.logger.Info("  %s: %d 个", errType, count)
	}

	// 打印详细错误信息
	e.logger.Info("详细错误信息:")
	for i, errMsg := range e.errResults {
		e.logger.Error("错误 %d: %s", i+1, errMsg)

		// 对于exit status 128错误，提供额外的诊断信息
		if strings.Contains(errMsg, "exit status 128") {
			suggestion := analyzeGitError(errMsg)
			e.logger.Info("  诊断: %s", suggestion)
			e.logger.Info("  建议: 检查网络连接、验证URL、确认访问权限、尝试 --retry-fetches 或 --verbose")
		}
	}
}

// loadManifestSilently 静默加载清单
// 只使用合并后的清单文件(.repo/manifest.xml)作为输入，不使用原始仓库列表
func (e *Engine) loadManifestSilently() error {
	parser := manifest.NewParser()
	// 设置解析器为静默模式
	parser.SetSilentMode(true)

	// 确保Groups是字符串切片
	var groups []string
	if e.options.Groups != nil {
		groups = e.options.Groups
	} else if e.config != nil && e.config.Groups != "" {
		groups = strings.Split(e.config.Groups, ",")
		// 去除空白
		validGroups := make([]string, 0, len(groups))
		for _, g := range groups {
			g = strings.TrimSpace(g)
			if g != "" {
				validGroups = append(validGroups, g)
			}
		}
		groups = validGroups
	}

	// 直接使用.repo/manifest.xml文件（合并后的清单）
	manifestPath := filepath.Join(e.repoRoot, ".repo", "manifest.xml")

	// 解析合并后的清单文件，根据组过滤项目
	m, err := parser.ParseFromFile(manifestPath, groups)
	if err != nil {
		return fmt.Errorf("加载清单文件失败: %w", err)
	}

	// 检查清单是否有效
	if m == nil || len(m.Projects) == 0 {
		return fmt.Errorf("清单文件无效或不包含任何项目")
	}

	e.manifest = m
	return nil
}

// syncProjectImpl 同步单个项目的实现
func (e *Engine) syncProjectImpl(p *project.Project) error {
	// 检查并设置remote信息
	if p.References != "" {
		if err := e.setupProjectReferences(p); err != nil {
			return err
		}
	}
	if p.Remote == "" {
		p.Remote = e.manifest.Default.Remote
	}

	// 阶段一：确保项目目录存在（克隆或跳过）
	if err := e.ensureProjectCloned(p); err != nil {
		return err
	}

	// 阶段二：网络同步（fetch）
	if !e.options.LocalOnly {
		if err := e.fetchProjectWithRetry(p); err != nil {
			return err
		}
	}

	// 阶段三：本地检出（checkout）
	if !e.options.NetworkOnly {
		if err := e.checkoutProjectWithRetry(p); err != nil {
			return err
		}
	}

	return nil
}

// setupProjectReferences 解析和设置项目的 references 配置
func (e *Engine) setupProjectReferences(p *project.Project) error {
	refParts := strings.Split(p.References, ":")
	if len(refParts) != 2 {
		return fmt.Errorf("项目 %s 的references格式无效，应为 'remote:refs' 格式", p.Name)
	}
	p.Remote = refParts[0]
	p.RemoteName = refParts[0]
	p.RemoteURL, _ = e.manifest.GetRemoteURL(p.Remote)
	p.Revision = refParts[1]
	return nil
}

// ensureProjectCloned 确保项目已被克隆，如果不存在则克隆
func (e *Engine) ensureProjectCloned(p *project.Project) error {
	worktreeExists := false
	if _, err := os.Stat(p.Worktree); err == nil {
		worktreeExists = true
		// 检查是否已经是一个有效的git仓库
		gitDirPath := filepath.Join(p.Worktree, ".git")
		if _, err := os.Stat(gitDirPath); err == nil {
			if e.options.Verbose {
				e.logger.Debug("项目 %s 目录已存在且是一个git仓库，跳过克隆步骤", p.Name)
			}
			return nil // 已是 git 仓库，无需克隆
		}
	}

	if worktreeExists {
		// 目录存在但不是 git 仓库的情况已由 cloneProject 处理
		return nil
	}

	// 目录不存在，需要克隆
	if err := os.MkdirAll(filepath.Dir(p.Worktree), 0755); err != nil {
		return fmt.Errorf("创建项目目录失败 %s: %w", p.Name, err)
	}

	// 验证远程 URL
	if err := e.validateRemoteURL(p); err != nil {
		return err
	}

	e.logger.Info("正在克隆缺失项目: %s", p.Name)
	if e.options.Verbose {
		e.logger.Debug("使用URL: %s", p.RemoteURL)
	}

	// 使用 RetryWithBackoff 替代手动重试循环
	retryOpts := DefaultRetryOptions()
	retryOpts.MaxRetries = e.options.RetryFetches
	if retryOpts.MaxRetries <= 0 {
		retryOpts.MaxRetries = 3
	}

	cloneErr := RetryWithBackoff(e.ctx, retryOpts, func(attempt int) error {
		if attempt > 0 {
			e.logger.Info("克隆项目 %s 第 %d 次重试...", p.Name, attempt)
			// 重试前检查目录并清理
			e.cleanupFailedClone(p)
		}
		return e.cloneProject(p)
	})

	if cloneErr != nil {
		// 最终检查是否实际已是 git 仓库
		gitDirPath := filepath.Join(p.Worktree, ".git")
		if _, err := os.Stat(gitDirPath); err == nil {
			e.logger.Info("项目 %s 已是有效的 git 仓库", p.Name)
			return nil
		}

		errorMsg := fmt.Sprintf("克隆项目 %s 失败: %v", p.Name, cloneErr)
		e.recordError(errorMsg)
		return fmt.Errorf("克隆项目 %s 失败: %w", p.Name, cloneErr)
	}

	e.logger.Info("成功克隆项目: %s", p.Name)
	return nil
}

// validateRemoteURL 验证远程 URL 格式
func (e *Engine) validateRemoteURL(p *project.Project) error {
	if p.RemoteURL == "" {
		return fmt.Errorf("克隆项目 %s 失败: 远程URL未设置", p.Name)
	}
	if strings.ContainsAny(p.RemoteURL, " \t\n\r") {
		return fmt.Errorf("克隆项目 %s 失败: 远程URL包含空白字符", p.Name)
	}

	validProtocol := strings.HasPrefix(p.RemoteURL, "http") ||
		strings.HasPrefix(p.RemoteURL, "https") ||
		strings.HasPrefix(p.RemoteURL, "git@") ||
		strings.HasPrefix(p.RemoteURL, "ssh://") ||
		strings.HasPrefix(p.RemoteURL, "/") ||
		strings.HasPrefix(p.RemoteURL, "file://") ||
		strings.HasPrefix(p.RemoteURL, "./") ||
		strings.HasPrefix(p.RemoteURL, "../")

	if !validProtocol {
		return fmt.Errorf("克隆项目 %s 失败: 远程URL格式无效 %s (支持的协议: http, https, git@, ssh://, file://, /, ./, ../)", p.Name, p.RemoteURL)
	}
	return nil
}

// cleanupFailedClone 清理失败的克隆目录
func (e *Engine) cleanupFailedClone(p *project.Project) {
	if _, err := os.Stat(p.Worktree); err != nil {
		return // 目录不存在，无需清理
	}
	// 使用统一的安全路径检查
	if err := IsSafeToDelete(p.Worktree, e.repoRoot); err != nil {
		e.logger.Warn("跳过清理不安全路径 %s: %v", p.Worktree, err)
		return
	}
	e.logger.Info("删除不完整的克隆目录: %s", p.Worktree)
	os.RemoveAll(p.Worktree)
}

// fetchProjectWithRetry 使用统一重试逻辑执行 fetch
func (e *Engine) fetchProjectWithRetry(p *project.Project) error {
	if e.options.Verbose {
		e.logger.Debug("正在获取项目更新: %s", p.Name)
	}

	if p.RemoteURL == "" {
		errorMsg := fmt.Sprintf("获取项目 %s 更新失败: 远程URL未设置", p.Name)
		e.recordError(errorMsg)
		return fmt.Errorf("远程URL未设置")
	}

	if p.RemoteName == "" {
		p.RemoteName = "origin"
		if e.options.Verbose {
			e.logger.Debug("项目 %s 的远程名称未设置，使用默认名称 'origin'", p.Name)
		}
	}

	retryOpts := DefaultRetryOptions()
	retryOpts.MaxRetries = e.options.RetryFetches
	if retryOpts.MaxRetries <= 0 {
		retryOpts.MaxRetries = 3
	}

	fetchErr := RetryWithBackoff(e.ctx, retryOpts, func(attempt int) error {
		if attempt > 0 {
			e.logger.Info("获取项目 %s 更新第 %d 次重试...", p.Name, attempt)
		}
		return p.GitRepo.Fetch(p.RemoteName, git.FetchOptions{
			Prune: e.options.Prune,
			Tags:  e.options.Tags,
		})
	})

	if fetchErr != nil {
		errorMsg := fmt.Sprintf("获取项目 %s 更新失败: %v", p.Name, fetchErr)
		if e.options.Verbose {
			errorDetails := analyzeGitError(fetchErr.Error())
			errorMsg = fmt.Sprintf("获取项目 %s 更新失败: %v\n远程名称: %s\n远程URL: %s\n错误详情: %s",
				p.Name, fetchErr, p.RemoteName, p.RemoteURL, errorDetails)
		}
		e.recordError(errorMsg)
		return fmt.Errorf("获取项目 %s 更新失败: %w", p.Name, fetchErr)
	}

	return nil
}

// checkoutProjectWithRetry 使用统一重试逻辑执行 checkout
func (e *Engine) checkoutProjectWithRetry(p *project.Project) error {
	// 检查是否有本地修改
	clean, err := p.GitRepo.IsClean()
	if err != nil {
		return fmt.Errorf("检查项目 %s 工作区状态失败: %w", p.Name, err)
	}

	if !clean && !e.options.ForceSync {
		return fmt.Errorf("项目 %s 工作区不干净，使用 --force-sync 覆盖本地修改", p.Name)
	}

	if e.options.Verbose {
		e.logger.Debug("正在检出项目 %s 的版本 %s", p.Name, p.Revision)
	}

	if p.Revision == "" {
		p.Revision = "HEAD"
		if e.options.Verbose {
			e.logger.Debug("项目 %s 的修订版本未设置，使用默认 'HEAD'", p.Name)
		}
	}

	retryOpts := DefaultRetryOptions()
	retryOpts.MaxRetries = e.options.RetryFetches
	if retryOpts.MaxRetries <= 0 {
		retryOpts.MaxRetries = 3
	}
	retryOpts.ShouldRetry = func(err error) bool {
		// checkout 的本地修改冲突需要特殊处理
		errMsg := err.Error()
		if strings.Contains(errMsg, "local changes") || strings.Contains(errMsg, "would be overwritten") {
			if e.options.ForceSync {
				e.logger.Info("检出项目 %s 时发现本地修改，尝试强制重置...", p.Name)
				_, resetErr := p.GitRepo.Runner.RunInDir(p.Worktree, "reset", "--hard")
				return resetErr == nil // 重置成功才重试
			}
			return false
		}
		return IsRetryableGitError(err)
	}

	checkoutErr := RetryWithBackoff(e.ctx, retryOpts, func(attempt int) error {
		if attempt > 0 {
			e.logger.Info("检出项目 %s 第 %d 次重试...", p.Name, attempt)
		}
		return p.GitRepo.Checkout(p.Revision)
	})

	if checkoutErr != nil {
		errorMsg := fmt.Sprintf("检出项目 %s 失败: %v", p.Name, checkoutErr)
		if e.options.Verbose {
			errorDetails := analyzeGitError(checkoutErr.Error())
			errorMsg = fmt.Sprintf("检出项目 %s 的版本 %s 失败: %v\n错误详情: %s",
				p.Name, p.Revision, checkoutErr, errorDetails)
		}
		e.recordError(errorMsg)
		return fmt.Errorf("检出项目 %s 的版本 %s 失败: %w", p.Name, p.Revision, checkoutErr)
	}

	return nil
}

// recordError 线程安全地记录错误信息（使用正确的互斥锁）
func (e *Engine) recordError(errorMsg string) {
	e.errResultsMu.Lock()
	e.errResults = append(e.errResults, errorMsg)
	e.errResultsMu.Unlock()
}

func analyzeGitError(errMsg string) string {
	// 分析常见的Git错误
	if strings.Contains(errMsg, "exit status 128") {
		if strings.Contains(errMsg, "does not appear to be a git repository") {
			return "远程仓库路径不正确或不是有效的Git仓库"
		} else if strings.Contains(errMsg, "repository not found") || strings.Contains(errMsg, "not found") {
			return "远程仓库不存在，请检查URL是否正确"
		} else if strings.Contains(errMsg, "authentication failed") || strings.Contains(errMsg, "could not read Username") {
			return "认证失败，请检查您的凭据或确保有访问权限"
		} else if strings.Contains(errMsg, "unable to access") || strings.Contains(errMsg, "Could not resolve host") {
			return "网络连接问题，无法访问远程仓库"
		} else if strings.Contains(errMsg, "Permission denied") {
			return "权限被拒绝，请检查您的SSH密钥或访问权限"
		} else if strings.Contains(errMsg, "already exists") && strings.Contains(errMsg, "destination path") {
			return "目标路径已存在，但不是一个有效的Git仓库，请检查目录或使用--force-sync选项"
		} else {
			return "Git命令执行失败，可能是权限问题、网络问题或仓库配置错误"
		}
	} else if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "timed out") {
		return "操作超时，可能是网络连接缓慢或服务器响应时间长"
	} else if strings.Contains(errMsg, "connection refused") {
		return "连接被拒绝，远程服务器可能未运行或防火墙阻止了连接"
	} else if strings.Contains(errMsg, "already exists") && strings.Contains(errMsg, ".git") {
		return "Git目录已存在，可能需要使用--force-sync选项"
	} else if strings.Contains(errMsg, "conflict") {
		return "存在冲突，需要手动解决或使用--force-sync选项"
	}

	return "未知Git错误，请查看详细日志以获取更多信息"
}

// 以下保留变量声明避免 unused import（config, manifest 被 loadManifestSilently 使用）
var (
	_ = (*config.Config)(nil)
	_ = (*manifest.Manifest)(nil)
)
