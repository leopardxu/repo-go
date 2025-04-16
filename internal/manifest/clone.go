package manifest

import (
	"fmt"
	"os"
	"path/filepath"
)

// CloneManifestRepo 克隆清单仓库
func CloneManifestRepo(gitRunner GitRunner, cfg *Config) error {
	// 创建.repo目录
	if err := os.MkdirAll(".repo", 0755); err != nil {
		return fmt.Errorf("failed to create .repo directory: %w", err)
	}

	// 创建.repo/manifests目录
	manifestsDir := ".repo/manifests"
	if err := os.MkdirAll(manifestsDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", manifestsDir, err)
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

	// 添加URL和目标目录
	args = append(args, cfg.ManifestURL, manifestsDir)

	// 执行git clone命令
	_, err := gitRunner.Run(args...)
	if err != nil {
		return fmt.Errorf("failed to clone manifest repository: %w", err)
	}

	// 创建清单符号链接
	manifestLink := ".repo/manifest.xml"
	manifestFile := filepath.Join(manifestsDir, cfg.ManifestName)

	// 检查清单文件是否存在
	if _, err := os.Stat(manifestFile); os.IsNotExist(err) {
		return fmt.Errorf("manifest file %s does not exist", cfg.ManifestName)
	}

	// 创建相对路径
	relPath, err := filepath.Rel(".repo", manifestFile)
	if err != nil {
		return fmt.Errorf("failed to create relative path: %w", err)
	}

	// 删除现有链接（如果存在）
	_ = os.Remove(manifestLink)

	// 创建符号链接
	if err := os.Symlink(relPath, manifestLink); err != nil {
		return fmt.Errorf("failed to create manifest symlink: %w", err)
	}

	return nil
}