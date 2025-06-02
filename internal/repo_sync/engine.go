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

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/progress"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/leopardxu/repo-go/internal/ssh"
	"github.com/leopardxu/repo-go/internal/workerpool"
	"golang.org/x/sync/errgroup"
)

// SyncError 表示同步过程中的错误
type SyncError struct {
	ProjectName string
	Phase       string
	Err         error
	Output      string
	Timestamp   time.Time // 添加时间�?
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
		return fmt.Sprintf("[%s] %s �?%s 阶段失败%s: %v\n%s",
			timeStr, e.ProjectName, e.Phase, retryInfo, e.Err, e.Output)
	}
	return fmt.Sprintf("[%s] %s �?%s 阶段失败%s: %v",
		timeStr, e.ProjectName, e.Phase, retryInfo, e.Err)
}

// NewMultiError 创建包含多个错误的错误对�?
func NewMultiError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return fmt.Errorf("发生�?%d 个错�?, len(errs))
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
	commitHash      string               // 要cherry-pick的提交哈�?
	cherryPickStats *cherryPickStats     // cherry-pick操作的统计信�?
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

	// 初始化项目列�?
	var projects []*project.Project
	// 项目列表将在后续操作中填�?

	return &Engine{
		projects:       projects,
		options:        options,
		manifest:       manifest,
		logger:         log,
		progressReport: progressReport,
		workerPool:     workerpool.New(options.Jobs),
		errEvent:       make(chan error),           // 初始�?errEvent 字段
		fetchTimes:     make(map[string]time.Time), // 初始�?fetchTimes 映射
		ctx:            context.Background(),       // 初始�?ctx 字段
		log:            log,                        // 初始�?log 字段
	}
}

// Sync 执行同步
func (e *Engine) Sync() error {
	// 创建带取消功能的上下�?
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // 确保函数退出时取消上下�?

	totalProjects := len(e.projects)
	if totalProjects == 0 {
		e.logger.Info("没有项目需要同�?)
		return nil
	}

	// 记录开始时间，用于计算预估完成时间
	startTime := time.Now()

	if !e.options.Quiet {
		e.logger.Info("同步 %d 个项目，并发�? %d", totalProjects, e.options.Jobs)
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
			// 检查上下文是否已取�?
			select {
			case <-ctx.Done():
				return // 如果上下文已取消，则不执行任�?
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
						etaStr = fmt.Sprintf("，预计剩余时�? %s", formatDuration(estimatedRemaining))
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

	// 等待所有任务完�?
	e.workerPool.Wait()

	if !e.options.Quiet && e.progressReport != nil {
		e.progressReport.Finish()
	}

	// 计算总耗时
	totalDuration := time.Since(startTime)

	// 汇总错�?
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
		return fmt.Sprintf("%d小时%d分钟%d�?, h, m, s)
	} else if m > 0 {
		return fmt.Sprintf("%d分钟%d�?, m, s)
	}
	return fmt.Sprintf("%d�?, s)
}

// syncProject 同步单个项目
func (e *Engine) syncProject(p *project.Project) error {
	// 检查项目目录是否存�?
	exists, err := e.projectExists(p)
	if err != nil {
		return fmt.Errorf("检查项�?%s 失败: %w", p.Name, err)
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

		// 更新成功后处�?linkfile �?copyfile
		if !e.options.NetworkOnly { // 只有在执行了本地操作后才处理
		    if err := e.processLinkAndCopyFiles(p); err != nil {
		        return &SyncError{
		            ProjectName: p.Name,
		            Phase:       "link_copy_files_after_update",
		            Err:         err,
		            Timestamp:   time.Now(),
		        }
		    }
		}
	}

	return nil
}

// resolveRemoteURL 解析项目的远程URL
func (e *Engine) resolveRemoteURL(p *project.Project) string {
	// 确保使用项目�?RemoteURL 属�?
	remoteURL := p.RemoteURL

	if remoteURL == "" {
		remoteURL = ".."
	}

	// 如果是相对路径，转换为完整的 URL
	if remoteURL == ".." || strings.HasPrefix(remoteURL, "../") || strings.HasPrefix(remoteURL, "./") {
		// 尝试从清单中获取远程URL
		var baseURL string
		var remoteName string
		var cfg *config.Config
		var manifestURL string

		// 首先尝试从清单中获取远程URL
		if e.manifest != nil {
			// 获取项目的远程名�?
			remoteName = p.RemoteName

			// 如果项目未指定远程名称，则使用默认远�?
			if remoteName == "" {
				// 如果设置了DefaultRemote选项，优先使用它
				if e.options != nil && e.options.DefaultRemote != "" {
					remoteName = e.options.DefaultRemote
					if e.options.Verbose && e.logger != nil {
						e.logger.Debug("项目 %s 未指定远程名称，使用命令行指定的默认远程: %s", p.Name, remoteName)
					}
				} else if e.manifest.Default.Remote != "" {
					// 否则使用清单中的默认远程
					remoteName = e.manifest.Default.Remote
					if e.options != nil && e.options.Verbose && e.logger != nil {
						e.logger.Debug("项目 %s 未指定远程名称，使用清单中的默认远程: %s", p.Name, remoteName)
					}
				}
			}

			// 从清单中获取远程URL
			if remoteName != "" {
				var err error
				baseURL, err = e.manifest.GetRemoteURL(remoteName)
				if err == nil && baseURL != "" {
					if e.options != nil && e.options.Verbose && e.logger != nil {
						e.logger.Debug("从清单中获取到远�?%s 的URL: %s", remoteName, baseURL)
					}
				} else if e.logger != nil {
					e.logger.Debug("无法从清单中获取远程 %s 的URL: %v", remoteName, err)
				}
			}
		}
		// 辅助函数：安全地移除URL最后一个路径段，保留协议和主机名部�?
		trimLastPathSegment := func(url string) string {
			// 确保URL不以/结尾
			url = strings.TrimSuffix(url, "/")

			// 检查是否是有效的URL格式
			hasProtocol := strings.Contains(url, "://")

			// 分割URL
			parts := strings.Split(url, "/")
			if len(parts) <= 3 && hasProtocol {
				// URL格式�?protocol://host �?protocol://host/，保持不�?
				return url
			}

			// 移除最后一个路径段
			return strings.Join(parts[:len(parts)-1], "/")
		}

		// 如果无法从清单中获取远程URL或者URL不是有效的协议格式，则回退到从配置中获取的方法
		if !(strings.HasPrefix(baseURL, "ssh://") || strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://")) {
			// 首先检�?e.config 是否已初始化
			if e.config != nil && e.config.ManifestURL != "" {
				cfg = e.config
				manifestURL = e.config.ManifestURL
				if e.options != nil && e.options.Verbose && e.logger != nil {
					e.logger.Debug("使用已加载的配置，ManifestURL: %s", manifestURL)
				}
			} else {
				// 如果 e.config 为空�?ManifestURL 为空，尝试从文件加载配置
				var err error
				cfg, err = config.Load()
				if err == nil && cfg != nil {
					// 更新 Engine 的配�?
					e.config = cfg
					manifestURL = cfg.ManifestURL
					if e.options != nil && e.options.Verbose && e.logger != nil {
						e.logger.Debug("已从文件加载配置，ManifestURL: %s", manifestURL)
					}
				} else {
					// 记录错误日志
					if e.logger != nil {
						e.logger.Error("无法从文件加载配�? %v", err)
					}
					// 尝试直接�?.repo/config.json 文件读取
					configPath := filepath.Join(".repo", "config.json")
					if _, statErr := os.Stat(configPath); statErr == nil {
						data, readErr := os.ReadFile(configPath)
						if readErr == nil {
							var configData struct {
								ManifestURL string `json:"manifest_url"`
							}
							if jsonErr := json.Unmarshal(data, &configData); jsonErr == nil && configData.ManifestURL != "" {
								manifestURL = configData.ManifestURL
								if e.options != nil && e.options.Verbose && e.logger != nil {
									e.logger.Debug("直接从config.json读取到ManifestURL: %s", manifestURL)
								}
							} else if e.logger != nil {
								e.logger.Debug("解析config.json失败或ManifestURL为空: %v", jsonErr)
							}
						} else if e.logger != nil {
							e.logger.Debug("读取config.json文件失败: %v", readErr)
						}
					} else if e.logger != nil {
						e.logger.Debug("config.json文件不存�? %v", statErr)
					}
				}
			}

			// 如果成功获取到ManifestURL，解析相对路�?
			if manifestURL != "" {
				// 如果cfg为空，创建一个临时配置对�?
				if cfg == nil {
					cfg = &config.Config{ManifestURL: manifestURL}
				}

				// 安全地调�?ExtractBaseURLFromManifestURL 方法
				baseURL = trimLastPathSegment(manifestURL)
				if baseURL != "" {
					if e.options != nil && e.options.Verbose && e.logger != nil {
						e.logger.Debug("从配置中提取的baseURL: %s", baseURL)
					}
				} else if e.logger != nil {
					e.logger.Error("无法从ManifestURL提取baseURL: %s", manifestURL)
				}
			} else if e.logger != nil {
				// 记录警告日志，配置为空或缺少ManifestURL
				e.logger.Error("无法解析相对路径 %s: 未能获取ManifestURL", p.RemoteURL)
			}
		}

		// 如果成功获取到baseURL，处理相对路�?
		if baseURL != "" {
			// 确保baseURL不以/结尾
			baseURL = strings.TrimSuffix(baseURL, "/")

			// 处理不同类型的相对路�?
			if remoteURL == ".." {
				// 处理remote为空或单独的".."路径
				// 移除baseURL最后一个路径段
				baseURL = trimLastPathSegment(baseURL)
				remoteURL = baseURL + "/" + p.Name
			} else if strings.HasPrefix(remoteURL, "../") {
				// 处理"../"开头的路径
				// 计算需要向上回溯的层数
				count := 0
				tempURL := remoteURL
				for strings.HasPrefix(tempURL, "../") {
					count++
					tempURL = tempURL[3:]
				}

				// 从baseURL中移除相应数量的路径�?
				tempBaseURL := baseURL
				for i := 0; i < count; i++ {
					tempBaseURL = trimLastPathSegment(tempBaseURL)
				}

				// 获取剩余路径并拼�?
				if tempURL == "" {
					// 如果只有../没有后续路径，直接拼接项目名�?
					remoteURL = tempBaseURL + "/" + p.Name
				} else {
					// 如果有后续路径，拼接后续路径和项目名�?
					remoteURL = tempBaseURL + "/" + tempURL + p.Name
				}
			} else if strings.HasPrefix(remoteURL, "./") {
				// 处理"./"开头的路径
				// 移除baseURL最后一个路径段
				baseURL = trimLastPathSegment(baseURL)

				// 获取./后面的路�?
				relPath := strings.TrimPrefix(remoteURL, "./")
				if relPath == "" {
					remoteURL = baseURL + "/" + p.Name
				} else {
					remoteURL = baseURL + "/" + relPath + p.Name
				}
			}

			if e.options != nil && e.options.Verbose && e.logger != nil {
				e.logger.Debug("将相对路�?%s 转换为远�?URL: %s", p.RemoteURL, remoteURL)
			}
		}
	}

	return remoteURL
}

// fetchProject 执行单个项目的网络同�?
func (e *Engine) fetchProject(p *project.Project) error {
	// 输出详细日志，显示实际使用的远程 URL
	if e.options.Verbose {
		e.logger.Debug("正在获取项目 %s，原始远�?URL: %s", p.Name, p.RemoteURL)
	}

	// 解析远程URL
	remoteURL := e.resolveRemoteURL(p)
	// 更新项目�?RemoteURL 为解析后�?URL
	p.RemoteURL = remoteURL

	// 执行 Git 操作
	// 检查远程仓库是否存�?
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
			e.logger.Info("正在重试获取项目 %s (�?%d 次尝�?，将�?%v 后重�?,
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
			// 成功获取，跳出重试循�?
			break
		}

		// 如果已经达到最大重试次数，则返回错�?
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

	// 如果启用�?LFS，执�?LFS 拉取
	if e.options.GitLFS {
		if err := e.pullLFS(p); err != nil {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "lfs_pull",
				Err:         err,
			}
		}
	}

	// 处理 linkfile �?copyfile
	if err := e.processLinkAndCopyFiles(p); err != nil {
		return &SyncError{
			ProjectName: p.Name,
			Phase:       "link_copy_files",
			Err:         err,
			Timestamp:   time.Now(),
		}
	}

	return nil
}

// cloneProject 克隆单个项目
func (e *Engine) cloneProject(p *project.Project) error {
	// 解析远程URL
	remoteURL := e.resolveRemoteURL(p)
	// 更新项目�?RemoteURL 为解析后�?URL
	p.RemoteURL = remoteURL

	// 创建父目�?
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
		// 确保 git-lfs 已安�?
		if _, err := exec.LookPath("git-lfs"); err == nil {
			args = append(args, "--filter=blob:limit=0")
		}
	}

	if e.options.Quiet {
		args = append(args, "--quiet")
	}

	// 添加远程URL和目标目�?
	args = append(args, remoteURL, p.Worktree)

	// 添加重试机制
	const maxRetries = 3
	var lastErr error
	var stderr bytes.Buffer

	for retryCount := 0; retryCount <= maxRetries; retryCount++ {
		// 如果不是第一次尝试，则等待一段时间后重试
		if retryCount > 0 {
			retryDelay := time.Duration(retryCount) * 3 * time.Second
			e.logger.Info("正在重试克隆项目 %s (�?%d 次尝�?，将�?%v 后重�?,
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
			// 成功克隆，跳出重试循�?
			break
		}

		// 如果已经达到最大重试次数，则返回错�?
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

	// 如果启用�?LFS，执�?LFS 拉取
	if e.options.GitLFS {
		if err := e.pullLFS(p); err != nil {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "lfs_pull",
				Err:         err,
			}
		}
	}

	// 处理 linkfile �?copyfile
	if err := e.processLinkAndCopyFiles(p); err != nil {
		return &SyncError{
			ProjectName: p.Name,
			Phase:       "link_copy_files",
			Err:         err,
			Timestamp:   time.Now(),
		}
	}

	return nil
}

// checkoutProject 检出项�?
func (e *Engine) checkoutProject(p *project.Project) error {
	// 执行 checkout 命令
	args := []string{"-C", p.Worktree, "checkout"}
	if e.options.Detach {
		args = append(args, "--detach")
	}
	if strings.HasPrefix(p.Revision, "refs/heads/") {
		p.Revision = strings.TrimPrefix(p.Revision, "refs/heads/")
	}
	if strings.HasPrefix(p.Revision, "refs/tags/") {
		p.Revision = strings.TrimPrefix(p.Revision, "refs/tags/")
	}
	args = append(args, p.Revision)

	// 添加重试机制
	const maxRetries = 2 // 检出操作通常不需要太多重�?
	var lastErr error
	var stderr bytes.Buffer

	for retryCount := 0; retryCount <= maxRetries; retryCount++ {
		// 如果不是第一次尝试，则等待一段时间后重试
		if retryCount > 0 {
			retryDelay := time.Duration(retryCount) * time.Second
			e.logger.Info("正在重试检出项�?%s �?%s 分支 (�?%d 次尝�?，将�?%v 后重�?,
				p.Name, p.Revision, retryCount, retryDelay)
			time.Sleep(retryDelay)

			// 清空上一次的错误输出
			stderr.Reset()

			// 如果检出失败，可能是因为有未提交的更改，尝试强制检�?
			if retryCount == maxRetries {
				e.logger.Info("尝试强制检出项�?%s", p.Name)
				// 添加 --force 参数
				forceArgs := make([]string, len(args))
				copy(forceArgs, args)
				// �?checkout 后插�?--force
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

		// 如果已经达到最大重试次数，则返回错�?
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

// projectExists 检查项目目录是否存�?
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

	if p.RemoteName == "" {
		p.RemoteName = "origin"
	}

	// 如果远程仓库不存在，添加�?
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
	if p.RemoteName == "" {
		p.RemoteName = "origin"
	}
	remotes := strings.Split(strings.TrimSpace(string(output)), "\n")
	remoteExists := false
	for _, r := range remotes {
		if r == p.RemoteName {
			remoteExists = true
			break
		}
	}
	// 如果远程仓库不存在，添加�?
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

	// 检查仓库是否使�?LFS
	cmd := exec.Command("git", "-C", p.Worktree, "lfs", "ls-files")
	output, err := cmd.Output()
	if err != nil {
		// 可能不是 LFS 仓库，跳�?
		return nil
	}

	// 如果�?LFS 文件，执行拉�?
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

// checkoutProject 执行单个项目的本地检�?
// checkoutProjectSimple 简单检出项�?
func (e *Engine) checkoutProjectSimple(p *project.Project) error {
	// 检查项目工作目录是否存�?
	if _, err := os.Stat(p.Worktree); os.IsNotExist(err) {
		return fmt.Errorf("project directory %q does not exist", p.Worktree)
	}

	// 实现项目本地检出逻辑
	return nil
}

// checkoutParallel 并行执行本地检�?
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

// processLinkAndCopyFiles 处理项目中的 linkfile �?copyfile
func (e *Engine) processLinkAndCopyFiles(p *project.Project) error {
	if p == nil {
		return fmt.Errorf("项目对象为空")
	}

	projectRoot := filepath.Join(e.repoRoot, p.Path) // 获取项目在工作区的实际路�?
	if e.repoRoot == "" { // 如果 repoRoot 未设置，尝试从项目工作树推断
	    // 这部分逻辑可能需要根据您的项目结构进行调�?
	    // 一个简单的假设是项目的工作树就是项目路径本身（相对于某个根�?
	    // 或者，如果项目路径是绝对路径，�?repoRoot 可以是其父目录的某个层级
	    // 为简化，这里假设项目路径是相对于当前工作目录�?
	    cwd, err := os.Getwd()
	    if err != nil {
	        return fmt.Errorf("无法获取当前工作目录: %w", err)
	    }
	    projectRoot = filepath.Join(cwd, p.Path)
	    // 如果 p.Worktree 已经包含完整路径，可以直接使�?
	    if filepath.IsAbs(p.Worktree) {
	        projectRoot = p.Worktree
	    } else {
	        projectRoot = filepath.Join(cwd, p.Worktree) // 假设 Worktree 是相对路�?
	    }
	    // 更健壮的方式是确�?e.repoRoot �?Engine 初始化时被正确设�?
	    if e.repoRoot == "" && e.manifest != nil && e.manifest.Topdir != "" {
	        e.repoRoot = e.manifest.Topdir
	        projectRoot = filepath.Join(e.repoRoot, p.Path)
	    }
	}


	// 处理 Copyfile
	for _, cpFile := range p.Copyfiles {
		sourcePath := filepath.Join(projectRoot, cpFile.Src) // 源文件在项目内部
		destPath := filepath.Join(e.repoRoot, cpFile.Dest)    // 目标文件在仓库根目录或其他指定位�?

		if !filepath.IsAbs(cpFile.Dest) { // 如果Dest是相对路径，则相对于repoRoot
		    destPath = filepath.Join(e.repoRoot, cpFile.Dest)
		} else { // 如果Dest是绝对路径，则直接使�?
		    destPath = cpFile.Dest
		}
		// 确保源文件相对于项目路径
		sourcePath = filepath.Join(projectRoot, cpFile.Src)


		e.logger.Info("复制文件: �?%s �?%s", sourcePath, destPath)

		input, err := os.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("读取源文�?%s 失败: %w", sourcePath, err)
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		    return fmt.Errorf("创建目标目录 %s 失败: %w", filepath.Dir(destPath), err)
		}

		if err := os.WriteFile(destPath, input, 0644); err != nil {
			return fmt.Errorf("写入目标文件 %s 失败: %w", destPath, err)
		}
	}

	// 处理 Linkfile
	for _, lnFile := range p.Linkfiles {
		// Linkfile �?Dest 通常是相对于仓库根目录的路径，Src 是相对于项目根目录的路径
		// targetPath 指向实际的文件或目录（源�?
		targetPath := filepath.Join(projectRoot, lnFile.Src) 
		// linkPath 是要创建的符号链接的路径（目标）
		linkPath := filepath.Join(e.repoRoot, lnFile.Dest)

		if !filepath.IsAbs(lnFile.Dest) { // 如果Dest是相对路径，则相对于repoRoot
		    linkPath = filepath.Join(e.repoRoot, lnFile.Dest)
		} else { // 如果Dest是绝对路径，则直接使�?
		    linkPath = lnFile.Dest
		}
		// 确保源文件相对于项目路径
		targetPath = filepath.Join(projectRoot, lnFile.Src)


		e.logger.Info("创建链接: �?%s 指向 %s", linkPath, targetPath)

		// 创建链接前，确保目标目录存在
		if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
			return fmt.Errorf("创建链接的目标目�?%s 失败: %w", filepath.Dir(linkPath), err)
		}

		// 如果链接已存在，先删�?
		if _, err := os.Lstat(linkPath); err == nil {
			if err := os.Remove(linkPath); err != nil {
				return fmt.Errorf("删除已存在的链接 %s 失败: %w", linkPath, err)
			}
		}

		// 在Windows上，创建符号链接需要管理员权限，或者开发者模式已开启�?
		// os.Symlink的target应该是相对于linkPath的相对路径，或者是一个绝对路径�?
		// 为了简单和跨平台性，我们先尝试将targetPath转换为相对于linkPath父目录的相对路径�?
        linkDir := filepath.Dir(linkPath)
        relTargetPath, err := filepath.Rel(linkDir, targetPath)
        if err != nil {
            // 如果无法计算相对路径（例如，它们不在同一个卷上），则直接使用绝对路径
            relTargetPath = targetPath
            e.logger.Debug("无法计算相对路径，将为链�?%s 使用绝对目标路径 %s: %v", linkPath, targetPath, err)
        }


		if err := os.Symlink(relTargetPath, linkPath); err != nil {
			return fmt.Errorf("创建符号链接�?%s �?%s 失败: %w", linkPath, relTargetPath, err)
		}
	}

	return nil
}

// Errors 返回同步过程中收集的错误
func (e *Engine) Errors() []string {
	return e.errResults
}

// Cleanup 清理资源并释放内�?
func (e *Engine) Cleanup() {
	// 停止工作�?
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
	e.logger.Debug("同步引擎资源已清理完�?)
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

		// 按照反向顺序，先删除子文件夹再删除父文件�?
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
						return fmt.Errorf("删除工作�?%s 失败: %w", path, err)
					}
				}
			}
		}
	}

	// 排序并写入新的项目列�?
	sort.Strings(newProjectPaths)
	if err := os.WriteFile(filePath, []byte(strings.Join(newProjectPaths, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("写入项目列表失败: %w", err)
	}

	return nil
}

// updateCopyLinkfileList 更新复制和链接文件列�?
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

// SetSilentMode 设置引擎的静默模�?
func (e *Engine) SetSilentMode(silent bool) {
	// 根据静默模式设置日志级别或其他相关配�?
	// 这里可以根据实际需求实现具体逻辑
}

// Run 执行同步操作
func (e *Engine) Run() error {
	// 初始化项目列�?
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
