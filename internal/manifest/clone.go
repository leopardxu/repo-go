package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/leopardxu/repo-go/internal/logger"
)

// extractBaseURL 从清单URL中提取基础URL
func extractBaseURL(url string) string {
	if url == "" {
		return ""
	}

	// 处理SSH URL格式: ssh://git@example.com/path/to/repo
	if strings.HasPrefix(url, "ssh://") {
		// 查找第三个斜杠的位置（ssh://后的第一个斜杠）
		parts := strings.SplitN(url, "/", 4)
		if len(parts) >= 3 {
			// 返回 ssh://hostname 部分
			return strings.Join(parts[:3], "/")
		}
	}
	
	// 处理SCP格式: git@example.com:path/to/repo
	if strings.Contains(url, "@") && strings.Contains(url, ":") {
		// 查找冒号的位�?
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			// 返回 user@hostname 部分
			return parts[0]
		}
	}
	
	// 处理HTTP/HTTPS URL
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		// 查找第三个斜杠后的位�?
		parts := strings.SplitN(url, "/", 4)
		if len(parts) >= 3 {
			// 返回 protocol://hostname 部分
			return strings.Join(parts[:3], "/")
		}
	}
	
	// 无法解析的情况下返回空字符串
	logger.Debug("无法�?%s 提取基础URL", url)
	return ""
}

// CloneManifestRepo 克隆清单仓库
func CloneManifestRepo(gitRunner GitRunner, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("配置不能为空")
	}

	if cfg.ManifestURL == "" {
		return fmt.Errorf("清单仓库URL不能为空")
	}

	logger.Info("开始克隆清单仓�? %s", cfg.ManifestURL)
	logger.Debug("清单分支: %s, 清单文件: %s", cfg.ManifestBranch, cfg.ManifestName)

	// 创建.repo目录
	repoDir := ".repo"
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		return fmt.Errorf("创建 %s 目录失败: %w", repoDir, err)
	}

	// 创建.repo/manifests目录
	manifestsDir := filepath.Join(repoDir, "manifests")
	if err := os.MkdirAll(manifestsDir, 0755); err != nil {
		return fmt.Errorf("创建 %s 目录失败: %w", manifestsDir, err)
	}

	// 处理URL中的..替换
	manifestURL := cfg.ManifestURL
	if strings.Contains(manifestURL, "..") {
		// 从清单URL中提取基础URL
		baseURL := extractBaseURL(cfg.ManifestURL)
		if baseURL != "" {
			// 替换..为baseURL
			oldURL := manifestURL
			manifestURL = strings.Replace(manifestURL, "..", baseURL, -1)
			logger.Debug("URL替换: %s -> %s", oldURL, manifestURL)
		}
	}

	// 构建git clone命令参数
	args := []string{"clone"}

	// 添加深度参数
	if cfg.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", cfg.Depth))
	}

	// 添加分支参数
	if cfg.ManifestBranch != "" {
		args = append(args, "--branch", cfg.ManifestBranch)
	}

	// 添加镜像参数
	if cfg.Mirror {
		args = append(args, "--mirror")
	}

	// 添加引用参数
	if cfg.Reference != "" {
		args = append(args, "--reference", cfg.Reference)
	}

	// 添加URL和目标目�?
	args = append(args, manifestURL, manifestsDir)

	// 执行git clone命令
	logger.Info("克隆清单仓库�?..")
	_, err := gitRunner.Run(args...)
	if err != nil {
		return fmt.Errorf("克隆清单仓库失败: %w", err)
	}

	// 创建清单符号链接
	manifestLink := filepath.Join(repoDir, "manifest.xml")
	manifestFile := filepath.Join(manifestsDir, cfg.ManifestName)

	// 检查清单文件是否存�?
	if _, err := os.Stat(manifestFile); os.IsNotExist(err) {
		return fmt.Errorf("清单文件 %s 不存�?, cfg.ManifestName)
	}

	// 创建相对路径
	relPath, err := filepath.Rel(repoDir, manifestFile)
	if err != nil {
		return fmt.Errorf("创建相对路径失败: %w", err)
	}

	// 删除现有链接（如果存在）
	if err := removeExistingLink(manifestLink); err != nil {
		logger.Warn("移除现有链接失败: %v", err)
	}

	// 创建符号链接
	logger.Info("创建清单符号链接: %s -> %s", manifestLink, relPath)
	if err := createSymlink(relPath, manifestLink); err != nil {
		return fmt.Errorf("创建清单符号链接失败: %w", err)
	}

	logger.Info("清单仓库克隆完成")
	return nil
}

// removeExistingLink 安全地移除现有链�?
func removeExistingLink(path string) error {
	// 检查文件是否存�?
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// 文件不存在，无需删除
		return nil
	}

	// 删除现有文件或链�?
	return os.Remove(path)
}

// createSymlink 创建符号链接，处理不同操作系统的差异
func createSymlink(oldname, newname string) error {
	// Windows系统下创建符号链接可能需要特殊处�?
	if runtime.GOOS == "windows" {
		// 检查目标是否为目录
		fi, err := os.Stat(oldname)
		if err == nil && fi.IsDir() {
			// Windows下创建目录符号链接需要额外权�?
			logger.Debug("在Windows上创建目录符号链�? %s -> %s", newname, oldname)
		}
	}

	// 创建符号链接
	return os.Symlink(oldname, newname)
}
