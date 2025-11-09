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
		return fmt.Sprintf("[%s] %s 在%s 阶段失败%s: %v\n%s",
			timeStr, e.ProjectName, e.Phase, retryInfo, e.Err, e.Output)
	}
	return fmt.Sprintf("[%s] %s 在%s 阶段失败%s: %v",
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

	// 获取仓库根目录
	var repoRoot string
	if manifest != nil && manifest.Topdir != "" {
		repoRoot = manifest.Topdir
	} else {
		// 如果manifest.Topdir为空，尝试从当前工作目录推断
		cwd, err := os.Getwd()
		if err == nil {
			// 查找顶层仓库目录
			topDir := project.FindTopLevelRepoDir(cwd)
			if topDir != "" {
				repoRoot = topDir
			} else {
				repoRoot = cwd // 如果找不到顶层目录，使用当前目录
			}
		}
	}

	return &Engine{
		projects:       projects,
		options:        options,
		manifest:       manifest,
		logger:         log,
		progressReport: progressReport,
		workerPool:     workerpool.New(options.Jobs),
		repoRoot:       repoRoot,                   // 设置仓库根目录
		errEvent:       make(chan error),           // 初始化errEvent 字段
		fetchTimes:     make(map[string]time.Time), // 初始化fetchTimes 映射
		ctx:            context.Background(),       // 初始化ctx 字段
		log:            log,                        // 初始化log 字段
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
		e.logger.Info("同步 %d 个项目，并发数 %d", totalProjects, e.options.Jobs)
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
						etaStr = fmt.Sprintf("，预计剩余时 %s", formatDuration(estimatedRemaining))
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

	// 等待所有任务完
	e.workerPool.Wait()

	if !e.options.Quiet && e.progressReport != nil {
		e.progressReport.Finish()
	}

	// 计算总耗时
	totalDuration := time.Since(startTime)

	// 汇总错
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
		return fmt.Errorf("检查项%s 失败: %w", p.Name, err)
	}

	// 检查是否为镜像模式
	isMirror := false
	if e.options.Config != nil && e.options.Config.Mirror {
		isMirror = true
		if !e.options.Quiet {
			e.logger.Debug("项目 %s 使用镜像模式", p.Name)
		}
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
			// 执行本地操作（镜像模式下不需要检出特定分支）
			if !isMirror {
				if err := e.checkoutProject(p); err != nil {
					return err
				}
			}

			// 更新成功后处理linkfile copyfile（仅在非NetworkOnly模式且非镜像模式下）
			if !isMirror {
				e.logger.Info("项目 %s 更新完成，开始处理 linkfile 和 copyfile", p.Name)
				if err := e.processLinkAndCopyFiles(p); err != nil {
					e.logger.Error("项目 %s 更新后处理 linkfile 和 copyfile 失败: %v", p.Name, err)
					return &SyncError{
						ProjectName: p.Name,
						Phase:       "link_copy_files_after_update",
						Err:         err,
						Timestamp:   time.Now(),
					}
				}
				e.logger.Info("成功处理项目 %s 更新后的链接文件和复制文件", p.Name)

				// 处理 submodule（如果启用）
				if err := e.updateSubmodules(p); err != nil {
					e.logger.Error("项目 %s 更新 submodule 失败: %v", p.Name, err)
					// submodule 更新失败不阻断整个同步流程，只记录错误
					if !e.options.Quiet {
						e.logger.Warn("跳过项目 %s 的 submodule 更新", p.Name)
					}
				}
			} else {
				e.logger.Info("镜像模式，跳过处理项目 %s 的链接文件和复制文件", p.Name)
			}
		}
	}

	return nil
}

// resolveRemoteURL 解析项目的远程URL
func (e *Engine) resolveRemoteURL(p *project.Project) string {
	// 确保使用项目RemoteURL 属
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
			// 获取项目的远程名
			remoteName = p.RemoteName

			// 如果项目未指定远程名称，则使用默认远
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
						e.logger.Debug("从清单中获取到远%s 的URL: %s", remoteName, baseURL)
					}
				} else if e.logger != nil {
					e.logger.Debug("无法从清单中获取远程 %s 的URL: %v", remoteName, err)
				}
			}
		}
		// 辅助函数：安全地移除URL最后一个路径段，保留协议和主机名部
		trimLastPathSegment := func(url string) string {
			// 确保URL不以/结尾
			url = strings.TrimSuffix(url, "/")

			// 检查是否是有效的URL格式
			hasProtocol := strings.Contains(url, "://")

			// 分割URL
			parts := strings.Split(url, "/")
			if len(parts) <= 3 && hasProtocol {
				// URL格式protocol://host protocol://host/，保持不
				return url
			}

			// 移除最后一个路径段
			return strings.Join(parts[:len(parts)-1], "/")
		}

		// 如果无法从清单中获取远程URL或者URL不是有效的协议格式，则回退到从配置中获取的方法
		if !(strings.HasPrefix(baseURL, "ssh://") || strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://")) {
			// 首先检e.config 是否已初始化
			if e.config != nil && e.config.ManifestURL != "" {
				cfg = e.config
				manifestURL = e.config.ManifestURL
				if e.options != nil && e.options.Verbose && e.logger != nil {
					e.logger.Debug("使用已加载的配置，ManifestURL: %s", manifestURL)
				}
			} else {
				// 如果 e.config 为空ManifestURL 为空，尝试从文件加载配置
				var err error
				cfg, err = config.Load()
				if err == nil && cfg != nil {
					// 更新 Engine 的配
					e.config = cfg
					manifestURL = cfg.ManifestURL
					if e.options != nil && e.options.Verbose && e.logger != nil {
						e.logger.Debug("已从文件加载配置，ManifestURL: %s", manifestURL)
					}
				} else {
					// 记录错误日志
					if e.logger != nil {
						e.logger.Error("无法从文件加载配 %v", err)
					}
					// 尝试直接.repo/config.json 文件读取
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
						e.logger.Debug("config.json文件不存 %v", statErr)
					}
				}
			}

			// 如果成功获取到ManifestURL，解析相对路
			if manifestURL != "" {
				// 如果cfg为空，创建一个临时配置对
				if cfg == nil {
					cfg = &config.Config{ManifestURL: manifestURL}
				}

				// 安全地调ExtractBaseURLFromManifestURL 方法
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

		// 如果成功获取到baseURL，处理相对路
		if baseURL != "" {
			// 确保baseURL不以/结尾
			baseURL = strings.TrimSuffix(baseURL, "/")

			// 处理不同类型的相对路
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

				// 从baseURL中移除相应数量的路径
				tempBaseURL := baseURL
				for i := 0; i < count; i++ {
					tempBaseURL = trimLastPathSegment(tempBaseURL)
				}

				// 获取剩余路径并拼
				if tempURL == "" {
					// 如果只有../没有后续路径，直接拼接项目名
					remoteURL = tempBaseURL + "/" + p.Name
				} else {
					// 如果有后续路径，拼接后续路径和项目名
					remoteURL = tempBaseURL + "/" + tempURL + p.Name
				}
			} else if strings.HasPrefix(remoteURL, "./") {
				// 处理"./"开头的路径
				// 移除baseURL最后一个路径段
				baseURL = trimLastPathSegment(baseURL)

				// 获取./后面的路
				relPath := strings.TrimPrefix(remoteURL, "./")
				if relPath == "" {
					remoteURL = baseURL + "/" + p.Name
				} else {
					remoteURL = baseURL + "/" + relPath + p.Name
				}
			}

			if e.options != nil && e.options.Verbose && e.logger != nil {
				e.logger.Debug("将相对路%s 转换为远URL: %s", p.RemoteURL, remoteURL)
			}
		}
	}

	return remoteURL
}

// fetchProject 执行单个项目的网络同
func (e *Engine) fetchProject(p *project.Project) error {
	// 输出详细日志，显示实际使用的远程 URL
	if e.options.Verbose {
		e.logger.Debug("正在获取项目 %s，原始远URL: %s", p.Name, p.RemoteURL)
	}

	// 解析远程URL
	remoteURL := e.resolveRemoteURL(p)
	// 更新项目RemoteURL 为解析后URL
	p.RemoteURL = remoteURL

	// 执行 Git 操作
	// 检查远程仓库是否存
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

	// 检查是否为镜像模式
	isMirror := false
	if e.options.Config != nil && e.options.Config.Mirror {
		isMirror = true
		// 镜像模式下添加 --prune 参数，确保删除远程已经不存在的引用
		args = append(args, "--prune")
		if !e.options.Quiet && e.options.Verbose {
			e.logger.Debug("项目 %s 使用镜像模式获取，添加 --prune 参数", p.Name)
		}
	}
	e.logger.Debug("项目镜像模式 %s ", isMirror)

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
			e.logger.Info("正在重试获取项目 %s (第%d 次尝试，将在%v 后重试)",
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
			// 成功获取，跳出重试循
			break
		}

		// 如果已经达到最大重试次数，则返回错
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

	// 如果启用LFS，执LFS 拉取
	if e.options.GitLFS {
		if err := e.pullLFS(p); err != nil {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "lfs_pull",
				Err:         err,
			}
		}
	}

	// 在网络操作中不处理 linkfile copyfile，这将在本地操作中处理
	// 这样可以避免重复处理，并确保在完整的同步流程中只处理一次

	return nil
}

// cloneProject 克隆单个项目
func (e *Engine) cloneProject(p *project.Project) error {
	// 解析远程URL
	remoteURL := e.resolveRemoteURL(p)
	// 更新项目RemoteURL 为解析后URL
	p.RemoteURL = remoteURL

	// 确保RemoteName有值
	if p.RemoteName == "" {
		p.RemoteName = "origin"
		if e.options.Verbose {
			e.logger.Debug("项目 %s 未指定远程名称，使用默认值 'origin'", p.Name)
		}
	}

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

	// 添加 --origin 参数，指定远程名称
	args = append(args, "--origin", p.RemoteName)
	if e.options.Verbose {
		e.logger.Debug("项目 %s 克隆时指定远程名称: %s", p.Name, p.RemoteName)
	}

	// 检查是否为镜像模式
	isMirror := false
	if e.options.Config != nil && e.options.Config.Mirror {
		isMirror = true
		args = append(args, "--mirror")
		if !e.options.Quiet {
			e.logger.Info("项目 %s 将以镜像模式克隆", p.Name)
		}
	}

	// 添加 --reference 参数，指定本地参考仓库路径
	if e.options.Reference != "" {
		// 检查参考仓库路径是否存在
		if _, err := os.Stat(e.options.Reference); err == nil {
			args = append(args, "--reference", e.options.Reference)
			if !e.options.Quiet {
				e.logger.Info("项目 %s 将使用参考仓库: %s", p.Name, e.options.Reference)
			}
		} else {
			e.logger.Warn("指定的参考仓库路径不存在: %s，将不使用参考仓库", e.options.Reference)
		}
	}

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

	// 添加分支参数，确保克隆时指定正确的分支
	// 注意：镜像模式下不需要指定分支，因为镜像会克隆所有分支
	if p.Revision != "" && !isMirror {
		// 处理 revision 格式，移除可能的 refs/heads/ 或 refs/tags/ 前缀
		revision := p.Revision
		if strings.HasPrefix(revision, "refs/heads/") {
			revision = strings.TrimPrefix(revision, "refs/heads/")
		} else if strings.HasPrefix(revision, "refs/tags/") {
			revision = strings.TrimPrefix(revision, "refs/tags/")
		}

		// 判断 revision 是否看起来像提交 ID（通常是 40 位或缩短的十六进制字符串）
		isCommitID := false
		if len(revision) >= 7 && len(revision) <= 40 {
			isCommitID = true
			for _, c := range revision {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					isCommitID = false
					break
				}
			}
		}

		// 只有当 revision 不是提交 ID 时才使用 --branch 参数
		// 对于提交 ID，我们将在克隆后执行检出操作
		if !isCommitID {
			args = append(args, "--branch", revision)
			if !e.options.Quiet {
				e.logger.Info("克隆项目 %s 时指定分支: %s", p.Name, revision)
			}
		} else if !e.options.Quiet {
			e.logger.Info("项目 %s 的 revision 看起来像提交 ID: %s，将在克隆后检出", p.Name, revision)
		}
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
			e.logger.Info("正在重试克隆项目 %s (第%d 次尝试，将在%v 后重试)",
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

	// 克隆成功后，确保检出正确的分支（仅在非镜像模式下）
	if !e.options.NetworkOnly && p.Revision != "" && !isMirror {
		if !e.options.Quiet && e.options.Verbose {
			e.logger.Info("确保项目 %s 检出到正确的版本: %s", p.Name, p.Revision)
		}

		// 执行检出操作
		if err := e.checkoutProject(p); err != nil {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "post_clone_checkout",
				Err:         err,
				Timestamp:   time.Now(),
			}
		}
	}

	// 如果启用LFS，执行LFS 拉取（仅在非镜像模式下）
	if e.options.GitLFS && !isMirror {
		if err := e.pullLFS(p); err != nil {
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "lfs_pull",
				Err:         err,
			}
		}
	}

	// 记录克隆完成日志
	e.logger.Debug("项目 %s 克隆完成", p.Name)

	// 处理 linkfile 和 copyfile（仅在非NetworkOnly模式且非镜像模式下）
	if !e.options.NetworkOnly && !isMirror {
		e.logger.Info("开始处理项目 %s 的链接文件和复制文件", p.Name)
		if err := e.processLinkAndCopyFiles(p); err != nil {
			e.logger.Error("项目 %s 处理 linkfile 和 copyfile 失败: %v", p.Name, err)
			return &SyncError{
				ProjectName: p.Name,
				Phase:       "link_copy_files",
				Err:         err,
				Timestamp:   time.Now(),
			}
		}
		e.logger.Info("成功处理项目 %s 的链接文件和复制文件", p.Name)

		// 处理 submodule（如果启用）
		if err := e.updateSubmodules(p); err != nil {
			e.logger.Error("项目 %s 更新 submodule 失败: %v", p.Name, err)
			// submodule 更新失败不阻断整个同步流程，只记录错误
			if !e.options.Quiet {
				e.logger.Warn("跳过项目 %s 的 submodule 更新", p.Name)
			}
		}
	} else if isMirror {
		e.logger.Info("镜像模式，跳过处理项目 %s 的链接文件和复制文件", p.Name)
	} else {
		e.logger.Info("NetworkOnly模式，跳过处理项目 %s 的链接文件和复制文件", p.Name)
	}

	return nil
}

// checkoutProject 检出项
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
	const maxRetries = 2 // 检出操作通常不需要太多重
	var lastErr error
	var stderr bytes.Buffer

	for retryCount := 0; retryCount <= maxRetries; retryCount++ {
		// 如果不是第一次尝试，则等待一段时间后重试
		if retryCount > 0 {
			retryDelay := time.Duration(retryCount) * time.Second
			e.logger.Info("正在重试检出项目 %s 的 %s 分支 (第%d 次尝试，将在%v 后重试)",
				p.Name, p.Revision, retryCount, retryDelay)
			time.Sleep(retryDelay)

			// 清空上一次的错误输出
			stderr.Reset()

			// 如果检出失败，可能是因为有未提交的更改，尝试强制检
			if retryCount == maxRetries {
				e.logger.Info("尝试强制检出项%s", p.Name)
				// 添加 --force 参数
				forceArgs := make([]string, len(args))
				copy(forceArgs, args)
				// checkout 后插--force
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

		// 如果已经达到最大重试次数，则返回错
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

// projectExists 检查项目目录是否存
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

	// 确保RemoteName有值
	if p.RemoteName == "" {
		p.RemoteName = "origin"
	}

	// 检查项目指定的远程是否存在
	remoteExists := false
	originExists := false
	for _, r := range remotes {
		if r == p.RemoteName {
			remoteExists = true
		}
		if r == "origin" {
			originExists = true
		}
	}

	// 处理两个远程的情况：如果origin存在且不等于项目指定的远程名称，则删除origin
	if originExists && p.RemoteName != "origin" {
		if e.options.Verbose {
			e.logger.Debug("项目 %s 存在默认远程 'origin'，但项目指定的远程名称为 '%s'，将删除 'origin' 远程", p.Name, p.RemoteName)
		}
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "remove", "origin")
		if err := cmd.Run(); err != nil {
			e.logger.Warn("删除项目 %s 的 'origin' 远程失败: %v", p.Name, err)
		}
	}

	// 如果项目指定的远程不存在，添加
	if !remoteExists {
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "add", p.RemoteName, remoteURL)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("添加远程仓库失败: %w", err)
		}
		if e.options.Verbose {
			e.logger.Debug("已为项目 %s 添加远程 '%s': %s", p.Name, p.RemoteName, remoteURL)
		}
	} else {
		// 如果远程仓库已存在，更新URL
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "set-url", p.RemoteName, remoteURL)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("更新远程仓库URL失败: %w", err)
		}
		if e.options.Verbose {
			e.logger.Debug("已更新项目 %s 的远程 '%s' URL: %s", p.Name, p.RemoteName, remoteURL)
		}
	}

	// 如果是镜像模式，设置mirror=true
	if e.options.Config != nil && e.options.Config.Mirror {
		cmd = exec.Command("git", "-C", p.Worktree, "config", "--add", fmt.Sprintf("remote.%s.mirror", p.RemoteName), "true")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("设置远程仓库镜像模式失败: %w", err)
		}
		if e.options.Verbose {
			e.logger.Debug("已为项目 %s 的远程 %s 设置镜像模式", p.Name, p.RemoteName)
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

	// 确保RemoteName有值
	if p.RemoteName == "" {
		p.RemoteName = "origin"
	}

	// 检查项目指定的远程是否存在
	remotes := strings.Split(strings.TrimSpace(string(output)), "\n")
	remoteExists := false
	originExists := false
	for _, r := range remotes {
		if r == p.RemoteName {
			remoteExists = true
		}
		if r == "origin" {
			originExists = true
		}
	}

	// 处理两个远程的情况：如果origin存在且不等于项目指定的远程名称，则删除origin
	if originExists && p.RemoteName != "origin" {
		if e.options.Verbose {
			e.logger.Debug("项目 %s 存在默认远程 'origin'，但项目指定的远程名称为 '%s'，将删除 'origin' 远程", p.Name, p.RemoteName)
		}
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "remove", "origin")
		if err := cmd.Run(); err != nil {
			e.logger.Warn("删除项目 %s 的 'origin' 远程失败: %v", p.Name, err)
		}
	}

	// 如果项目指定的远程不存在，添加
	if !remoteExists {
		cmd = exec.Command("git", "-C", p.Worktree, "remote", "add", p.RemoteName, remoteURL)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("添加远程仓库失败: %w", err)
		}
		if e.options.Verbose {
			e.logger.Debug("已为项目 %s 添加远程 '%s': %s", p.Name, p.RemoteName, remoteURL)
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
			if e.options.Verbose {
				e.logger.Debug("已更新项目 %s 的远程 '%s' URL: %s", p.Name, p.RemoteName, remoteURL)
			}
		}
	}

	// 检查是否为镜像模式
	if e.options.Config != nil && e.options.Config.Mirror {
		// 为镜像仓库设置mirror=true配置
		cmd = exec.Command("git", "-C", p.Worktree, "config", "--add", fmt.Sprintf("remote.%s.mirror", p.RemoteName), "true")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("设置镜像仓库配置失败: %w", err)
		}
		if !e.options.Quiet && e.options.Verbose {
			e.logger.Debug("项目 %s 的远程 %s 已设置为镜像模式", p.Name, p.RemoteName)
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

	// 检查仓库是否使LFS
	cmd := exec.Command("git", "-C", p.Worktree, "lfs", "ls-files")
	output, err := cmd.Output()
	if err != nil {
		// 可能不是 LFS 仓库，跳
		return nil
	}

	// 如果LFS 文件，执行拉
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

// checkoutProject 执行单个项目的本地检
// checkoutProjectSimple 简单检出项
func (e *Engine) checkoutProjectSimple(p *project.Project) error {
	// 检查项目工作目录是否存
	if _, err := os.Stat(p.Worktree); os.IsNotExist(err) {
		return fmt.Errorf("project directory %q does not exist", p.Worktree)
	}

	// 实现项目本地检出逻辑
	return nil
}

// checkoutParallel 并行执行本地检
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

// processLinkAndCopyFiles 处理项目中的 linkfile copyfile
func (e *Engine) processLinkAndCopyFiles(p *project.Project) error {
	if p == nil {
		return fmt.Errorf("项目对象为空")
	}

	e.logger.Info("开始处理项目 %s 的 linkfile 和 copyfile", p.Name)

	// 首先确保 repoRoot 已正确设置，使用更可靠的初始化逻辑
	if e.repoRoot == "" {
		// 优先使用清单中的顶级目录
		if e.manifest != nil && e.manifest.Topdir != "" {
			e.repoRoot = e.manifest.Topdir
			e.logger.Info("从清单中获取仓库根目录: %s", e.repoRoot)
		} else {
			// 如果清单中没有顶级目录，尝试从当前工作目录推断
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("无法获取当前工作目录: %w", err)
			}

			// 查找顶层仓库目录
			topDir := project.FindTopLevelRepoDir(cwd)
			if topDir != "" {
				e.repoRoot = topDir
				e.logger.Info("从当前工作目录推断仓库根目录: %s", e.repoRoot)
			} else {
				// 如果找不到顶层目录，使用当前目录
				e.repoRoot = cwd
				e.logger.Info("使用当前工作目录作为仓库根目录: %s", e.repoRoot)
			}
		}
	}

	// 记录项目的 Linkfiles 和 Copyfiles 数量
	e.logger.Info("项目 %s 有 %d 个 linkfile 和 %d 个 copyfile 需要处理",
		p.Name, len(p.Linkfiles), len(p.Copyfiles))

	// 确定项目根目录
	var projectRoot string
	if filepath.IsAbs(p.Worktree) {
		// 如果工作树是绝对路径，直接使用
		projectRoot = p.Worktree
		e.logger.Debug("使用绝对路径作为项目根目录: %s", projectRoot)
	} else {
		// 如果工作树是相对路径，相对于仓库根目录
		projectRoot = filepath.Join(e.repoRoot, p.Worktree)
		e.logger.Debug("使用相对路径作为项目根目录: %s", projectRoot)
	}

	// 处理 Copyfile
	for i, cpFile := range p.Copyfiles {
		e.logger.Info("处理第 %d 个 copyfile: src=%s, dest=%s", i+1, cpFile.Src, cpFile.Dest)

		// 源文件路径处理
		var sourcePath string
		if cpFile.Src == "." {
			// 如果源是当前目录，使用项目根目录
			sourcePath = projectRoot
			e.logger.Info("源路径是当前目录，使用项目根目录: %s", sourcePath)
		} else {
			// 否则，源文件在项目内部
			sourcePath = filepath.Join(projectRoot, cpFile.Src)
			e.logger.Info("源路径: %s", sourcePath)
		}

		// 目标文件路径处理
		var destPath string
		if filepath.IsAbs(cpFile.Dest) {
			// 如果目标是绝对路径，直接使用
			destPath = cpFile.Dest
			e.logger.Info("目标路径是绝对路径: %s", destPath)
		} else {
			// 如果目标是相对路径，相对于仓库根目录
			destPath = filepath.Join(e.repoRoot, cpFile.Dest)
			e.logger.Info("目标路径是相对路径，相对于仓库根目录: %s", destPath)
		}

		e.logger.Info("复制文件: 从 %s 到 %s", sourcePath, destPath)

		// 检查源文件是否存在
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			e.logger.Error("源文件 %s 不存在，跳过复制", sourcePath)
			continue
		}

		// 读取源文件
		input, err := os.ReadFile(sourcePath)
		if err != nil {
			e.logger.Error("读取源文件 %s 失败: %v", sourcePath, err)
			return fmt.Errorf("读取源文件 %s 失败: %w", sourcePath, err)
		}

		// 确保目标目录存在
		destDir := filepath.Dir(destPath)
		e.logger.Info("创建目标目录: %s", destDir)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			e.logger.Error("创建目标目录 %s 失败: %v", destDir, err)
			return fmt.Errorf("创建目标目录 %s 失败: %w", destDir, err)
		}

		// 写入目标文件
		if err := os.WriteFile(destPath, input, 0644); err != nil {
			e.logger.Error("写入目标文件 %s 失败: %v", destPath, err)
			return fmt.Errorf("写入目标文件 %s 失败: %w", destPath, err)
		}

		e.logger.Info("成功复制文件: 从 %s 到 %s", sourcePath, destPath)
	}

	// 处理 Linkfile
	for i, lnFile := range p.Linkfiles {
		e.logger.Info("处理第 %d 个 linkfile: src=%s, dest=%s", i+1, lnFile.Src, lnFile.Dest)

		// 源文件路径处理
		var targetPath string
		if lnFile.Src == "." {
			// 如果源是当前目录，使用项目根目录
			targetPath = projectRoot
			e.logger.Info("链接源路径是当前目录，使用项目根目录: %s", targetPath)
		} else {
			// 否则，源文件在项目内部
			targetPath = filepath.Join(projectRoot, lnFile.Src)
			e.logger.Info("链接源路径: %s", targetPath)
		}

		// 检查源路径是否存在
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			e.logger.Error("链接源路径 %s 不存在，跳过创建链接", targetPath)
			continue
		}

		// 链接文件路径处理
		var linkPath string
		if filepath.IsAbs(lnFile.Dest) {
			// 如果目标是绝对路径，直接使用
			linkPath = lnFile.Dest
			e.logger.Info("链接目标路径是绝对路径: %s", linkPath)
		} else {
			// 如果目标是相对路径，相对于仓库根目录
			linkPath = filepath.Join(e.repoRoot, lnFile.Dest)
			e.logger.Info("链接目标路径是相对路径，相对于仓库根目录: %s", linkPath)
		}

		e.logger.Info("创建链接: 从 %s 指向 %s", linkPath, targetPath)

		// 创建链接前，确保目标目录存在
		linkDir := filepath.Dir(linkPath)
		e.logger.Info("创建链接的目标目录: %s", linkDir)
		if err := os.MkdirAll(linkDir, 0755); err != nil {
			e.logger.Error("创建链接的目标目录 %s 失败: %v", linkDir, err)
			return fmt.Errorf("创建链接的目标目录 %s 失败: %w", linkDir, err)
		}

		// 如果链接已存在，先删除
		if _, err := os.Lstat(linkPath); err == nil {
			e.logger.Info("链接 %s 已存在，将删除", linkPath)
			if err := os.Remove(linkPath); err != nil {
				e.logger.Error("删除已存在的链接 %s 失败: %v", linkPath, err)
				return fmt.Errorf("删除已存在的链接 %s 失败: %w", linkPath, err)
			}
		}

		// 计算相对路径
		linkDir = filepath.Dir(linkPath) // 重新获取，确保准确性
		relTargetPath, err := filepath.Rel(linkDir, targetPath)
		if err != nil {
			// 如果无法计算相对路径，则直接使用绝对路径
			relTargetPath = targetPath
			e.logger.Info("无法计算相对路径，将为链接 %s 使用绝对目标路径 %s: %v", linkPath, targetPath, err)
		} else {
			e.logger.Info("计算得到相对路径: %s", relTargetPath)
		}

		// 创建符号链接
		e.logger.Info("创建符号链接: %s -> %s", linkPath, relTargetPath)
		if err := os.Symlink(relTargetPath, linkPath); err != nil {
			e.logger.Error("创建符号链接从 %s 到 %s 失败: %v", linkPath, relTargetPath, err)
			return fmt.Errorf("创建符号链接从 %s 到 %s 失败: %w", linkPath, relTargetPath, err)
		}

		e.logger.Info("成功创建链接: %s -> %s", linkPath, relTargetPath)
	}

	return nil
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
	e.logger.Debug("同步引擎资源已清理完成")
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

		// 按照反向顺序，先删除子文件夹再删除父文件
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
						return fmt.Errorf("删除工作%s 失败: %w", path, err)
					}
				}
			}
		}
	}

	// 排序并写入新的项目列
	sort.Strings(newProjectPaths)
	if err := os.WriteFile(filePath, []byte(strings.Join(newProjectPaths, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("写入项目列表失败: %w", err)
	}

	return nil
}

// updateCopyLinkfileList 更新复制和链接文件列
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

// SetSilentMode 设置引擎的静默模
func (e *Engine) SetSilentMode(silent bool) {
	// 根据静默模式设置日志级别或其他相关配
	// 这里可以根据实际需求实现具体逻辑
}

// Run 执行同步操作
func (e *Engine) Run() error {
	// 初始化项目列
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

// updateSubmodules 更新项目的 submodule
func (e *Engine) updateSubmodules(p *project.Project) error {
	// 检查是否启用 submodule 功能
	if !e.shouldUpdateSubmodules() {
		if e.options.Verbose {
			e.logger.Debug("项目 %s: submodule 功能未启用，跳过", p.Name)
		}
		return nil
	}

	// 检查项目是否包含 .gitmodules 文件
	gitmodulesPath := filepath.Join(p.Worktree, ".gitmodules")
	if _, err := os.Stat(gitmodulesPath); os.IsNotExist(err) {
		// 没有 .gitmodules 文件，跳过
		if e.options.Verbose {
			e.logger.Debug("项目 %s 不包含 submodule", p.Name)
		}
		return nil
	}

	if !e.options.Quiet {
		e.logger.Info("正在更新项目 %s 的 submodule...", p.Name)
	}

	// 执行 git submodule update --init --recursive
	args := []string{"-C", p.Worktree, "submodule", "update", "--init", "--recursive"}

	// 如果启用了 Quiet 模式，添加 --quiet 参数
	if e.options.Quiet {
		args = append(args, "--quiet")
	}

	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// submodule 更新失败
		errorMsg := stderr.String()
		if errorMsg == "" {
			errorMsg = err.Error()
		}
		return fmt.Errorf("git submodule update 失败: %s", errorMsg)
	}

	if !e.options.Quiet {
		e.logger.Info("项目 %s 的 submodule 更新成功", p.Name)
	}

	return nil
}

// shouldUpdateSubmodules 判断是否应该更新 submodule
func (e *Engine) shouldUpdateSubmodules() bool {
	// 优先检查命令行参数
	if e.options.FetchSubmodules {
		return true
	}

	// 检查配置文件
	if e.options.Config != nil && e.options.Config.Submodules {
		return true
	}

	return false
}
