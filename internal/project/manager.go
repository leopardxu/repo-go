package project

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"strings"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/git"
	"github.com/cix-code/gogo/internal/manifest"
)

// Manager 管理所有项目
type Manager struct {
	Projects []*Project
	Config   *config.Config
	GitRunner git.Runner
}

// NewManager 创建项目管理器
// 在 NewManager 函数中，修改远程查找逻辑
func NewManager(manifest *manifest.Manifest, config *config.Config) *Manager {
	gitRunner := git.NewCommandRunner()
	
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
			fmt.Printf("Project %s does not specify remote, using default remote %s\n", p.Name, remoteName)
		}
		
		// 查找远程配置
		remoteFound := false
		var remoteURL string
		
		// 首先检查是否已经在自定义属性中存储了远程URL
		if customURL, ok := p.GetCustomAttr("__remote_url"); ok && customURL != "" {
		    remoteURL = customURL
		    
		    // 检查是否是相对路径，如果是则尝试构建SSH URL
		    if strings.HasPrefix(remoteURL, "../") || strings.HasPrefix(remoteURL, "./") {
		        // 尝试从config.json获取SSH基础URL
		        cwd, err := os.Getwd()
		        if err == nil {
		            topDir := findTopLevelRepoDir(cwd)
		            if topDir != "" {
		                // 直接使用固定的SSH URL格式
		                remoteURL = fmt.Sprintf("ssh://git@gitmirror.cixtech.com/%s", p.Name)
		                fmt.Printf("Converted relative path to SSH URL: %s\n", remoteURL)
		            }
		        }
		    }
		    
		    remoteFound = true
		    fmt.Printf("Using custom remote URL for project %s: %s\n", p.Name, remoteURL)
		} else {
		    // 否则，查找远程配置并构建URL
			for _, r := range manifest.Remotes {
				if r.Name == remoteName {
					// 构建远程URL
					remoteURL = r.Fetch
					if !strings.HasSuffix(remoteURL, "/") {
						remoteURL += "/"
					}
					remoteURL += p.Name
					remoteFound = true
					fmt.Printf("Found remote %s in manifest for project %s: %s\n", remoteName, p.Name, remoteURL)
					break
				}
			}
			
			// 如果在当前manifest中找不到remote，尝试从.repo/manifests目录下查找
			if !remoteFound {
				// 获取当前工作目录
				cwd, err := os.Getwd()
				if err == nil {
					// 查找顶层仓库目录
					topDir := findTopLevelRepoDir(cwd)
					if topDir == "" {
						topDir = cwd // 如果找不到顶层目录，使用当前目录
					}
					
					fmt.Printf("Searching for remote %s in .repo/manifests for project %s\n", remoteName, p.Name)
					
					// 尝试从config.json获取manifest URL
					configFile := filepath.Join(topDir, ".repo", "manifests.git", "config")
					if _, err := os.Stat(configFile); err == nil {
						data, err := os.ReadFile(configFile)
						if err == nil {
							content := string(data)
							// 查找URL行
							urlLines := regexp.MustCompile(`(?m)^\s*url\s*=\s*(.+)$`).FindAllStringSubmatch(content, -1)
							for _, match := range urlLines {
								if len(match) > 1 {
									baseURL := match[1]
									// 如果是SSH URL，提取域名部分
									if strings.HasPrefix(baseURL, "ssh://") || strings.Contains(baseURL, "@") {
										parts := strings.Split(baseURL, ":")
										if len(parts) > 1 {
											domain := parts[0]
											if strings.Contains(domain, "@") {
												domain = strings.Split(domain, "@")[1]
											}
											// 构建SSH URL
											remoteURL = fmt.Sprintf("ssh://%s/%s", domain, p.Name)
											remoteFound = true
											fmt.Printf("Using SSH URL from config: %s\n", remoteURL)
											break
										}
									}
								}
							}
						}
					}
					
					// 如果仍未找到，尝试从manifest文件中查找
					if !remoteFound {
					    // 尝试从.repo/manifests/default.xml中查找
					    manifestsFiles := []string{
					        filepath.Join(topDir, ".repo", "manifests", "default.xml"),
					        filepath.Join(topDir, ".repo", "manifests", "manifest.xml"),
					        filepath.Join(topDir, ".repo", "manifests", "cix.xml"),
					    }
					    
					    for _, manifestFile := range manifestsFiles {
					        if _, statErr := os.Stat(manifestFile); statErr == nil {
					            fmt.Printf("Checking manifest file: %s\n", manifestFile)
					            // 文件存在，尝试解析
					            data, readErr := os.ReadFile(manifestFile)
					            if readErr == nil {
					                // 解析manifest文件
					                // 使用匿名结构体
					                var manifestObj struct {
					                    XMLName  xml.Name `xml:"manifest"`
					                    Remotes []struct {
					                        Name  string `xml:"name,attr"`
					                        Fetch string `xml:"fetch,attr"`
					                    } `xml:"remote"`
					                    Default struct {
					                        Remote   string `xml:"remote,attr"`
					                        Revision string `xml:"revision,attr"`
					                    } `xml:"default"`
					                    Projects []struct {
					                        Name     string `xml:"name,attr"`
					                        Path     string `xml:"path,attr"`
					                        Remote   string `xml:"remote,attr"`
					                        Revision string `xml:"revision,attr"`
					                        Groups   string `xml:"groups,attr"`
					                    } `xml:"project"`
					                }
					                parseErr := xml.Unmarshal(data, &manifestObj)
					                if parseErr == nil {
					                    fmt.Printf("Successfully parsed manifest file: %s\n", manifestFile)
					                    // 在解析的manifest中查找remote
					                    for _, r := range manifestObj.Remotes {
					                        fmt.Printf("Found remote in %s: %s -> %s\n", manifestFile, r.Name, r.Fetch)
					                        if r.Name == remoteName {
					                            // 构建远程URL
					                            remoteURL = r.Fetch
					                            if !strings.HasSuffix(remoteURL, "/") {
					                                remoteURL += "/"
					                            }
					                            remoteURL += p.Name
					                            remoteFound = true
					                            fmt.Printf("Found remote %s in %s for project %s: %s\n", remoteName, manifestFile, p.Name, remoteURL)
					                            break
					                        }
					                    }
					                } else {
					                    fmt.Printf("Error parsing manifest file %s: %v\n", manifestFile, parseErr)
					                }
					            } else {
					                fmt.Printf("Error reading manifest file %s: %v\n", manifestFile, readErr)
					            }
					        } else {
					            fmt.Printf("Manifest file not found: %s\n", manifestFile)
					        }
					        
					        if remoteFound {
					            break
					        }
					    }
					}
				}
			}
			
			// 如果仍然找不到远程，尝试使用一些常见的远程URL模式
			if !remoteFound {
				// 尝试一些常见的远程URL模式
				commonRemotes := map[string]string{
					"cix": "ssh://git@github.com/cix-code/",
					// 添加其他可能的远程URL前缀
				}
				
				if baseURL, ok := commonRemotes[remoteName]; ok {
					remoteURL = baseURL + p.Name
					remoteFound = true
					fmt.Printf("Using common remote pattern for %s: %s\n", remoteName, remoteURL)
				}
			}
		}
		
		if !remoteFound {
			fmt.Printf("Warning: Skipping project %s because remote %s not found\n", p.Name, remoteName)
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
	Depth       int    // 添加缺少的字段
	LocalOnly   bool   // 添加缺少的字段
	NetworkOnly bool   // 添加缺少的字段
	Prune       bool   // 添加缺少的字段
	Tags        bool   // 添加缺少的字段
}

// findTopLevelRepoDir 查找包含.repo目录的顶层目录
func findTopLevelRepoDir(startDir string) string {
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