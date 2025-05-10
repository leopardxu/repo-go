package config

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cix-code/gogo/internal/logger"
)

// 包级别的日志记录器
var log logger.Logger = &logger.DefaultLogger{}

// 配置缓存
var (
	configCache *Config
	configMutex sync.RWMutex
	configLastModTime time.Time
)

// SetLogger 设置日志记录器
func SetLogger(logger logger.Logger) {
	log = logger
}

// ConfigError 表示配置操作中的错误
type ConfigError struct {
	Op   string // 操作名称
	Path string // 文件路径
	Err  error  // 原始错误
}

// Error 实现error接口
func (e *ConfigError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

// Unwrap 返回原始错误
func (e *ConfigError) Unwrap() error {
	return e.Err
}

// Config 表示repo配置
type Config struct {
	Version             int    `json:"version"`             // 配置版本号
	ManifestURL         string `json:"manifest_url"`        // 清单仓库的URL
	ManifestBranch      string `json:"manifest_branch"`     // 清单仓库的分支
	ManifestName        string `json:"manifest_name"`       // 清单文件的名称
	Groups              string `json:"groups"`              // 项目组
	Platform            string `json:"platform"`            // 平台
	Mirror              bool   `json:"mirror"`              // 是否为镜像
	Archive             bool   `json:"archive"`             // 是否为存档
	Worktree            bool   `json:"worktree"`            // 是否使用工作树
	Reference           string `json:"reference"`           // 引用
	NoSmartCache        bool   `json:"no_smart_cache"`      // 是否禁用智能缓存
	Dissociate          bool   `json:"dissociate"`          // 是否解除关联
	Depth               int    `json:"depth"`               // 克隆深度
	PartialClone        bool   `json:"partial_clone"`       // 是否部分克隆
	PartialCloneExclude string `json:"partial_clone_exclude"` // 部分克隆排除
	CloneFilter         string `json:"clone_filter"`        // 克隆过滤器
	UseSuperproject     bool   `json:"use_superproject"`    // 是否使用超级项目
	CloneBundle         bool   `json:"clone_bundle"`        // 是否使用克隆包
	GitLFS              bool   `json:"git_lfs"`             // 是否使用Git LFS
	RepoURL             string `json:"repo_url"`            // Repo URL
	RepoRev             string `json:"repo_rev"`            // Repo版本
	NoRepoVerify        bool   `json:"no_repo_verify"`      // 是否禁用Repo验证
	StandaloneManifest  bool   `json:"standalone_manifest"` // 是否为独立清单
	Submodules          bool   `json:"submodules"`          // 是否包含子模块
	CurrentBranch       bool   `json:"current_branch"`      // 是否使用当前分支
	Tags                bool   `json:"tags"`               // 是否包含标签
	ConfigName          string `json:"config_name"`         // 配置名称
	RepoRoot            string `yaml:"repo_root"`           // 仓库根目录
	DefaultRemoteURL    string `json:"default_remote_url"`   // 默认远程URL
	Verbose             bool   `json:"verbose"`             // 是否详细输出
	Quiet               bool   `json:"quiet"`               // 是否安静模式
}

// Load 加载配置
func Load() (*Config, error) {
	configPath := filepath.Join(".repo", "config.json")
	log.Debug("加载配置文件: %s", configPath)
	
	// 检查配置文件是否存在
	fileInfo, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		log.Error("配置文件不存在: %s", configPath)
		return nil, &ConfigError{Op: "load", Path: configPath, Err: fmt.Errorf("repo not initialized, run 'repo init' first")}
	}
	if err != nil {
		log.Error("访问配置文件失败: %s, %v", configPath, err)
		return nil, &ConfigError{Op: "load", Path: configPath, Err: fmt.Errorf("failed to access config file: %w", err)}
	}
	
	// 检查缓存是否有效
	configMutex.RLock()
	if configCache != nil && !fileInfo.ModTime().After(configLastModTime) {
		config := configCache
		configMutex.RUnlock()
		log.Debug("使用缓存的配置")
		return config, nil
	}
	configMutex.RUnlock()
	
	// 缓存无效，重新加载
	configMutex.Lock()
	defer configMutex.Unlock()
	
	// 再次检查，避免在获取写锁期间其他goroutine已经更新了缓存
	if configCache != nil && !fileInfo.ModTime().After(configLastModTime) {
		return configCache, nil
	}
	
	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Error("读取配置文件失败: %v", err)
		return nil, &ConfigError{Op: "read", Path: configPath, Err: err}
	}
	
	log.Debug("成功读取配置文件，大小: %d 字节", len(data))
	
	// 解析配置
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		log.Error("解析配置文件失败: %v", err)
		return nil, &ConfigError{Op: "parse", Path: configPath, Err: err}
	}
	
	// 设置默认版本
	if config.Version == 0 {
		config.Version = 1
	}
	
	// 迁移配置
	if err := migrateConfig(&config); err != nil {
		log.Error("迁移配置失败: %v", err)
		return nil, &ConfigError{Op: "migrate", Path: configPath, Err: err}
	}
	
	// 应用环境变量
	config.ApplyEnvironment()
	
	// 验证配置
	if err := config.Validate(); err != nil {
		log.Warn("配置验证警告: %v", err)
	}
	
	// 更新缓存
	configCache = &config
	configLastModTime = fileInfo.ModTime()
	
	log.Debug("成功加载配置")
	return configCache, nil
}

// Save 保存配置
func (c *Config) Save() error {
	log.Debug("保存配置")
	
	// 确保.repo目录存在
	if err := os.MkdirAll(".repo", 0755); err != nil {
		log.Error("创建.repo目录失败: %v", err)
		return &ConfigError{Op: "save", Path: ".repo", Err: fmt.Errorf("failed to create .repo directory: %w", err)}
	}
	
	// 验证配置
	if err := c.Validate(); err != nil {
		log.Warn("配置验证警告: %v", err)
	}
	
	// 确保版本号存在
	if c.Version == 0 {
		c.Version = 1
	}
	
	// 序列化配置
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		log.Error("序列化配置失败: %v", err)
		return &ConfigError{Op: "serialize", Err: err}
	}
	
	// 写入配置文件
	configPath := filepath.Join(".repo", "config.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		log.Error("写入配置文件失败: %v", err)
		return &ConfigError{Op: "write", Path: configPath, Err: err}
	}
	
	// 更新缓存
	configMutex.Lock()
	configCache = c
	fileInfo, _ := os.Stat(configPath)
	if fileInfo != nil {
		configLastModTime = fileInfo.ModTime()
	}
	configMutex.Unlock()
	
	log.Debug("配置保存成功")
	return nil
}

// GetRepoRoot 获取repo根目录
func GetRepoRoot() (string, error) {
	log.Debug("查找repo根目录")
	
	// 从当前目录开始向上查找.repo目录
	dir, err := os.Getwd()
	if err != nil {
		log.Error("获取当前目录失败: %v", err)
		return "", &ConfigError{Op: "get_repo_root", Err: fmt.Errorf("failed to get current directory: %w", err)}
	}
	
	for {
		repoDir := filepath.Join(dir, ".repo")
		if _, err := os.Stat(repoDir); err == nil {
			log.Debug("找到repo根目录: %s", dir)
			return dir, nil
		}
		
		// 到达根目录，停止查找
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	
	log.Error("未找到repo根目录")
	return "", &ConfigError{Op: "get_repo_root", Err: fmt.Errorf("not in a repo client")}
}
func (c *Config) GetRemoteURL() string {
	if c == nil {
		return ""
	}
	
	// 首先检查DefaultRemoteURL是否已设置
	if c.DefaultRemoteURL != "" {
		// 确保URL以斜杠结尾
		if !strings.HasSuffix(c.DefaultRemoteURL, "/") {
			return c.DefaultRemoteURL + "/"
		}
		return c.DefaultRemoteURL
	}
	
	// 尝试从.repo/manifest.xml解析远程URL
	manifestPath := filepath.Join(".repo", "manifest.xml")
	if _, err := os.Stat(manifestPath); err == nil {
		// 读取manifest.xml文件
		data, err := os.ReadFile(manifestPath)
		if err == nil {
			// 解析XML
			var manifest struct {
				XMLName xml.Name `xml:"manifest"`
				Remotes []struct {
					Name  string `xml:"name,attr"`
					Fetch string `xml:"fetch,attr"`
				} `xml:"remote"`
				Default struct {
					Remote string `xml:"remote,attr"`
				} `xml:"default"`
			}
			
			if err := xml.Unmarshal(data, &manifest); err == nil {
				// 获取默认远程名称
				defaultRemote := manifest.Default.Remote
				
				// 查找对应的远程URL
				for _, remote := range manifest.Remotes {
					if remote.Name == defaultRemote {
						fetch := remote.Fetch
						// 确保URL以斜杠结尾
						if !strings.HasSuffix(fetch, "/") {
							fetch += "/"
						}
						return fetch
					}
				}
				
				// 如果没有找到默认远程，但有其他远程，使用第一个
				if len(manifest.Remotes) > 0 {
					fetch := manifest.Remotes[0].Fetch
					// 确保URL以斜杠结尾
					if !strings.HasSuffix(fetch, "/") {
						fetch += "/"
					}
					return fetch
				}
			}
		}
	}
	
	// 如果无法从manifest.xml获取，尝试从.repo/config.json读取
	configPath := filepath.Join(".repo", "config.json")
	if _, err := os.Stat(configPath); err == nil {
		// 读取config.json文件
		data, err := os.ReadFile(configPath)
		if err == nil {
			// 解析JSON
			var config struct {
				ManifestURL string `json:"manifest_url"`
			}
			
			if err := json.Unmarshal(data, &config); err == nil && config.ManifestURL != "" {
				// 使用config.json中的manifest_url
				return c.ExtractBaseURLFromManifestURL(config.ManifestURL)
			}
		}
	}
	
	// 如果无法从config.json获取，尝试从当前配置的ManifestURL提取
	if c.ManifestURL == "" {
		return ""
	}
	
	// 使用提取方法从ManifestURL获取基础URL
	return c.ExtractBaseURLFromManifestURL(c.ManifestURL)
}

// ExtractBaseURLFromManifestURL 从清单URL中提取基础URL
func (c *Config) ExtractBaseURLFromManifestURL(manifestURL string) string {
	// 处理SSH URL格式: ssh://git@example.com/path/to/repo
	if strings.HasPrefix(manifestURL, "ssh://") {
		// 查找第三个斜杠的位置（ssh://后的第一个斜杠）
		parts := strings.SplitN(manifestURL, "/", 4)
		if len(parts) >= 3 {
			// 返回 ssh://hostname 部分
			return strings.Join(parts[:3], "/")
		}
	}
	
	// 处理SCP格式: git@example.com:path/to/repo
	if strings.Contains(manifestURL, "@") && strings.Contains(manifestURL, ":") {
		// 查找冒号的位置
		parts := strings.SplitN(manifestURL, ":", 2)
		if len(parts) == 2 {
			// 返回 user@hostname 部分
			return parts[0]
		}
	}
	
	// 处理HTTP/HTTPS URL
	if strings.HasPrefix(manifestURL, "http://") || strings.HasPrefix(manifestURL, "https://") {
		// 查找第三个斜杠后的位置
		parts := strings.SplitN(manifestURL, "/", 4)
		if len(parts) >= 3 {
			// 返回 protocol://hostname 部分
			return strings.Join(parts[:3], "/")
		}
	}
	
	// 无法解析的情况下返回原始URL
	return manifestURL
}

// GetProjectRemoteURL 获取项目的远程URL
func (c *Config) GetProjectRemoteURL(projectName string) string {
	if c == nil || projectName == "" {
		return ""
	}
	
	// 如果配置中有DefaultRemoteURL，使用它
	if c.DefaultRemoteURL != "" {
		baseURL := c.DefaultRemoteURL
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
		return baseURL + projectName
	}
	
	// 如果没有DefaultRemoteURL，尝试从ManifestURL提取
	if c.ManifestURL != "" {
		baseURL := c.ExtractBaseURLFromManifestURL(c.ManifestURL)
		if baseURL != "" {
			if !strings.HasSuffix(baseURL, "/") {
				baseURL += "/"
			}
			return baseURL + projectName
		}
	}
	
	return ""
}

// resolveRelativePath 解析相对路径
func resolveRelativePath(basePath, relativePath string) string {
	log.Debug("解析相对路径: basePath=%s, relativePath=%s", basePath, relativePath)
	
	// 处理空路径的情况
	if relativePath == "" {
		return basePath
	}
	
	// 如果相对路径是绝对路径，直接返回
	if filepath.IsAbs(relativePath) {
		return relativePath
	}
	
	// 规范化路径，统一使用斜杠
	basePath = filepath.ToSlash(basePath)
	relativePath = filepath.ToSlash(relativePath)
	
	// 确保basePath不以斜杠结尾
	basePath = strings.TrimSuffix(basePath, "/")
	
	// 计算绝对路径
	baseDir := filepath.Dir(basePath)
	resolvedPath := filepath.Join(baseDir, relativePath)
	
	// 规范化路径
	resolvedPath = filepath.Clean(resolvedPath)
	
	log.Debug("解析结果: %s", filepath.ToSlash(resolvedPath))
	return filepath.ToSlash(resolvedPath)
}

// ResolveRelativeURL 将相对URL解析为完整URL
func (c *Config) ResolveRelativeURL(relativeURL string) string {
    log.Debug("解析相对URL: %s", relativeURL)
    
    // 如果不是相对路径，直接返回
    if !strings.HasPrefix(relativeURL, "../") {
        return relativeURL
    }
    
    // 如果是相对路径，尝试将其转换为完整URL
    if !strings.Contains(relativeURL, "://") {
        // 从配置中获取基础URL
        baseURL := "ssh://git@gitmirror.cixtech.com/"
        if c.DefaultRemoteURL != "" {
            baseURL = c.DefaultRemoteURL
        }
        
        // 确保baseURL以/结尾
        if !strings.HasSuffix(baseURL, "/") {
            baseURL += "/"
        }
        
        // 移除相对路径前缀
        relPath := strings.TrimPrefix(relativeURL, "../")
        resolvedURL := baseURL + relPath
        log.Debug("解析结果: %s", resolvedURL)
        return resolvedURL
    }
    
    return relativeURL
}

// Validate 验证配置的完整性和正确性
func (c *Config) Validate() error {
    var errs []string
    
    // 验证必填字段
    if c.ManifestURL == "" {
        errs = append(errs, "manifest_url is required")
    }
    
    if c.ManifestName == "" {
        errs = append(errs, "manifest_name is required")
    }
    
    // 验证深度值
    if c.Depth < 0 {
        errs = append(errs, "depth must be non-negative")
    }
    
    // 验证互斥选项
    if c.Mirror && c.Archive {
        errs = append(errs, "mirror and archive options are mutually exclusive")
    }
    
    if len(errs) > 0 {
        return fmt.Errorf("配置验证失败: %v", strings.Join(errs, "; "))
    }
    
    return nil
}

// ApplyEnvironment 应用环境变量覆盖配置
func (c *Config) ApplyEnvironment() {
    log.Debug("应用环境变量覆盖配置")
    
    // 检查环境变量并覆盖配置
    if manifestURL := os.Getenv("GOGO_MANIFEST_URL"); manifestURL != "" {
        log.Debug("从环境变量设置 MANIFEST_URL: %s", manifestURL)
        c.ManifestURL = manifestURL
    }
    
    if manifestBranch := os.Getenv("GOGO_MANIFEST_BRANCH"); manifestBranch != "" {
        log.Debug("从环境变量设置 MANIFEST_BRANCH: %s", manifestBranch)
        c.ManifestBranch = manifestBranch
    }
    
    if manifestName := os.Getenv("GOGO_MANIFEST_NAME"); manifestName != "" {
        log.Debug("从环境变量设置 MANIFEST_NAME: %s", manifestName)
        c.ManifestName = manifestName
    }
    
    if groups := os.Getenv("GOGO_GROUPS"); groups != "" {
        log.Debug("从环境变量设置 GROUPS: %s", groups)
        c.Groups = groups
    }
    
    if platform := os.Getenv("GOGO_PLATFORM"); platform != "" {
        log.Debug("从环境变量设置 PLATFORM: %s", platform)
        c.Platform = platform
    }
    
    // 布尔值环境变量
    if mirror := os.Getenv("GOGO_MIRROR"); mirror == "true" {
        log.Debug("从环境变量设置 MIRROR: true")
        c.Mirror = true
    } else if mirror == "false" {
        log.Debug("从环境变量设置 MIRROR: false")
        c.Mirror = false
    }
    
    if archive := os.Getenv("GOGO_ARCHIVE"); archive == "true" {
        log.Debug("从环境变量设置 ARCHIVE: true")
        c.Archive = true
    } else if archive == "false" {
        log.Debug("从环境变量设置 ARCHIVE: false")
        c.Archive = false
    }
    
    // 整数值环境变量
    if depthStr := os.Getenv("GOGO_DEPTH"); depthStr != "" {
        if depth, err := strconv.Atoi(depthStr); err == nil {
            log.Debug("从环境变量设置 DEPTH: %d", depth)
            c.Depth = depth
        } else {
            log.Warn("无效的DEPTH环境变量值: %s", depthStr)
        }
    }
    
    // 日志级别环境变量
    if verbose := os.Getenv("GOGO_VERBOSE"); verbose == "true" {
        log.Debug("从环境变量设置 VERBOSE: true")
        c.Verbose = true
    } else if verbose == "false" {
        log.Debug("从环境变量设置 VERBOSE: false")
        c.Verbose = false
    }
    
    if quiet := os.Getenv("GOGO_QUIET"); quiet == "true" {
        log.Debug("从环境变量设置 QUIET: true")
        c.Quiet = true
    } else if quiet == "false" {
        log.Debug("从环境变量设置 QUIET: false")
        c.Quiet = false
    }
}

// migrateConfig 根据版本号迁移配置
func migrateConfig(config *Config) error {
    // 如果没有版本号，假设为版本1
    if config.Version == 0 {
        config.Version = 1
    }
    
    // 根据版本号进行迁移
    switch config.Version {
    case 1:
        // 当前版本，无需迁移
        return nil
    default:
        return fmt.Errorf("unsupported config version: %d", config.Version)
    }
}