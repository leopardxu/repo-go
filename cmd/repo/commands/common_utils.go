package commands

import (
	"fmt"
	"os"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
)

// EnsureRepoRoot 确保当前工作目录在repo根目录下
// 如果不在，则切换到repo根目录
// 返回原始工作目录，以便在需要时恢复
func EnsureRepoRoot(log logger.Logger) (string, error) {
	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取当前工作目录失败: %w", err)
	}

	// 查找repo根目录
	repoRoot, err := config.GetRepoRoot()
	if err != nil {
		return "", fmt.Errorf("查找repo根目录失败: %w", err)
	}

	// 如果当前目录不是repo根目录，切换到repo根目录
	if cwd != repoRoot {
		log.Debug("切换工作目录: %s -> %s", cwd, repoRoot)
		if err := os.Chdir(repoRoot); err != nil {
			return "", fmt.Errorf("切换到repo根目录失败: %w", err)
		}
	}

	return cwd, nil
}

// RestoreWorkDir 恢复到原始工作目录
func RestoreWorkDir(originalDir string, log logger.Logger) error {
	if originalDir == "" {
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前工作目录失败: %w", err)
	}

	if cwd != originalDir {
		log.Debug("恢复工作目录: %s -> %s", cwd, originalDir)
		if err := os.Chdir(originalDir); err != nil {
			return fmt.Errorf("恢复工作目录失败: %w", err)
		}
	}

	return nil
}
