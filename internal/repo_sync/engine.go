package repo_sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/ssh"
)

// Options 包含同步引擎的选项
// Options moved to options.go to avoid duplicate declarations

// Engine 是同步引擎
type Engine struct {
	manifestCache []byte
	projects []*project.Project
	options  *Options
	manifest *manifest.Manifest
	config   *config.Config
	sshProxy *ssh.Proxy
	
	// 同步状态
	fetchTimes     map[string]float64
	fetchTimesLock sync.Mutex
	errResults     []string
	errEvent       chan struct{}
	
	// 仓库根目录
	repoRoot string
}

// NewEngine 创建一个新的同步引擎
func NewEngine(projects []*project.Project, options *Options, manifest *manifest.Manifest, config *config.Config) *Engine {
	// 确保options.HTTPTimeout和options.Debug可用
	if options.HTTPTimeout == 0 {
		options.HTTPTimeout = 30 * time.Second
	}
	// 设置默认值
	if options.JobsNetwork <= 0 {
		options.JobsNetwork = options.Jobs
	}
	if options.JobsCheckout <= 0 {
		options.JobsCheckout = options.Jobs
	}
	
	// 获取仓库根目录
	var repoRoot string
	if len(projects) > 0 && projects[0].Worktree != "" {
		// 通过项目路径推断 repo 根目录
		repoRoot = filepath.Dir(projects[0].Worktree)
	} else if config != nil {
		// 从配置中获取
		repoRoot = config.RepoRoot
	} else {
		// 默认使用当前目录
		repoRoot, _ = os.Getwd()
	}
	
	return &Engine{
		projects:   projects,
		options:    options,
		manifest:   manifest,
		config:     config,
		fetchTimes: make(map[string]float64),
		errEvent:   make(chan struct{}, 1),
		repoRoot:   repoRoot,
	}
}

// Run 执行同步操作
func (e *Engine) Run() error {
	var err error
	
	// 初始化SSH代理
	e.sshProxy, err = ssh.NewProxy()
	if err != nil {
		return fmt.Errorf("初始化SSH代理失败: %w", err)
	}
	defer e.sshProxy.Close()
	
	// 更新项目列表
	if e.options.Prune {
		if err := e.updateProjectList(); err != nil {
			return err
		}
	}
	
	// 更新复制和链接文件列表
	if err := e.updateCopyLinkfileList(); err != nil {
		return err
	}
	
	// 处理智能同步
	if e.options.SmartSync || e.options.SmartTag != "" {
		if err := e.handleSmartSync(); err != nil {
			return err
		}
	}
	
	// 处理超级项目
	if e.options.UseSuperproject {
		_, err = e.updateProjectsRevisionId()
		if err != nil {
			return err
		}
	}
	
	// 处理HyperSync
	var hyperSyncProjects []*project.Project
	if e.options.HyperSync {
		hyperSyncProjects, err = e.getHyperSyncProjects()
		if err != nil {
			return err
		}
	}
	
	// 执行网络同步
	if err := e.fetchMain(e.projects); err != nil {
		return err
	}
	
	// 如果只执行网络同步，则返回
	if e.options.NetworkOnly {
		if len(e.errResults) > 0 {
			return fmt.Errorf("同步失败: 同步过程中发生了 %d 个错误", len(e.errResults))
		}
		return nil
	}
	
	// 执行本地检出
	if err := e.checkout(e.projects, hyperSyncProjects); err != nil {
		return err
	}
	
	// 检查是否有错误
	if len(e.errResults) > 0 {
		return fmt.Errorf("同步失败: 同步过程中发生了 %d 个错误", len(e.errResults))
	}
	
	return nil
}

// Errors 返回同步过程中收集的错误
func (e *Engine) Errors() []string {
	return e.errResults
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
func (e *Engine) reloadManifest(manifestName string, localOnly bool) error {
    if manifestName == "" {
        manifestName = e.config.ManifestName
    }
    
    // 解析清单
    parser := manifest.NewParser()
    newManifest, err := parser.ParseFromFile(manifestName)
    if err != nil {
        return fmt.Errorf("failed to parse manifest: %w", err)
    }
    
    // 更新清单
    e.manifest = newManifest
    
    // 更新项目列表 - 修复参数类型
    projects, err := project.NewManager(e.manifest, e.config).GetProjects(e.options.Groups)
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
    projects, err := project.NewManager(e.manifest, e.config).GetProjects(e.options.Groups)
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
    newManifest, err := parser.ParseFromBytes(e.manifestCache)
    if err != nil {
        return fmt.Errorf("failed to parse manifest from cache: %w", err)
    }

    // 更新引擎中的manifest
    e.manifest = newManifest

    // 重新获取项目列表
    projects, err := project.NewManager(e.manifest, e.config).GetProjects(e.options.Groups)
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