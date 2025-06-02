package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/git"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
)

// Manager 管理项目列表
type Manager struct {
	Projects     []*Project
	ManifestURL  string
	ManifestName string
	RepoDir      string
	GitRunner    git.Runner
	mu           sync.RWMutex // 添加锁保护并发访�?}

// NewManager 创建项目管理�?func NewManager(manifestURL, manifestName, repoDir string, gitRunner git.Runner) *Manager {
	logger.Debug("创建项目管理�? manifestURL=%s, manifestName=%s, repoDir=%s", manifestURL, manifestName, repoDir)
	return &Manager{
		Projects:     make([]*Project, 0),
		ManifestURL:  manifestURL,
		ManifestName: manifestName,
		RepoDir:      repoDir,
		GitRunner:    gitRunner,
	}
}

// NewManagerFromManifest 从清单和配置创建项目管理�?func NewManagerFromManifest(m *manifest.Manifest, cfg *config.Config) *Manager {
	logger.Info("从清单创建项目管理器，清单服务器: %s", m.ManifestServer)

	// 创建一个新的Manager实例
	manager := &Manager{
		Projects:     make([]*Project, 0),
		ManifestURL:  m.ManifestServer,
		ManifestName: "default.xml", // 默认清单名称
		RepoDir:      m.RepoDir,
		GitRunner:    git.NewRunner(),
	}

	// 记录项目加载开�?	logger.Info("开始从清单加载 %d 个项�?, len(m.Projects))

	// 从清单中加载项目
	for _, p := range m.Projects {
		// 获取远程信息
		var remoteName, remoteURL string
		if p.Remote != "" {
			remoteName = p.Remote
		} else if m.Default.Remote != "" {
			remoteName = m.Default.Remote
		}

		// 查找远程配置
		for _, r := range m.Remotes {
			if r.Name == remoteName {
				remoteURL = r.Fetch
				break
			}
		}

		// 获取修订版本
		revision := p.Revision
		if revision == "" {
			revision = m.Default.Revision
		}

		// 创建项目路径
		projectPath := filepath.Join(m.RepoDir, p.Path)

		// 创建项目对象
		project := NewProject(
			p.Name,
			projectPath,
			remoteName,
			remoteURL,
			revision,
			strings.Split(p.Groups, ","),
			git.NewRunner(),
		)

		// 添加项目到管理器
		manager.AddProject(project)
	}

	logger.Info("项目管理器创建完成，共加�?%d 个项�?, len(manager.Projects))
	return manager
}

// GetProjectsInGroups 获取指定组中的项�?func (m *Manager) GetProjectsInGroups(groups []string) ([]*Project, error) {
	// 如果没有指定组，返回所有项�?	if len(groups) == 0 {
		logger.Debug("未指定项目组，返回所有项�?)
		return m.GetProjects(), nil
	}

	// 记录过滤操作
	logger.Info("过滤项目�? %v", groups)

	// 获取在指定组中的项目
	projects := m.GetProjectsInAnyGroup(groups)

	// 如果没有找到项目，返回空列表而不是错误，让调用者决定如何处�?	if len(projects) == 0 {
		logger.Warn("在指定组 %v 中未找到项目，返回空列表", groups)
	}

	logger.Info("找到 %d 个匹配项�?, len(projects))
	return projects, nil
}

// AddProject 添加项目
func (m *Manager) AddProject(p *Project) {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger.Info("添加项目: %s (路径: %s, 修订版本: %s)", p.Name, p.Path, p.Revision)
	m.Projects = append(m.Projects, p)
}

// GetProjectsByNames 根据名称列表获取项目
func (m *Manager) GetProjectsByNames(names []string) ([]*Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var projects []*Project
	for _, name := range names {
		found := false
		for _, p := range m.Projects {
			if p.Name == name {
				projects = append(projects, p)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("未找到项�? %s", name)
		}
	}

	return projects, nil
}

// GetProject 获取项目
func (m *Manager) GetProject(name string) *Project {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.Projects {
		if p.Name == name {
			return p
		}
	}

	logger.Debug("未找到项�? %s", name)
	return nil
}

// GetProjects 获取所有项�?func (m *Manager) GetProjects() []*Project {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 创建副本以避免并发修�?	projects := make([]*Project, len(m.Projects))
	copy(projects, m.Projects)

	logger.Debug("获取所有项目，�?%d �?, len(projects))
	return projects
}

// GetProjectsInGroup 获取指定组中的项�?func (m *Manager) GetProjectsInGroup(group string) []*Project {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var projects []*Project

	for _, p := range m.Projects {
		if p.IsInGroup(group) {
			projects = append(projects, p)
		}
	}

	if len(projects) > 0 {
		logger.Info("�?%s 中找�?%d 个项�?, group, len(projects))
	} else {
		logger.Debug("�?%s 中未找到项目", group)
	}
	return projects
}

// 判断是否包含所有组
func containsAll(groups []string) bool {
	for _, group := range groups {
		if group == "all" {
			return true
		}
	}
	return false
}

// GetProjectsInAnyGroup 获取在任意指定组中的项目
func (m *Manager) GetProjectsInAnyGroup(groups []string) []*Project {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(groups) == 0 || containsAll(groups) {
		// 创建副本以避免并发修�?		projects := make([]*Project, len(m.Projects))
		copy(projects, m.Projects)
		return projects
	}

	logger.Debug("获取在任意组 %v 中的项目", groups)
	var projects []*Project

	for _, p := range m.Projects {
		if p.IsInAnyGroup(groups) {
			projects = append(projects, p)
		}
	}

	logger.Debug("在指定组中找�?%d 个项�?, len(projects))
	return projects
}

// ResolveRemoteURL 解析远程URL
func (m *Manager) ResolveRemoteURL(remoteURL string) string {
	logger.Debug("解析远程URL: %s", remoteURL)

	// 如果URL为空，返回空字符�?	if remoteURL == "" {
		return ""
	}

	// 如果URL是绝对路径，直接返回
	if strings.HasPrefix(remoteURL, "http://") ||
		strings.HasPrefix(remoteURL, "https://") ||
		strings.HasPrefix(remoteURL, "git://") ||
		strings.HasPrefix(remoteURL, "ssh://") ||
		strings.HasPrefix(remoteURL, "file://") ||
		strings.Contains(remoteURL, "@") {
		return remoteURL
	}

	// 如果URL是相对路径，基于manifestURL解析
	baseURL := m.extractBaseURL(m.ManifestURL)
	if baseURL == "" {
		logger.Warn("无法�?%s 提取基础URL", m.ManifestURL)
		return remoteURL
	}

	resolvedURL := baseURL
	if !strings.HasSuffix(resolvedURL, "/") {
		resolvedURL += "/"
	}
	resolvedURL += remoteURL

	logger.Debug("解析后的URL: %s", resolvedURL)
	return resolvedURL
}

// extractBaseURL 提取基础URL
func (m *Manager) extractBaseURL(url string) string {
	logger.Debug("�?%s 提取基础URL", url)

	// 处理不同格式的URL

	// HTTP/HTTPS URL
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		// 移除最后一个路径组�?		lastSlash := strings.LastIndex(url, "/")
		if lastSlash > 8 { // 确保不是协议后的第一个斜�?			return url[:lastSlash]
		}
		return url
	}

	// SSH URL (git@github.com:user/repo.git)
	if strings.Contains(url, "@") && strings.Contains(url, ":") {
		parts := strings.Split(url, ":")
		if len(parts) == 2 {
			host := parts[0]
			path := parts[1]

			// 移除最后一个路径组�?			lastSlash := strings.LastIndex(path, "/")
			if lastSlash >= 0 {
				path = path[:lastSlash]
			} else {
				// 如果没有斜杠，可能是直接的仓库名
				path = ""
			}

			if path == "" {
				return host + ":"
			}
			return host + ":" + path
		}
	}

	// 文件URL
	if strings.HasPrefix(url, "file://") {
		path := strings.TrimPrefix(url, "file://")
		dir := filepath.Dir(path)
		return "file://" + dir
	}

	// 无法识别的URL格式
	logger.Warn("无法识别的URL格式: %s", url)
	return ""
}

// ForEach 对每个项目执行操�?func (m *Manager) ForEach(fn func(*Project) error) error {
	m.mu.RLock()
	projects := make([]*Project, len(m.Projects))
	copy(projects, m.Projects)
	m.mu.RUnlock()

	logger.Debug("�?%d 个项目执行操�?, len(projects))

	if len(projects) == 0 {
		logger.Warn("没有项目可执行操�?)
		return nil
	}

	// 创建错误通道
	errChan := make(chan error, len(projects))

	// 创建等待�?	var wg sync.WaitGroup

	// 对每个项目执行操�?	for _, p := range projects {
		wg.Add(1)
		go func(p *Project) {
			defer wg.Done()

			logger.Debug("对项�?%s 执行操作", p.Name)
			err := fn(p)
			if err != nil {
				logger.Error("项目 %s 操作失败: %v", p.Name, err)
				errChan <- fmt.Errorf("项目 %s: %w", p.Name, err)
			} else {
				logger.Debug("项目 %s 操作成功", p.Name)
			}
		}(p)
	}

	wg.Wait()
	close(errChan)

	// 收集错误
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		logger.Error("�?%d 个项目操作失�?, len(errors))
		return fmt.Errorf("�?%d 个项目操作失�?, len(errors))
	}

	logger.Debug("所有项目操作完�?)
	return nil
}

// ForEachWithJobs 使用指定数量的并发任务对每个项目执行操作
func (m *Manager) ForEachWithJobs(fn func(*Project) error, jobs int) error {
	m.mu.RLock()
	projects := make([]*Project, len(m.Projects))
	copy(projects, m.Projects)
	m.mu.RUnlock()

	logger.Debug("使用 %d 个并发任务对 %d 个项目执行操�?, jobs, len(projects))

	if len(projects) == 0 {
		logger.Warn("没有项目可执行操�?)
		return nil
	}

	// 如果jobs <= 0，使用项目数量作为并发数
	if jobs <= 0 {
		jobs = len(projects)
		logger.Debug("未指定并发数，使用项目数�?%d 作为并发�?, jobs)
	}

	// 创建任务通道
	taskChan := make(chan *Project, len(projects))

	// 创建错误通道
	errChan := make(chan error, len(projects))

	// 创建等待�?	var wg sync.WaitGroup

	// 启动工作协程
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			logger.Debug("启动工作协程 #%d", workerID)
			for p := range taskChan {
				logger.Debug("工作协程 #%d 处理项目 %s", workerID, p.Name)
				err := fn(p)
				if err != nil {
					logger.Error("项目 %s 操作失败: %v", p.Name, err)
					errChan <- fmt.Errorf("项目 %s: %w", p.Name, err)
				} else {
					logger.Debug("项目 %s 操作成功", p.Name)
				}
			}
			logger.Debug("工作协程 #%d 完成", workerID)
		}(i)
	}

	// 发送任�?	for _, p := range projects {
		taskChan <- p
	}
	close(taskChan)

	// 等待所有工作协程完�?	wg.Wait()
	close(errChan)

	// 收集错误
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		logger.Error("�?%d 个项目操作失�?, len(errors))
		return fmt.Errorf("�?%d 个项目操作失�?, len(errors))
	}

	logger.Debug("所有项目操作完�?)
	return nil
}

// Sync 同步所有项�?func (m *Manager) Sync(opts SyncOptions) error {
	logger.Info("开始同�?%d 个项�?, len(m.Projects))

	// 如果指定了并发数，使用ForEachWithJobs
	if opts.Jobs > 0 {
		logger.Debug("使用 %d 个并发任务同步项�?, opts.Jobs)
		return m.ForEachWithJobs(func(p *Project) error {
			if !opts.Quiet {
				logger.Info("同步项目 %s", p.Name)
			}
			return p.Sync(opts)
		}, opts.Jobs)
	}

	// 否则使用ForEach
	return m.ForEach(func(p *Project) error {
		if !opts.Quiet {
			logger.Info("同步项目 %s", p.Name)
		}
		return p.Sync(opts)
	})
}

// SyncOptions 同步选项
type SyncOptions struct {
	Force       bool   // 强制同步，覆盖本地修�?	DryRun      bool   // 仅显示将要执行的操作，不实际执行
	Quiet       bool   // 静默模式，减少输�?	Detach      bool   // 分离模式，不检出工作区
	Jobs        int    // 并发任务�?	Current     bool   // 仅同步当前分�?	Depth       int    // 克隆深度
	LocalOnly   bool   // 仅执行本地同�?	NetworkOnly bool   // 仅执行网络同�?	Prune       bool   // 修剪远程跟踪分支
	Tags        bool   // 获取标签
	Group       string // 指定要同步的�?	NoGC        bool   // 不执行垃圾回�?}

// FindTopLevelRepoDir 查找包含.repo目录的顶层目�?func FindTopLevelRepoDir(startDir string) string {
	logger.Debug("�?%s 开始查找顶层仓库目�?, startDir)

	// 从当前目录开始向上查找，直到找到包含.repo目录的目�?	dir := startDir
	for {
		// 检查当前目录是否包�?repo目录
		repoDir := filepath.Join(dir, ".repo")
		if _, err := os.Stat(repoDir); err == nil {
			// 找到�?repo目录
			logger.Debug("找到顶层仓库目录: %s", dir)
			return dir
		}

		// 获取父目�?		parent := filepath.Dir(dir)
		if parent == dir {
			// 已经到达根目录，没有找到.repo目录
			logger.Warn("未找到顶层仓库目�?)
			return ""
		}
		dir = parent
	}
}

// ForEachProject 对每个项目执行操作，支持并发执行
func (m *Manager) ForEachProject(fn func(*Project) error, concurrency int) error {
	projects := m.GetProjects()

	// 如果并发数为1，则串行执行
	if concurrency <= 1 {
		for _, p := range projects {
			if err := fn(p); err != nil {
				return err
			}
		}
		return nil
	}

	// 并发执行
	var wg sync.WaitGroup
	errChan := make(chan error, len(projects))
	semaphore := make(chan struct{}, concurrency)

	for _, p := range projects {
		wg.Add(1)
		go func(proj *Project) {
			defer wg.Done()

			// 获取信号�?			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := fn(proj); err != nil {
				errChan <- err
			}
		}(p)
	}

	// 等待所有任务完�?	go func() {
		wg.Wait()
		close(errChan)
	}()

	// 收集错误
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// 返回第一个错�?		return errs[0]
	}

	return nil
}

// SyncProjects 同步所有项目，支持并发
func (m *Manager) SyncProjects(opts SyncOptions, concurrency int) error {
	logger.Info("开始同�?%d 个项目，并发�? %d", len(m.Projects), concurrency)

	// 使用 ForEachProject 并发执行同步
	err := m.ForEachProject(func(p *Project) error {
		return p.Sync(opts)
	}, concurrency)

	if err != nil {
		logger.Error("项目同步过程中发生错�? %v", err)
		return err
	}

	// 同步完成后执行垃圾回�?	if !opts.NoGC {
		logger.Info("执行项目垃圾回收")
		_ = m.ForEachProject(func(p *Project) error {
			return p.GC()
		}, concurrency)
	}

	logger.Info("所有项目同步完�?)
	return nil
}

// FilterProjects 根据条件过滤项目
func (m *Manager) FilterProjects(filter func(*Project) bool) []*Project {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []*Project
	for _, p := range m.Projects {
		if filter(p) {
			filtered = append(filtered, p)
		}
	}

	return filtered
}
