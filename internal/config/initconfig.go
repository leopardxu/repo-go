package config

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// LoadGitConfig 加载git配置
func LoadGitConfig() error {
	log.Debug("加载Git配置")

	// 检查git是否安装
	gitPath, err := exec.LookPath("git")
	if err != nil {
		log.Error("未找到Git: %v", err)
		return &ConfigError{Op: "load_git_config", Err: fmt.Errorf("git not found: %w", err)}
	}
	log.Debug("找到Git路径: %s", gitPath)

	// 设置默认git配置
	if err := setDefaultGitConfig(); err != nil {
		log.Error("设置Git配置失败: %v", err)
		return &ConfigError{Op: "set_git_config", Err: fmt.Errorf("failed to set git config: %w", err)}
	}

	log.Info("Git配置加载成功")
	return nil
}

// setDefaultGitConfig 设置默认git配置
func setDefaultGitConfig() error {
	log.Debug("设置默认Git配置")

	// 检查是否已经设置了用户名和邮箱
	hasUserName, err := hasGitConfig("user.name")
	if err != nil {
		log.Warn("检查Git用户名配置失�? %v", err)
	}

	hasUserEmail, err := hasGitConfig("user.email")
	if err != nil {
		log.Warn("检查Git邮箱配置失败: %v", err)
	}

	// 只有在未设置的情况下才设置默认�?
	if !hasUserName {
		log.Info("设置默认Git用户�? CIX Code")
		if err := runGitCommand("config", "--global", "user.name", "CIX Code"); err != nil {
			log.Error("设置Git用户名失�? %v", err)
			return err
		}
	} else {
		log.Debug("Git用户名已设置，跳�?)
	}

	if !hasUserEmail {
		log.Info("设置默认Git邮箱: cix-code@example.com")
		if err := runGitCommand("config", "--global", "user.email", "cix-code@example.com"); err != nil {
			log.Error("设置Git邮箱失败: %v", err)
			return err
		}
	} else {
		log.Debug("Git邮箱已设置，跳过")
	}

	// 检查是否已经设置了checkout.defaultRemote
	hasDefaultRemote, err := hasGitConfig("checkout.defaultRemote")
	if err != nil {
		log.Warn("检查Git checkout.defaultRemote配置失败: %v", err)
	}

	// 如果未设置，则设置默认值为origin
	if !hasDefaultRemote {
		log.Info("设置默认Git checkout.defaultRemote: origin")
		if err := runGitCommand("config", "--global", "checkout.defaultRemote", "origin"); err != nil {
			log.Error("设置Git checkout.defaultRemote失败: %v", err)
			return err
		}
	} else {
		log.Debug("Git checkout.defaultRemote已设置，跳过")
	}

	// 设置其他默认配置
	log.Debug("设置Git core.autocrlf=false")
	if err := runGitCommand("config", "--global", "core.autocrlf", "false"); err != nil {
		log.Error("设置Git autocrlf失败: %v", err)
		return err
	}

	log.Debug("设置Git core.filemode=false")
	if err := runGitCommand("config", "--global", "core.filemode", "false"); err != nil {
		log.Error("设置Git filemode失败: %v", err)
		return err
	}

	log.Debug("Git默认配置设置完成")
	return nil
}

// runGitCommand 执行git命令
func runGitCommand(args ...string) error {
	log.Debug("执行Git命令: git %s", strings.Join(args, " "))

	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()

	if err != nil {
		log.Error("Git命令执行失败: %v", err)
		return &ConfigError{Op: "git_command", Err: fmt.Errorf("git command failed: %w", err)}
	}

	return nil
}

// hasGitConfig 检查是否已设置了指定的Git配置
func hasGitConfig(name string) (bool, error) {
	log.Debug("检查Git配置: %s", name)

	cmd := exec.Command("git", "config", "--global", "--get", name)
	output, err := cmd.Output()

	if err != nil {
		// 如果命令返回非零状态码，通常表示配置不存�?
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			log.Debug("Git配置 %s 未设�?, name)
			return false, nil
		}

		// 其他错误
		log.Error("检查Git配置失败: %v", err)
		return false, err
	}

	// 如果有输出，说明配置已存�?
	hasConfig := len(output) > 0
	log.Debug("Git配置 %s %s", name, map[bool]string{true: "已设�?, false: "未设�?}[hasConfig])
	return hasConfig, nil
}
