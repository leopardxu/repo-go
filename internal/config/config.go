package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config 表示repo配置
type Config struct {
	ManifestURL          string `json:"manifest_url"`
	ManifestBranch       string `json:"manifest_branch"`
	ManifestName         string `json:"manifest_name"`
	Groups              string `json:"groups"`
	Platform            string `json:"platform"`
	Mirror              bool   `json:"mirror"`
	Archive             bool   `json:"archive"`
	Worktree            bool   `json:"worktree"`
	Reference           string `json:"reference"`
	NoSmartCache        bool   `json:"no_smart_cache"`
	Dissociate          bool   `json:"dissociate"`
	Depth               int    `json:"depth"`
	PartialClone        bool   `json:"partial_clone"`
	PartialCloneExclude string `json:"partial_clone_exclude"`
	CloneFilter         string `json:"clone_filter"`
	UseSuperproject     bool   `json:"use_superproject"`
	CloneBundle         bool   `json:"clone_bundle"`
	GitLFS              bool   `json:"git_lfs"`
	RepoURL             string `json:"repo_url"`
	RepoRev             string `json:"repo_rev"`
	NoRepoVerify        bool   `json:"no_repo_verify"`
	StandaloneManifest  bool   `json:"standalone_manifest"`
	Submodules          bool   `json:"submodules"`
	CurrentBranch       bool   `json:"current_branch"`
	Tags                bool   `json:"tags"`
	ConfigName          string `json:"config_name"`
	RepoRoot            string `yaml:"repo_root"`
	DefaultRemoteURL    string `json:"default_remote_url"`
	Verbose            bool   `json:"verbose"`
	Quiet              bool   `json:"quiet"`
}

// Load 加载配置
func Load() (*Config, error) {
	configPath := filepath.Join(".repo", "config.json")
	
	// 检查配置文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("repo not initialized, run 'repo init' first")
	}
	
	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	// 解析配置
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	
	return &config, nil
}

// Save 保存配置
func (c *Config) Save() error {
	// 确保.repo目录存在
	if err := os.MkdirAll(".repo", 0755); err != nil {
		return fmt.Errorf("failed to create .repo directory: %w", err)
	}
	
	// 序列化配置
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	
	// 写入配置文件
	configPath := filepath.Join(".repo", "config.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	
	return nil
}

// GetRepoRoot 获取repo根目录
func GetRepoRoot() (string, error) {
	// 从当前目录开始向上查找.repo目录
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	
	for {
		repoDir := filepath.Join(dir, ".repo")
		if _, err := os.Stat(repoDir); err == nil {
			return dir, nil
		}
		
		// 到达根目录，停止查找
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	
	return "", fmt.Errorf("not in a repo client")
}
func (c *Config) GetRemoteURL() string {
	if c == nil || c.ManifestURL == "" {
		return ""
	}
	
	// 提取协议和域名部分
	manifestURL := c.ManifestURL
	if strings.HasPrefix(manifestURL, "ssh://") || strings.HasPrefix(manifestURL, "http://") || strings.HasPrefix(manifestURL, "https://") {
		// 移除路径部分
		// 查找协议后的第一个斜杠
		doubleSlash := strings.Index(manifestURL, "//")
		if doubleSlash > 0 {
			firstSlash := strings.Index(manifestURL[doubleSlash+2:], "/")
			if firstSlash > 0 {
				return manifestURL[:doubleSlash+2+firstSlash]
			}
			return manifestURL
		}
		return manifestURL
	}
	return ""
}