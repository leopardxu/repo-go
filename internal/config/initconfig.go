package config

import (
	"fmt"
	"os"
	"os/exec"
)

// LoadGitConfig 加载git配置
func LoadGitConfig() error {
	// 检查git是否安装
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found: %w", err)
	}

	// 设置默认git配置
	if err := setDefaultGitConfig(); err != nil {
		return fmt.Errorf("failed to set git config: %w", err)
	}

	return nil
}

// setDefaultGitConfig 设置默认git配置
func setDefaultGitConfig() error {
	// 设置用户名和邮箱
	if err := runGitCommand("config", "--global", "user.name", "CIX Code"); err != nil {
		return err
	}
	if err := runGitCommand("config", "--global", "user.email", "cix-code@example.com"); err != nil {
		return err
	}

	// 设置其他默认配置
	if err := runGitCommand("config", "--global", "core.autocrlf", "false"); err != nil {
		return err
	}
	if err := runGitCommand("config", "--global", "core.filemode", "false"); err != nil {
		return err
	}

	return nil
}

// runGitCommand 执行git命令
func runGitCommand(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}