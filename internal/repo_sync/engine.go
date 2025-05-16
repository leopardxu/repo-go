package repo_sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/logger"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/progress"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/ssh"
	"github.com/cix-code/gogo/internal/workerpool"
	"golang.org/x/sync/errgroup"
)

// SyncError 表示同步过程中的错误
type SyncError struct {
	ProjectName string
	Phase       string
	Err         error
	Output      string
	Timestamp   time.Time // 添加时间戳
	RetryCount  int       // 添加重试计数
}

// Error 实现 error 接口
func (e *SyncError) Error() string {
	timeStr := e.Timestamp.Format("2006-01-02 15:04:05")
	retryInfo := ""
	if e.RetryCount > 0 {
		retryInfo = fmt.Sprintf(" (重试次数: %d)", e.RetryCount)
	}

	if e.Output != "" {
		return fmt.Sprintf("[%s] %s 在 %s 阶段失败%s: %v\n%s",
			timeStr, e.ProjectName, e.Phase, retryInfo, e.Err, e.Output)
	}
	return fmt.Sprintf("[%s] %s 在 %s 阶段失败%s: %v",
		timeStr, e.ProjectName, e.Phase, retryInfo, e.Err)
}

// NewMultiError 创建包含多个错误的错误对象
func NewMultiError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return fmt.Errorf("发生了 %d 个错误", len(errs))
}

// Options 包含同步引擎的选项
// Options moved to options.go to avoid duplicate declarations

// Engine 同步引擎
type Engine struct {
	projects        []*project.Project
	config          *config.Config
	options         *Options
	logger          logger.Logger
	progressReport  progress.Reporter
	workerPool      *workerpool.WorkerPool
	repoRoot        string
	errors          []error
	errorsMu        sync.Mutex
	errResults      []string
	manifestCache   []byte
	manifest        *manifest.Manifest
	errEvent        chan error           // 添加 errEvent 字段
	sshProxy        *ssh.Proxy           // 添加 sshProxy 字段
	fetchTimes      map[string]time.Time // 添加 fetchTimes 字段
	fetchTimesLock  sync.Mutex           // 添加 fetchTimesLock 字段
	ctx             context.Context      // 添加 ctx 字段
	log             logger.Logger        // 添加 log 字段
	branchName      string               // 要检出的分支名称
	checkoutStats   *checkoutStats       // 检出操作的统计信息
	commitHash      string               // 要cherry-pick的提交哈希
	cherryPickStats *cherryPickStats     // cherry-pick操作的统计信息
}

// NewEngine 创建同步引擎
func NewEngine(options *Options, manifest *manifest.Manifest, log logger.Logger) *Engine {
	if options.Jobs <= 0 {
		options.Jobs = runtime.NumCPU()
	}

	var progressReport progress.Reporter
	if !options.Quiet {
		progressReport = progress.NewConsoleReporter()
	}

	// 初始化项目列表
	var projects []*project.Project
	// 项目列表将在后续操作中填充

	return &Engine{
		projects:       projects,
		options:        options,
		manifest:       manifest,
		logger:         log,
		progressReport: progressReport,
		workerPool:     workerpool.New(options.Jobs),
		errEvent:       make(chan error),           // 初始化 errEvent 字段
		fetchTimes:     make(map[string]time.Time), // 初始化 fetchTimes 映射
		ctx:            context.Background(),       // 初始化 ctx 字段
		log:            log,                        // 初始化 log 字段
	}
}

// Sync 执行同步
func (e *Engine) Sync() error {
	// 创建带取消功能的上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // 确保函数退出时取消上下文

	totalProjects := len(e.projects)
	if totalProjects == 0 {
		e.logger.Info("没有项目需要同步")
		return nil
	}

	// 记录开始时间，用于计算预估完成时间
	startTime := time.Now()

	if !e.options.Quiet {
		e.logger.Info("同步 %d 个项目，并发数: %d", totalProjects, e.options.Jobs)
		if e.progressReport != nil {
			e.progressReport.Start(totalProjects)
		}
	}

	var count int32
	var successCount int32
	var failCount int32

	// 提交同步任务
	for _, p := range e.projects {
		project := p // 创建副本避免闭包问题
		e.workerPool.Submit(func() {
			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return // 如果上下文已取消，则不执行任务
			default:
				// 继续执行
			}

			err := e.syncProject(project)

			current := atomic.AddInt32(&count, 1)
			if err != nil {
				atomic.AddInt32(&failCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}

			if !e.options.Quiet && e.progressReport != nil {
				status := "完成"
				if err != nil {
					status = "失败"
				}

				// 计算预估完成时间
				var etaStr string
				if current > 0 && current < int32(totalProjects) {
					elapsed := time.Since(startTime)
					estimatedTotal := elapsed * time.Duration(totalProjects) / time.Duration(current)
					estimatedRemaining := estimatedTotal - elapsed
					if estimatedRemaining > 0 {
						etaStr = fmt.Sprintf("，预计剩余时间: %s", formatDuration(estimatedRemaining))
					}
				}

				progressMsg := fmt.Sprintf("%s: %s (进度: %d/%d, 成功: %d, 失败: %d%s)",
					project.Name, status, current, totalProjects,
					successCount, failCount, etaStr)
				e.progressReport.Update(int(current), progressMsg)
			}

			if err != nil {
				e.errorsMu.Lock()
				e.errors = append(e.errors, err)
				e.errorsMu.Unlock()
				e.logger.Error("同步项目 %s 失败: %v", project.Name, err)
			} else if !e.options.Quiet {
				e.logger.Debug("同步项目 %s 完成", project.Name)
			}
		})
	}

	// 等待所有任务完成
	e.workerPool.Wait()

	if !e.options.Quiet && e.progressReport != nil {
		e.progressReport.Finish()
	}

	// 计算总耗时
	totalDuration := time.Since(startTime)

	// 汇总错误
	if len(e.errors) > 0 {
		e.logger.Error("同步完成，有 %d 个项目失败，总耗时: %s",
			len(e.errors), formatDuration(totalDuration))
		return NewMultiError(e.errors)
	}

	e.logger.Info("所有项目同步完成，总耗时: %s", formatDuration(totalDuration))
	return nil
}

// formatDuration 格式化持续时间为人类可读格式
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%d小时%d分钟%d秒", h, m, s)
	} else if m > 0 {
		return fmt.Sprintf("%d分钟%d秒", m, s)
	}
	return fmt.Sprintf("%d秒", s)
}

// syncProject 同步单个项目
func (e *Engine) syncProject(p *project.Project) error {
	// 检查项目目录是否存在
	exists, err := e.projectExists(p)
	if err != nil {
		return fmt.Errorf("检查项目 %s 失败: %w", p.Name, err)
	}

	if !exists {
		// 克隆项目
		if !e.options.Quiet {
			e.logger.Info("克隆项目: %s", p.Name)
		}
		return e.cloneProject(p)
	} else {
		// 更新项目
		if !e.options.NetworkOnly && !e.options.LocalOnly {
			if !e.options.Quiet {
				e.logger.Info("更新项目: %s", p.Name)
			}
		}

		if !e.options.LocalOnly {
			// 执行网络操作
			if err := e.fetchProject(p); err != nil {
				return err
			}
		}

		if !e.options.NetworkOnly {
			// 执行本地操作
			if err := e.checkoutProject(p); err != nil {
				return err
			}
		}
	}

	return nil
}

// resolveRemoteURL 解析项目的远程URL
func (e *Engine) resolveRemoteURL(p *project.Project) string {
	// 确保使用项目的 RemoteURL 属性
	remoteURL := p.RemoteURL

	// 如果是相对路径，转换为完整的 URL
	if remoteURL == ".." || strings.HasPrefix(remoteURL, "../") || strings.HasPrefix(remoteURL, "./") {
		// 尝试获取配置
		var cfg *config.Config
		var manifestURL string

		// 首先检查 e.config 是否已初始化
		if e.config != nil && e.config.ManifestURL != "" {
			cfg = e.config
			manifestURL = e.config.ManifestURL
		} else {
			// 如果 e.config 为空或 ManifestURL 为空，尝试从文件加载配置
			var err error
			cfg, err = config.Load()
			if err == nil && cfg != nil {
				// 更新 Engine 的配置
				e.config = cfg
				manifestURL = cfg.ManifestURL
				if e.options != nil && e.options.Verbose && e.logger != nil {
					e.logger.Debug("已从文件加载配置，ManifestURL: %s", manifestURL)
				}
			} else if e.logger != nil {
				e.logger.Debug("无法从文件加载配置: %v", err)
			}
		}

		// 如果成功获取到配置和ManifestURL，解析相对路径
		if cfg != nil && manifestURL != "" {
			// 安全地调用 ExtractBaseURLFromManifestURL 方法
			baseURL := cfg.ExtractBaseURLFromManifestURL(manifestURL)
			if baseURL != "" {
				// 移除相对路径前缀
				var relPath string
				if remoteURL == ".." {
					// 处理单独的 ".." 路径
					// 对于 ".."，我们需要使用项目名称作为路径
					relPath = p.Name
				} else {
					relPath = strings.TrimPrefix(remoteURL, "../")
					relPath = strings.TrimPrefix(relPath, "./")
					// 如果相对路径为空，使用项目名称
					if relPath == "" {
						relPath = p.Name
					}
				}

				// 确保baseURL不以/结尾
				baseURL = strings.TrimSuffix(baseURL, "/")

				// 构建完整URL
				remoteURL = baseURL + "/" + relPath

				if e.options != nil && e.options.Verbose && e.logger != nil {
					e.logger.Debug("将相对路径 %s 转换为远程 URL: %s", p.RemoteURL, remoteURL)
				}
			}
		} else if e.logger != nil {
			// 记录警告日志，配置为空或缺少ManifestURL
			e.logger.Debug("无法解析相对路径 %s: 配置为空或缺少ManifestURL", p.RemoteURL)
		}
	}

	return remoteURL
}

// fetchProject 执行单个项目的网络同步
func (e *Engine) fetchProject(p *project.Project) error {
	// 输出详细日志，显示实际使用的远程 URL
	if e.options.Verbose {
		e.logger.Debug("正在获取项目 %s，原始远程 URL: %s", p.Name, p.RemoteURL)
	}

	// 解析远程URL
	remoteURL := e.resolveRemoteURL(p)
	// 更新项目的 RemoteURL 为解析后的 URL
	p.RemoteURL = remoteURL

	// 执行 Git 操作
	// 检查远程仓库是否存在
	if err := e.ensureRemoteExists(p, remoteURL); err != nil {
		return &SyncError{
			ProjectName: p.Name,
			Phase:       "ensure_remote",
			Err:         err,
			Timestamp:   time.Now(),
		}
	}

	// 执行 fetch 命令
	args := []string{"-C", p.Worktree, "fetch"}
	if e.options.Tags {
		args = append(args, "--tags")
	}
	if e.options.Quiet {
		args = append(args, "--quiet")
	}

	// 使用远程名称
	args = append(args, p.RemoteName)

	// 添加重试机制
	const maxRetries = 3
	var lastErr error
	var stderr bytes.Buffer

	for retryCount := 0; retryCount <= maxRetries; retryCount++ {
		// 如果不是第一次尝试，则等待一段时间后重试
		if retryCount > 0 {
			retryDelay := time.Duration(retryCount) * 2 * time.Second
			e.logger.Info("正在重试获取项目 %s (第 %d 次尝试)，将在 %v 后重试",
				p.Name, retryCount, retryDelay)
			time.Sleep(retryDelay)

			// 清空上一次的错误输出
			stderr.Reset()
		}

		// 执行 git fetch
		cmd := exec.Command("git", args...)
		cmd.Stderr = &stderr
		lastErr = cmd.Run()

		if lastErr == nil {
			// 成功获取，跳出重试循环
			break
		}

		// 如果已经达到最大重试次数，则返回错误
		if retryCount == maxRetries {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "fetch",
				Err:         lastErr,
				Output:      stderr.String(),
				Timestamp:   time.Now(),
				RetryCount:  retryCount,
			}
		}
	}

	// 如果启用了 LFS，执行 LFS 拉取
	if e.options.GitLFS {
		if err := e.pullLFS(p); err != nil {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "lfs_pull",
				Err:         err,
			}
		}
	}

	return nil
}

// cloneProject 克隆单个项目
func (e *Engine) cloneProject(p *project.Project) error {
	// 解析远程URL
	remoteURL := e.resolveRemoteURL(p)
	// 更新项目的 RemoteURL 为解析后的 URL
	p.RemoteURL = remoteURL

	// 创建父目录
	if err := os.MkdirAll(filepath.Dir(p.Worktree), 0755); err != nil {
		return &SyncError{
			ProjectName: p.Name,
			Phase:       "mkdir",
			Err:         err,
			Timestamp:   time.Now(),
		}
	}

	// 构建 clone 命令
	args := []string{"clone"}

	// 添加 LFS 支持
	if e.options.GitLFS {
		// 确保 git-lfs 已安装
		if _, err := exec.LookPath("git-lfs"); err == nil {
			args = append(args, "--filter=blob:limit=0")
		}
	}

	if e.options.Quiet {
		args = append(args, "--quiet")
	}

	// 添加远程URL和目标目录
	args = append(args, remoteURL, p.Worktree)

	// 添加重试机制
	const maxRetries = 3
	var lastErr error
	var stderr bytes.Buffer

	for retryCount := 0; retryCount <= maxRetries; retryCount++ {
		// 如果不是第一次尝试，则等待一段时间后重试
		if retryCount > 0 {
			retryDelay := time.Duration(retryCount) * 3 * time.Second
			e.logger.Info("正在重试克隆项目 %s (第 %d 次尝试)，将在 %v 后重试",
				p.Name, retryCount, retryDelay)
			time.Sleep(retryDelay)

			// 清空上一次的错误输出
			stderr.Reset()

			// 检查目标目录是否已存在但不完整，如果存在则删除
			if _, err := os.Stat(p.Worktree); err == nil {
				e.logger.Info("删除不完整的克隆目录: %s", p.Worktree)
				os.RemoveAll(p.Worktree)
			}
		}

		// 执行 clone 命令
		cmd := exec.Command("git", args...)
		cmd.Stderr = &stderr
		lastErr = cmd.Run()

		if lastErr == nil {
			// 成功克隆，跳出重试循环
			break
		}

		// 如果已经达到最大重试次数，则返回错误
		if retryCount == maxRetries {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "clone",
				Err:         lastErr,
				Output:      stderr.String(),
				Timestamp:   time.Now(),
				RetryCount:  retryCount,
			}
		}
	}

	// 克隆成功后，设置远程仓库
	if err := e.setupRemote(p, remoteURL); err != nil {
		return &SyncError{
			ProjectName: p.Name,
			Phase:       "setup_remote",
			Err:         err,
		}
	}

	// 如果启用了 LFS，执行 LFS 拉取
	if e.options.GitLFS {
		if err := e.pullLFS(p); err != nil {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "lfs_pull",
				Err:         err,
			}
		}
	}

	return nil
}

// checkoutProject 检出项目
func (e *Engine) checkoutProject(p *project.Project) error {
	// 执行 checkout 命令
	args := []string{"-C", p.Worktree, "checkout"}
	if e.options.Detach {
		args = append(args, "--detach")
	}
	args = append(args, p.Revision)

	// 添加重试机制
	const maxRetries = 2 // 检出操作通常不需要太多重试
	var lastErr error
	var stderr bytes.Buffer

	for retryCount := 0; retryCount <= maxRetries; retryCount++ {
		// 如果不是第一次尝试，则等待一段时间后重试
		if retryCount > 0 {
			retryDelay := time.Duration(retryCount) * time.Second
			e.logger.Info("正在重试检出项目 %s 的 %s 分支 (第 %d 次尝试)，将在 %v 后重试",
				p.Name, p.Revision, retryCount, retryDelay)
			time.Sleep(retryDelay)

			// 清空上一次的错误输出
			stderr.Reset()

			// 如果检出失败，可能是因为有未提交的更改，尝试强制检出
			if retryCount == maxRetries {
				e.logger.Info("尝试强制检出项目 %s", p.Name)
				// 添加 --force 参数
				forceArgs := make([]string, len(args))
				copy(forceArgs, args)
				// 在 checkout 后插入 --force
				forceArgs = append(forceArgs[:3], append([]string{"--force"}, forceArgs[3:]...)...)
				args = forceArgs
			}
		}

		// 执行 checkout 命令
		cmd := exec.Command("git", args...)
		cmd.Stderr = &stderr
		lastErr = cmd.Run()

		if lastErr == nil {
			// 成功检出，跳出重试循环
			break
		}

		// 如果已经达到最大重试次数，则返回错误
		if retryCount == maxRetries {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "checkout",
				Err:         lastErr,
				Output:      stderr.String(),
				Timestamp:   time.Now(),
				RetryCount:  retryCount,
			}
		}
	}

	return nil
}

// projectExists 检查项目目录是否存在
func (e *Engine) projectExists(p *project.Project) (bool, error) {
	gitDir := filepath.Join(p.Worktree, ".git")
	_, err := os.Stat(gitDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// setupRemote 设置远程仓库
func (e *Engine) setupRemote(p *project.Project, remoteURL string) error {
	// 检查远程仓库是否已存在
	cmd := exec.Command("git", "-C", p.Worktree, "remote")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("获取远程仓库列表失败: %w", err)
	}

	remotes := strings.Split(strings.TrimSpace(string(output)), "\n")
	remoteExists := false
	for _, r := range remotes {
		if r == p.RemoteName {
			remoteExists = true
			break
		}
	}

	// 如果远程仓库不存在，添加它
	if !remoteExists {
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "add", p.RemoteName, remoteURL)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("添加远程仓库失败: %w", err)
		}
	} else {
		// 如果远程仓库已存在，更新URL
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "set-url", p.RemoteName, remoteURL)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("更新远程仓库URL失败: %w", err)
		}
	}

	return nil
}

// ensureRemoteExists 确保远程仓库存在
func (e *Engine) ensureRemoteExists(p *project.Project, remoteURL string) error {
	// 检查远程仓库是否已存在
	cmd := exec.Command("git", "-C", p.Worktree, "remote")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("获取远程仓库列表失败: %w", err)
	}

	remotes := strings.Split(strings.TrimSpace(string(output)), "\n")
	remoteExists := false
	for _, r := range remotes {
		if r == p.RemoteName {
			remoteExists = true
			break
		}
	}

	// 如果远程仓库不存在，添加它
	if !remoteExists {
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "add", p.RemoteName, remoteURL)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("添加远程仓库失败: %w", err)
		}
	} else {
		// 检查远程URL是否正确
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "get-url", p.RemoteName)
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("获取远程仓库URL失败: %w", err)
		}

		currentURL := strings.TrimSpace(string(output))
		if currentURL != remoteURL {
			// 更新远程URL
			cmd = exec.Command("git", "-C", p.Worktree, "remote", "set-url", p.RemoteName, remoteURL)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("更新远程仓库URL失败: %w", err)
			}
		}
	}

	return nil
}

// pullLFS 拉取 LFS 文件
func (e *Engine) pullLFS(p *project.Project) error {
	// 检查是否安装了 git-lfs
	if _, err := exec.LookPath("git-lfs"); err != nil {
		// git-lfs 未安装，跳过
		return nil
	}

	// 检查仓库是否使用 LFS
	cmd := exec.Command("git", "-C", p.Worktree, "lfs", "ls-files")
	output, err := cmd.Output()
	if err != nil {
		// 可能不是 LFS 仓库，跳过
		return nil
	}

	// 如果有 LFS 文件，执行拉取
	if len(output) > 0 {
		cmd = exec.Command("git", "-C", p.Worktree, "lfs", "pull")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("LFS 拉取失败: %w", err)
		}
	}

	return nil
}

// fetchMainParallel 并行执行网络同步
func (e *Engine) fetchMainParallel(projects []*project.Project) error {
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(e.options.JobsNetwork)

	var wg sync.WaitGroup
	for _, p := range projects {
		p := p
		wg.Add(1)
		g.Go(func() error {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return e.fetchProject(p)
			}
		})
	}

	wg.Wait()
	return g.Wait()
}

// checkoutProject 执行单个项目的本地检出
// checkoutProjectSimple 简单检出项目
func (e *Engine) checkoutProjectSimple(p *project.Project) error {
	// 检查项目工作目录是否存在
	if _, err := os.Stat(p.Worktree); os.IsNotExist(err) {
		return fmt.Errorf("project directory %q does not exist", p.Worktree)
	}

	// 实现项目本地检出逻辑
	return nil
}

// checkoutParallel 并行执行本地检出
func (e *Engine) checkoutParallel(projects []*project.Project, hyperSyncProjects []*project.Project) error {
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(e.options.JobsCheckout)

	var wg sync.WaitGroup
	for _, p := range projects {
		p := p
		wg.Add(1)
		g.Go(func() error {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return e.checkoutProjectSimple(p)
			}
		})
	}

	wg.Wait()
	return g.Wait()
}

// Errors 返回同步过程中收集的错误
func (e *Engine) Errors() []string {
	return e.errResults
}

// Cleanup 清理资源并释放内存
func (e *Engine) Cleanup() {
	// 停止工作池
	if e.workerPool != nil {
		e.workerPool.Stop()
	}

	// 关闭错误通道
	if e.errEvent != nil {
		close(e.errEvent)
	}

	// 清空错误列表
	e.errorsMu.Lock()
	e.errors = nil
	e.errResults = nil
	e.errorsMu.Unlock()

	// 清空项目列表
	e.projects = nil

	// 清空缓存
	e.manifestCache = nil

	// 记录清理完成
	e.logger.Debug("同步引擎资源已清理完毕")
}

// updateProjectList 更新项目列表
func (e *Engine) updateProjectList() error {
	newProjectPaths := []string{}
	for _, project := range e.projects {
		if project.Relpath != "" {
			newProjectPaths = append(newProjectPaths, project.Relpath)
		}
	}

	fileName := "project.list"
	filePath := filepath.Join(e.manifest.Subdir, fileName)
	oldProjectPaths := []string{}

	if _, err := os.Stat(filePath); err == nil {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("读取项目列表失败: %w", err)
		}
		oldProjectPaths = strings.Split(string(data), "\n")

		// 按照反向顺序，先删除子文件夹再删除父文件夹
		for _, path := range oldProjectPaths {
			if path == "" {
				continue
			}
			if !contains(newProjectPaths, path) {
				gitdir := filepath.Join(e.manifest.Topdir, path, ".git")
				if _, err := os.Stat(gitdir); err == nil {
					// 创建临时项目对象来删除工作树
					p := &project.Project{
						Name:     path,
						Worktree: filepath.Join(e.manifest.Topdir, path),
						Gitdir:   gitdir,
					}
					if err := p.DeleteWorktree(e.options.Quiet, e.options.ForceRemoveDirty); err != nil {
						return fmt.Errorf("删除工作树 %s 失败: %w", path, err)
					}
				}
			}
		}
	}

	// 排序并写入新的项目列表
	sort.Strings(newProjectPaths)
	if err := os.WriteFile(filePath, []byte(strings.Join(newProjectPaths, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("写入项目列表失败: %w", err)
	}

	return nil
}

// updateCopyLinkfileList 更新复制和链接文件列表
func (e *Engine) updateCopyLinkfileList() error {
	newLinkfilePaths := []string{}
	newCopyfilePaths := []string{}

	for _, project := range e.projects {
		for _, linkfile := range project.Linkfiles {
			newLinkfilePaths = append(newLinkfilePaths, linkfile.Dest)
		}
		for _, copyfile := range project.Copyfiles {
			newCopyfilePaths = append(newCopyfilePaths, copyfile.Dest)
		}
	}

	newPaths := map[string][]string{
		"linkfile": newLinkfilePaths,
		"copyfile": newCopyfilePaths,
	}

	copylinkfileName := "copy-link-files.json"
	copylinkfilePath := filepath.Join(e.manifest.Subdir, copylinkfileName)
	oldCopylinkfilePaths := map[string][]string{}

	if _, err := os.Stat(copylinkfilePath); err == nil {
		data, err := os.ReadFile(copylinkfilePath)
		if err != nil {
			return fmt.Errorf("读取copy-link-files.json失败: %w", err)
		}

		if err := json.Unmarshal(data, &oldCopylinkfilePaths); err != nil {
			fmt.Printf("错误: %s 不是一个JSON格式的文件。\n", copylinkfilePath)
			os.Remove(copylinkfilePath)
			return nil
		}

		// 删除不再需要的文件
		needRemoveFiles := []string{}
		needRemoveFiles = append(needRemoveFiles,
			difference(oldCopylinkfilePaths["linkfile"], newLinkfilePaths)...)
		needRemoveFiles = append(needRemoveFiles,
			difference(oldCopylinkfilePaths["copyfile"], newCopyfilePaths)...)

		for _, file := range needRemoveFiles {
			os.Remove(file)
		}
	}

	// 创建新的copy-link-files.json
	data, err := json.Marshal(newPaths)
	if err != nil {
		return fmt.Errorf("序列化copy-link-files.json失败: %w", err)
	}

	if err := os.WriteFile(copylinkfilePath, data, 0644); err != nil {
		return fmt.Errorf("写入copy-link-files.json失败: %w", err)
	}

	return nil
}

// reloadManifest 重新加载清单
func (e *Engine) reloadManifest(manifestName string, localOnly bool, groups []string) error {
	if manifestName == "" {
		manifestName = e.config.ManifestName
	}

	// 解析清单
	parser := manifest.NewParser()
	newManifest, err := parser.ParseFromFile(manifestName, groups)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 更新清单
	e.manifest = newManifest

	// 更新项目列表 - 修复参数类型
	projects, err := project.NewManagerFromManifest(e.manifest, e.config).GetProjectsInGroups(e.options.Groups)
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}

	e.projects = projects

	return nil
}

// getProjects 获取项目列表
func (e *Engine) getProjects() ([]*project.Project, error) {
	// 如果已经有项目列表，直接返回
	if len(e.projects) > 0 {
		return e.projects, nil
	}

	// 获取项目列表 - 修复参数类型
	projects, err := project.NewManagerFromManifest(e.manifest, e.config).GetProjectsInGroups(e.options.Groups)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects: %w", err)
	}

	e.projects = projects

	return e.projects, nil
}

// reloadManifestFromCache 重新加载manifest
func (e *Engine) reloadManifestFromCache() error {
	if len(e.manifestCache) == 0 {
		return fmt.Errorf("manifest cache is empty")
	}

	// 解析缓存的manifest数据
	parser := manifest.NewParser()
	newManifest, err := parser.ParseFromBytes(e.manifestCache, e.options.Groups)
	if err != nil {
		return fmt.Errorf("failed to parse manifest from cache: %w", err)
	}

	// 更新引擎中的manifest
	e.manifest = newManifest

	// 重新获取项目列表
	projects, err := project.NewManagerFromManifest(e.manifest, e.config).GetProjectsInGroups(e.options.Groups)
	if err != nil {
		return fmt.Errorf("failed to get projects from cached manifest: %w", err)
	}
	e.projects = projects

	return nil
}

// updateProjectsRevisionId 方法
func (e *Engine) updateProjectsRevisionId() (string, error) {
	// 创建超级项目
	sp, err := NewSuperproject(e.manifest, e.options.Quiet)
	if err != nil {
		return "", fmt.Errorf("创建超级项目失败: %w", err)
	}

	// 更新项目的修订ID
	manifestPath, err := sp.UpdateProjectsRevisionId(e.projects)
	if err != nil {
		return "", fmt.Errorf("更新项目修订ID失败: %w", err)
	}

	return manifestPath, nil
}

// SetSilentMode 设置引擎的静默模式
func (e *Engine) SetSilentMode(silent bool) {
	// 根据静默模式设置日志级别或其他相关配置
	// 这里可以根据实际需求实现具体逻辑
}

// Run 执行同步操作
func (e *Engine) Run() error {
	// 初始化项目列表
	projects, err := e.getProjects()
	if err != nil {
		return fmt.Errorf("获取项目列表失败: %w", err)
	}
	e.projects = projects

	// 执行同步操作
	return e.Sync()
}

// SetProjects 设置要同步的项目列表
func (e *Engine) SetProjects(projects []*project.Project) {
	e.projects = projects
}
