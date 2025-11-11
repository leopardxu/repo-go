package repo_sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

	// 使用goroutine池控制并发
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(e.options.Jobs)

	// 如果设置了FailFast选项，则在第一个错误发生时停止
	var errMutex sync.Mutex
	var firstError error

	var wg sync.WaitGroup
	for _, p := range e.projects {
		p := p
		wg.Add(1)
		g.Go(func() error {
			defer wg.Done()

			// 如果设置了FailFast选项并且已经有错误，则跳过此项目
			if e.options.FailFast {
				errMutex.Lock()
				if firstError != nil {
					errMutex.Unlock()
					return nil
				}
				errMutex.Unlock()
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				err := e.syncProject(p)

				// 如果发生错误且设置了FailFast选项，记录第一个错误
				if err != nil && e.options.FailFast {
					errMutex.Lock()
					if firstError == nil {
						firstError = err
					}
					errMutex.Unlock()
				}

				// 即使有错误也继续同步其他项目，不中断整个过程
				return err
			}
		})
	}

	wg.Wait()
	err := g.Wait()

	// 显示错误摘要
	if len(e.errResults) > 0 {
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

		// 打印错误摘要
		fmt.Printf("\n同步过程中发生了 %d 个错误\n", len(e.errResults))

		// 先打印错误类型统计
		fmt.Println("错误类型统计:")
		for errType, count := range errorTypes {
			fmt.Printf("  %s: %d 个\n", errType, count)
		}

		// 再打印详细错误信息
		fmt.Println("\n详细错误信息:")
		for i, errMsg := range e.errResults {
			fmt.Printf("错误 %d: %s\n", i+1, errMsg)

			// 对于exit status 128错误，提供额外的诊断信息
			if strings.Contains(errMsg, "exit status 128") {
				fmt.Println("  可能的原因:")
				if strings.Contains(errMsg, "does not appear to be a git repository") {
					fmt.Println("    - 远程仓库路径不正确或不是有效的Git仓库")
				} else if strings.Contains(errMsg, "repository not found") || strings.Contains(errMsg, "not found") {
					fmt.Println("    - 远程仓库不存在，请检查URL是否正确")
				} else if strings.Contains(errMsg, "authentication failed") || strings.Contains(errMsg, "could not read Username") {
					fmt.Println("    - 认证失败，请检查您的凭据或确保有访问权限")
				} else if strings.Contains(errMsg, "unable to access") || strings.Contains(errMsg, "Could not resolve host") {
					fmt.Println("    - 网络连接问题，无法访问远程仓库")
				} else if strings.Contains(errMsg, "Permission denied") {
					fmt.Println("    - 权限被拒绝，请检查您的SSH密钥或访问权限")
				} else {
					fmt.Println("    - Git命令执行失败，可能是权限问题、网络问题或仓库配置错误")
				}

				fmt.Println("  建议解决方案:")
				fmt.Println("    - 检查网络连接")
				fmt.Println("    - 验证远程仓库URL是否正确")
				fmt.Println("    - 确认您有访问权限")
				fmt.Println("    - 尝试增加重试次数 (--retry-fetches)")
				fmt.Println("    - 使用 --verbose 选项获取更详细的错误信息")
			}
		}

		return fmt.Errorf("同步失败: 同步过程中发生了 %d 个错误", len(e.errResults))
	}

	return err
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

	// 检查清单是否有
	if m == nil || len(m.Projects) == 0 {
		return fmt.Errorf("清单文件无效或不包含任何项目")
	}

	e.manifest = m
	return nil
}

// syncProjectImpl 同步单个项目的实
func (e *Engine) syncProjectImpl(p *project.Project) error {
	// 检查并设置remote信息
	if p.References != "" {
		// 解析references配置
		refParts := strings.Split(p.References, ":")
		if len(refParts) != 2 {
			return fmt.Errorf("项目 %s 的references格式无效，应remote:refs'格式", p.Name)
		}

		// 设置remote和refs
		p.Remote = refParts[0]
		p.RemoteName = refParts[0]
		p.RemoteURL, _ = e.manifest.GetRemoteURL(p.Remote)

		// 更新revision为refs
		p.Revision = refParts[1]
	}
	if p.Remote == "" {
		// 设置默认remote
		p.Remote = e.manifest.Default.Remote
	}

	// analyzeGitError 分析Git错误并提供详细信
	// 检查项目目录是否存
	worktreeExists := false
	if _, err := os.Stat(p.Worktree); err == nil {
		worktreeExists = true
		// 检查是否已经是一个有效的git仓库
		gitDirPath := filepath.Join(p.Worktree, ".git")
		if _, err := os.Stat(gitDirPath); err == nil {
			// 目录已存在且是一个git仓库，跳过克隆步
			if !e.options.Quiet && e.options.Verbose {
				fmt.Printf("项目 %s 目录已存在且是一个git仓库，跳过克隆步骤\n", p.Name)
			}
			// 继续执行后续的fetch和checkout操作
			goto SKIP_CLONE
		}
	}

	if !worktreeExists {
		// 创建项目目录
		if err := os.MkdirAll(filepath.Dir(p.Worktree), 0755); err != nil {
			return fmt.Errorf("创建项目目录失败 %s: %w", p.Name, err)
		}

		// 检查RemoteURL是否为空
		if p.RemoteURL == "" {
			return fmt.Errorf("克隆项目 %s 失败: 远程URL未设置", p.Name)
		}

		// 验证remote URL格式
		if p.RemoteURL == "" {
			return fmt.Errorf("克隆项目 %s 失败: 远程URL为空", p.Name)
		}

		// 检查URL是否包含非法字符
		if strings.ContainsAny(p.RemoteURL, " \t\n\r") {
			return fmt.Errorf("克隆项目 %s 失败: 远程URL包含空白字符", p.Name)
		}

		// 检查URL协议格式
		validProtocol := strings.HasPrefix(p.RemoteURL, "http") ||
			strings.HasPrefix(p.RemoteURL, "https") ||
			strings.HasPrefix(p.RemoteURL, "git@") ||
			strings.HasPrefix(p.RemoteURL, "ssh://") ||
			strings.HasPrefix(p.RemoteURL, "/") ||
			strings.HasPrefix(p.RemoteURL, "file://") ||
			strings.HasPrefix(p.RemoteURL, "./") ||
			strings.HasPrefix(p.RemoteURL, "../")

		if !validProtocol {
			return fmt.Errorf("克隆项目 %s 失败: 远程URL格式无效 %s (支持的协 http, https, git@, ssh://, file://, /, ./, ../)", p.Name, p.RemoteURL)
		}

		// 规范化URL格式
		if strings.HasPrefix(p.RemoteURL, "./") || strings.HasPrefix(p.RemoteURL, "../") || strings.HasPrefix(p.RemoteURL, "/") {
			cwd, err := os.Getwd()
			if err == nil {
				// 查找顶层仓库目录
				topDir := project.FindTopLevelRepoDir(cwd)
				if topDir == "" {
					topDir = cwd // 如果找不到顶层目录，使用当前目录
				}
				if !e.options.Quiet {
					fmt.Printf("规范化后的URL: %s\n", p.RemoteURL)
				}
			}
		}

		// 克隆项目
		if !e.options.Quiet {
			fmt.Printf("正在克隆缺失项目: %s\n", p.Name)
			// 只在详细模式下输出URL信息
			if e.options.Verbose {
				fmt.Printf("使用URL: %s\n", p.RemoteURL)
			}
		}

		// 使用 Engine cloneProject 方法来确保调resolveRemoteURL
		cloneErr := e.cloneProject(p)
		if cloneErr == nil {
			// 克隆成功，跳过后续重试逻辑
			if !e.options.Quiet {
				fmt.Printf("成功克隆项目: %s\n", p.Name)
			}
			goto SKIP_CLONE
		}

		// 如果 cloneProject 失败，回退到原有的重试逻辑
		// 增强的克隆重试逻辑
		maxRetries := e.options.RetryFetches
		if maxRetries <= 0 {
			maxRetries = 3 // 默认重试3
		}

		// 使用指数退避策
		baseDelay := 2 * time.Second

		for i := 0; i < maxRetries; i++ {
			// 检查上下文是否已取
			select {
			case <-e.ctx.Done():
				return fmt.Errorf("克隆项目 %s 取消: %w", p.Name, e.ctx.Err())
			default:
			}

			// 使用 Engine cloneProject 方法来确保调resolveRemoteURL
			cloneErr = e.cloneProject(p)

			if cloneErr == nil {
				break
			}

			// 分析错误类型，决定是否重
			shouldRetry := false
			retryDelay := time.Duration(1<<uint(i)) * baseDelay // 指数退

			// 检查是否为网络错误或临时错
			if strings.Contains(cloneErr.Error(), "fatal: unable to access") ||
				strings.Contains(cloneErr.Error(), "Could not resolve host") ||
				strings.Contains(cloneErr.Error(), "timed out") ||
				strings.Contains(cloneErr.Error(), "connection refused") ||
				strings.Contains(cloneErr.Error(), "temporarily unavailable") {
				shouldRetry = true
			} else if strings.Contains(cloneErr.Error(), "exit status 128") {
				// 对于exit status 128错误，需要进一步分
				if strings.Contains(cloneErr.Error(), "already exists") {
					// 目录已存在，检查是否是git仓库
					gitDirPath := filepath.Join(p.Worktree, ".git")
					if _, err := os.Stat(gitDirPath); err == nil {
						// 目录已存在且是一个git仓库，认为克隆成
						if !e.options.Quiet {
							fmt.Printf("项目目录 %s 已存在且是一个git仓库，视为克隆成功\n", p.Worktree)
						}
						cloneErr = nil
						break
					}

					// 目录存在但不是git仓库，尝试移除后重试
					if e.options.ForceSync && i == 0 { // 只在第一次尝试时执行
						if !e.options.Quiet {
							fmt.Printf("项目目录 %s 已存在但不是git仓库，尝试移除后重新克隆...\n", p.Worktree)
						}
						// 尝试移除目录
						// 检查是否为镜像模式
						isMirror := e.options.Config != nil && e.options.Config.Mirror

						if isMirror {
							// 对于镜像模式，项目路径是根据远程URL确定的，可能不在.repo目录下
							// 我们只需要确保路径是安全的，不删除系统目录
							absWorktree, absErr := filepath.Abs(p.Worktree)
							if absErr != nil {
								return fmt.Errorf("无法获取工作目录绝对路径: %w", absErr)
							}

							// 获取当前工作目录
							currentDir, dirErr := os.Getwd()
							if dirErr != nil {
								return fmt.Errorf("无法获取当前工作目录: %w", dirErr)
							}

							// 绝对禁止删除系统关键目录
							// 检查是否为危险路径
							if p.Worktree == "." || p.Worktree == ".." ||
								strings.HasPrefix(p.Worktree, "../") || strings.HasPrefix(p.Worktree, "..\\") ||
								absWorktree == "/" || absWorktree == "\\" ||
								filepath.VolumeName(absWorktree) == absWorktree { // Windows根目录
								// 路径绝对不安全，拒绝删除
								return fmt.Errorf("工作目录 %s 是系统关键目录，绝对禁止删除", p.Worktree)
							}

							// 检查路径是否在.repo目录下或当前目录下
							// 只有在这两个目录下的路径才允许删除
							// 特殊处理：如果路径是.repo目录本身或其子目录，则允许删除
							inRepoDir := strings.HasPrefix(absWorktree, e.repoRoot)
							inCurrentDir := strings.HasPrefix(absWorktree, currentDir)
							isRepoSubDir := absWorktree == e.repoRoot || strings.HasPrefix(absWorktree, e.repoRoot+string(filepath.Separator))

							if !inRepoDir && !inCurrentDir && !isRepoSubDir {
								// 路径不在允许的范围内，拒绝删除
								return fmt.Errorf("工作目录 %s 不在允许的范围内（.repo目录或当前目录），拒绝删除", p.Worktree)
							}
						} else {
							// 非镜像模式下，保持原有的安全检查
							// 安全检查：确保要删除的目录在repo根目录下
							repoRoot, repoErr := config.GetRepoRoot()
							if repoErr != nil {
								return fmt.Errorf("无法获取repo根目录: %w", repoErr)
							}

							// 检查工作目录是否在repo根目录下
							absWorktree, absErr := filepath.Abs(p.Worktree)
							if absErr != nil {
								return fmt.Errorf("无法获取工作目录绝对路径: %w", absErr)
							}

							relPath, relErr := filepath.Rel(repoRoot, absWorktree)
							if relErr != nil || strings.HasPrefix(relPath, "..") {
								// 目录不在repo根目录下，拒绝删除
								return fmt.Errorf("工作目录 %s 不在repo根目录下，拒绝删除", p.Worktree)
							}
						}

						if err := os.RemoveAll(p.Worktree); err == nil {
							// 移除成功，创建父目录
							os.MkdirAll(filepath.Dir(p.Worktree), 0755)
							shouldRetry = true
						} else {
							// 移除失败，不再重
							shouldRetry = false
						}
					} else {
						// 没有设置ForceSync，不再重
						shouldRetry = false
					}
				} else if strings.Contains(cloneErr.Error(), "does not appear to be a git repository") ||
					strings.Contains(cloneErr.Error(), "repository not found") ||
					strings.Contains(cloneErr.Error(), "authentication failed") {
					// 这些是不太可能通过重试解决的错
					shouldRetry = false
				} else {
					// 其他exit status 128错误可能是临时的，尝试重
					shouldRetry = true
				}
			}

			// 如果是最后一次尝试，不管什么错误都重试
			if i == maxRetries-1 {
				shouldRetry = true
			}

			if !shouldRetry {
				break
			}

			if !e.options.Quiet {
				if e.options.Verbose {
					fmt.Printf("克隆项目 %s %d 次尝试失 %v\n原因: %s\n将在 %s 后重..\n",
						p.Name, i+1, cloneErr, analyzeGitError(cloneErr.Error()), retryDelay)
				} else {
					fmt.Printf("克隆项目 %s %d 次尝试失败，将在 %s 后重..\n",
						p.Name, i+1, retryDelay)
				}
			}

			time.Sleep(retryDelay)
		}

		if cloneErr != nil {
			// 再次检查目录是否存在且是git仓库（可能在重试过程中被其他进程创建
			gitDirPath := filepath.Join(p.Worktree, ".git")
			if _, err := os.Stat(gitDirPath); err == nil {
				// 目录已存在且是一个git仓库，认为克隆成
				if !e.options.Quiet {
					fmt.Printf("项目目录 %s 已存在且是一个git仓库，视为克隆成功\n", p.Worktree)
				}
				// 不记录错误，继续执行
				if !e.options.Quiet {
					fmt.Printf("成功克隆项目: %s\n", p.Name)
				}
				return nil
			}

			// 记录详细的错误信
			var errorMsg string
			errorDetails := analyzeGitError(cloneErr.Error())

			if e.options.Verbose {
				// 详细模式下记录完整错误信
				errorMsg = fmt.Sprintf("克隆项目 %s 失败: %v\n远程URL: %s\n分支/修订版本: %s\n错误详情: %s\n重试次数: %d",
					p.Name, cloneErr, p.RemoteURL, p.Revision, errorDetails, maxRetries)
			}

			// 添加到错误结果列表（使用互斥锁保护）
			e.fetchTimesLock.Lock()
			e.errResults = append(e.errResults, errorMsg)
			e.fetchTimesLock.Unlock()

			return fmt.Errorf("克隆项目 %s 失败: %w", p.Name, cloneErr)
		}

		if !e.options.Quiet {
			fmt.Printf("成功克隆项目: %s\n", p.Name)
		}
		return nil
	}

SKIP_CLONE:
	// 如果项目目录已存在，执行同步操作
	// 如果不是只本地操作，执行网络同步
	if !e.options.LocalOnly {
		if !e.options.Quiet && e.options.Verbose {
			fmt.Printf("正在获取项目更新: %s\n", p.Name)
		}

		// 增强的重试逻辑
		var fetchErr error
		maxRetries := e.options.RetryFetches
		if maxRetries <= 0 {
			maxRetries = 3 // 默认重试3
		}

		// 使用指数退避策
		baseDelay := 2 * time.Second

		for i := 0; i < maxRetries; i++ {
			// 检查远程仓库URL和名称是否有效
			if p.RemoteURL == "" {
				fetchErr = fmt.Errorf("远程URL未设置")
				break
			}

			if p.RemoteName == "" {
				p.RemoteName = "origin" // 使用默认远程名称
				if !e.options.Quiet && e.options.Verbose {
					fmt.Printf("项目 %s 的远程名称未设置，使用默认名'origin'\n", p.Name)
				}
			}

			// 执行fetch操作
			fetchErr = p.GitRepo.Fetch(p.RemoteName, git.FetchOptions{
				Prune: e.options.Prune,
				Tags:  e.options.Tags,
			})

			if fetchErr == nil {
				break
			}

			// 分析错误类型，决定是否重
			shouldRetry := false
			retryDelay := time.Duration(1<<uint(i)) * baseDelay // 指数退

			// 检查是否为网络错误或临时错
			if strings.Contains(fetchErr.Error(), "fatal: unable to access") ||
				strings.Contains(fetchErr.Error(), "Could not resolve host") ||
				strings.Contains(fetchErr.Error(), "timed out") ||
				strings.Contains(fetchErr.Error(), "connection refused") ||
				strings.Contains(fetchErr.Error(), "temporarily unavailable") {
				shouldRetry = true
			} else if strings.Contains(fetchErr.Error(), "exit status 128") {
				// 对于exit status 128错误，需要进一步分
				if strings.Contains(fetchErr.Error(), "does not appear to be a git repository") ||
					strings.Contains(fetchErr.Error(), "repository not found") ||
					strings.Contains(fetchErr.Error(), "authentication failed") {
					// 这些是不太可能通过重试解决的错
					shouldRetry = false
				} else {
					// 其他exit status 128错误可能是临时的，尝试重
					shouldRetry = true
				}
			}

			// 如果是最后一次尝试，不管什么错误都重试
			if i == maxRetries-1 {
				shouldRetry = true
			}

			if !shouldRetry {
				break
			}

			if !e.options.Quiet {
				if e.options.Verbose {
					fmt.Printf("获取项目 %s 更新%d 次尝试失 %v\n原因: %s\n将在 %s 后重..\n",
						p.Name, i+1, fetchErr, analyzeGitError(fetchErr.Error()), retryDelay)
				} else {
					fmt.Printf("获取项目 %s 更新%d 次尝试失败，将在 %s 后重..\n",
						p.Name, i+1, retryDelay)
				}
			}

			time.Sleep(retryDelay)
		}

		if fetchErr != nil {
			// 记录详细的错误信
			var errorMsg string
			errorDetails := analyzeGitError(fetchErr.Error())

			if e.options.Verbose {
				// 详细模式下记录完整错误信
				errorMsg = fmt.Sprintf("获取项目 %s 更新失败: %v\n远程名称: %s\n远程URL: %s\n错误详情: %s\n重试次数: %d",
					p.Name, fetchErr, p.RemoteName, p.RemoteURL, errorDetails, maxRetries)
			} else {
				// 非详细模式下只记录简短错误信
				errorMsg = fmt.Sprintf("获取项目 %s 更新失败: %v", p.Name, fetchErr)
			}

			// 添加到错误结果列表（使用互斥锁保护）
			e.fetchTimesLock.Lock()
			e.errResults = append(e.errResults, errorMsg)
			e.fetchTimesLock.Unlock()

			return fmt.Errorf("获取项目 %s 更新失败: %w", p.Name, fetchErr)
		}
	}

	// 如果不是只网络操作，更新工作
	if !e.options.NetworkOnly {
		// 检查是否有本地修改
		clean, err := p.GitRepo.IsClean()
		if err != nil {
			return fmt.Errorf("检查项%s 工作区状态失 %w", p.Name, err)
		}

		// 如果有本地修改且不强制同步，报错
		if !clean && !e.options.ForceSync {
			return fmt.Errorf("项目 %s 工作区不干净，使--force-sync 覆盖本地修改", p.Name)
		}

		// 检出指定版
		if !e.options.Quiet && e.options.Verbose {
			fmt.Printf("正在检出项%s 的版%s\n", p.Name, p.Revision)
		}

		// 增强的checkout重试逻辑
		var checkoutErr error
		maxRetries := e.options.RetryFetches // 复用fetch的重试次
		if maxRetries <= 0 {
			maxRetries = 3 // 默认重试3
		}

		// 使用指数退避策
		baseDelay := 2 * time.Second

		// 检查revision是否有效
		if p.Revision == "" {
			p.Revision = "HEAD" // 使用默认分支
			if !e.options.Quiet && e.options.Verbose {
				fmt.Printf("项目 %s 的修订版本未设置，使用默认'HEAD'\n", p.Name)
			}
		}

		for i := 0; i < maxRetries; i++ {
			// 执行checkout操作
			checkoutErr = p.GitRepo.Checkout(p.Revision)
			if checkoutErr == nil {
				break
			}

			// 分析错误类型，决定是否重
			shouldRetry := false
			retryDelay := time.Duration(1<<uint(i)) * baseDelay // 指数退

			// 检查是否为可重试的错误
			if strings.Contains(checkoutErr.Error(), "exit status 128") {
				// 对于exit status 128错误，需要进一步分
				if strings.Contains(checkoutErr.Error(), "did not match any file(s) known to git") ||
					strings.Contains(checkoutErr.Error(), "unknown revision") ||
					strings.Contains(checkoutErr.Error(), "reference is not a tree") {
					// 这些是不太可能通过重试解决的错
					shouldRetry = false
				} else if strings.Contains(checkoutErr.Error(), "local changes") ||
					strings.Contains(checkoutErr.Error(), "would be overwritten") {
					// 本地修改冲突，如果设置了ForceSync，可以尝试强制检
					if e.options.ForceSync && i == 0 { // 只在第一次尝试时执行
						if !e.options.Quiet {
							fmt.Printf("检出项%s 时发现本地修改，尝试强制检..\n", p.Name)
						}
						// 先尝试重置工作区
						_, resetErr := p.GitRepo.Runner.RunInDir(p.Worktree, "reset", "--hard")
						if resetErr == nil {
							// 重置成功，继续尝试检
							shouldRetry = true
						} else {
							// 重置失败，不再重
							shouldRetry = false
						}
					} else {
						// 没有设置ForceSync，不再重
						shouldRetry = false
					}
				} else {
					// 其他exit status 128错误可能是临时的，尝试重
					shouldRetry = true
				}
			} else if strings.Contains(checkoutErr.Error(), "timeout") ||
				strings.Contains(checkoutErr.Error(), "timed out") ||
				strings.Contains(checkoutErr.Error(), "temporarily unavailable") {
				// 临时错误，可以重
				shouldRetry = true
			}

			// 如果是最后一次尝试，不管什么错误都重试
			if i == maxRetries-1 {
				shouldRetry = true
			}

			if !shouldRetry {
				break
			}

			if !e.options.Quiet {
				if e.options.Verbose {
					fmt.Printf("检出项%s 的版%s %d 次尝试失 %v\n原因: %s\n将在 %s 后重..\n",
						p.Name, p.Revision, i+1, checkoutErr, analyzeGitError(checkoutErr.Error()), retryDelay)
				} else {
					fmt.Printf("检出项%s 的版%s %d 次尝试失败，将在 %s 后重..\n",
						p.Name, p.Revision, i+1, retryDelay)
				}
			}

			time.Sleep(retryDelay)
		}

		if checkoutErr != nil {
			// 记录详细的错误信
			var errorMsg string
			errorDetails := analyzeGitError(checkoutErr.Error())

			if e.options.Verbose {
				// 详细模式下记录完整错误信
				errorMsg = fmt.Sprintf("检出项%s 的版%s 失败: %v\n错误详情: %s\n重试次数: %d",
					p.Name, p.Revision, checkoutErr, errorDetails, maxRetries)
			} else {
				// 非详细模式下只记录简短错误信
				errorMsg = fmt.Sprintf("检出项%s 失败: %v", p.Name, checkoutErr)
			}

			// 添加到错误结果列表（使用互斥锁保护）
			e.fetchTimesLock.Lock()
			e.errResults = append(e.errResults, errorMsg)
			e.fetchTimesLock.Unlock()

			return fmt.Errorf("检出项%s 的版%s 失败: %w", p.Name, p.Revision, checkoutErr)
		}
	}

	return nil
}

func analyzeGitError(errMsg string) string {
	// 分析常见的Git错误
	if strings.Contains(errMsg, "exit status 128") {
		// 处理exit status 128错误
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

	// 默认返回原始错误信息
	return "未知Git错误，请查看详细日志以获取更多信息"
}
