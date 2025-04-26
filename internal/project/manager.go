package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/git"
	"github.com/cix-code/gogo/internal/manifest"
)

// Manager 管理所有项目
type Manager struct {
	Projects  []*Project
	Config    *config.Config
	GitRunner git.Runner
}

// NewManager 创建项目管理器
// 在 NewManager 函数中，修改远程查找逻辑
func NewManager(manifest *manifest.Manifest, config *config.Config) *Manager {
	gitRunner, _ := git.NewCommandRunnerWithConfig(config)
	return &Manager{
		Config:    config,
		GitRunner: gitRunner,
	}

	// 创建项目列表
	var projects []*Project

	// 处理每个项目
	for _, p := range manifest.Projects {
		// 获取远程信息
		var remoteName string

		// 使用项目指定的远程或默认远程
		if p.Remote != "" {
			remoteName = p.Remote
		} else {
			remoteName = manifest.Default.Remote
		}

		// 只保留从config.GetRemoteURL()获取remoteURL的逻辑
		remoteURL := ""
		baseURL := config.GetRemoteURL()
		if baseURL != "" {
			remoteURL = baseURL + "/" + p.Name + ".git"
		}

		if remoteURL == "" {
			continue // 跳过找不到远程的项目
		}

		// 获取修订版本
		revision := p.Revision
		if revision == "" {
			revision = manifest.Default.Revision
		}

		// 获取项目路径
		path := p.Path
		if path == "" {
			path = p.Name
		}

		// 解析组
		var groups []string
		if p.Groups != "" {
			groups = strings.Split(p.Groups, ",")
		}

		// 创建项目
		project := NewProject(
			p.Name,
			path,
			remoteName,
			remoteURL,
			revision,
			groups,
			gitRunner,
		)

		projects = append(projects, project)
	}

	return &Manager{
		Projects:  projects,
		Config:    config,
		GitRunner: gitRunner,
	}
}

// GetProjects 获取符合条件的项目
func (m *Manager) GetProjects(groups []string) ([]*Project, error) {

	var filteredProjects []*Project
	for _, p := range m.Projects {
		if p.IsInAnyGroup(groups) {
			filteredProjects = append(filteredProjects, p)
		}
	}

	return filteredProjects, nil
}

// GetProject 获取指定项目
func (m *Manager) GetProject(name string) *Project {
	for _, p := range m.Projects {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// GetProjectsByNames 根据项目名称获取多个项目
func (m *Manager) GetProjectsByNames(names []string) ([]*Project, error) {
	var result []*Project

	for _, name := range names {
		found := false
		for _, p := range m.Projects {
			if p.Name == name || p.Path == name {
				result = append(result, p)
				found = true
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("project not found: %s", name)
		}
	}

	return result, nil
}

// ForEach 对每个项目执行操作
func (m *Manager) ForEach(fn func(*Project) error) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(m.Projects))

	for _, p := range m.Projects {
		wg.Add(1)
		go func(p *Project) {
			defer wg.Done()
			if err := fn(p); err != nil {
				errChan <- fmt.Errorf("project %s: %w", p.Name, err)
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
		return fmt.Errorf("errors in %d projects", len(errors))
	}

	return nil
}

// Sync 同步所有项目
func (m *Manager) Sync(opts SyncOptions) error {
	return m.ForEach(func(p *Project) error {
		return p.Sync(opts)
	})
}

// SyncOptions 同步选项
type SyncOptions struct {
	Force       bool
	DryRun      bool
	Quiet       bool
	Detach      bool
	Jobs        int
	Current     bool
	Depth       int  // 添加缺少的字段
	LocalOnly   bool // 添加缺少的字段
	NetworkOnly bool // 添加缺少的字段
	Prune       bool // 添加缺少的字段
	Tags        bool // 添加缺少的字段
}

// FindTopLevelRepoDir 查找包含.repo目录的顶层目录
func FindTopLevelRepoDir(startDir string) string {
	// 从当前目录开始向上查找，直到找到包含.repo目录的目录
	dir := startDir
	for {
		// 检查当前目录是否包含.repo目录
		repoDir := filepath.Join(dir, ".repo")
		if _, err := os.Stat(repoDir); err == nil {
			// 找到了.repo目录
			return dir
		}

		// 获取父目录
		parent := filepath.Dir(dir)
		if parent == dir {
			// 已经到达根目录，没有找到.repo目录
			return ""
		}
		dir = parent
	}
}
