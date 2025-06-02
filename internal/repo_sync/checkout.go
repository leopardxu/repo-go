package repo_sync

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/progress"
	"github.com/leopardxu/repo-go/internal/project"
)

// 添加分支名称字段和统计信�?
type checkoutStats struct {
	Success int
	Failed  int
	mu      sync.Mutex
}

// SetBranchName 设置要检出的分支名称
func (e *Engine) SetBranchName(branchName string) {
	e.branchName = branchName
}

// GetCheckoutStats 获取检出操作的统计信息
func (e *Engine) GetCheckoutStats() (int, int) {
	return e.checkoutStats.Success, e.checkoutStats.Failed
}

// CheckoutBranch 检出指定分�?
func (e *Engine) CheckoutBranch(projects []*project.Project) error {
	if e.log == nil {
		e.log = logger.NewDefaultLogger()
		if e.options.Verbose {
			e.log.SetLevel(logger.LogLevelDebug)
		} else if e.options.Quiet {
			e.log.SetLevel(logger.LogLevelError)
		}
	}

	if e.branchName == "" {
		return fmt.Errorf("branch name is not specified")
	}

	e.log.Info("开始检出分�?'%s' �?%d 个项�?, e.branchName, len(projects))

	// 初始化统计信�?
	e.checkoutStats = &checkoutStats{}

	// 只检出有工作树的项目
	var worktreeProjects []*project.Project
	for _, project := range projects {
		if project.Worktree != "" {
			worktreeProjects = append(worktreeProjects, project)
		}
	}

	// 创建进度�?
	pm := progress.NewConsoleReporter()
	if !e.options.Quiet {
		pm.Start(len(worktreeProjects))
	}

	// 执行检�?
	if len(worktreeProjects) == 0 {
		e.log.Info("没有可检出的项目")
		return nil
	}

	if e.options.JobsCheckout == 1 {
		// 单线程检�?
		e.log.Debug("使用单线程模式检出项�?)
		for _, project := range worktreeProjects {
			result := e.checkoutOneBranch(project)
			e.processCheckoutResult(result, pm)
		}
	} else {
		// 多线程检�?
		e.log.Debug("使用多线程模式检出项目，并发�? %d", e.options.JobsCheckout)

		// 创建工作�?
		var wg sync.WaitGroup
		resultsChan := make(chan CheckoutResult, len(worktreeProjects))

		// 限制并发�?
		semaphore := make(chan struct{}, e.options.JobsCheckout)

		for _, p := range worktreeProjects {
			wg.Add(1)
			go func(proj *project.Project) {
				defer wg.Done()

				// 获取信号�?
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// 执行检�?
				result := e.checkoutOneBranch(proj)
				resultsChan <- result
			}(p)
		}

		// 等待所有检出完�?
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// 处理结果
		for result := range resultsChan {
			e.processCheckoutResult(result, pm)
		}
	}

	if !e.options.Quiet {
		pm.Finish()
	}

	e.log.Info("检出分�?'%s' 完成: %d 成功, %d 失败", e.branchName, e.checkoutStats.Success, e.checkoutStats.Failed)

	if e.checkoutStats.Failed > 0 {
		return fmt.Errorf("检出失�? %d 个项目出�?, e.checkoutStats.Failed)
	}

	return nil
}

// processCheckoutResult 处理检出结�?
func (e *Engine) processCheckoutResult(result CheckoutResult, pm *progress.ConsoleReporter) {
	e.checkoutStats.mu.Lock()
	defer e.checkoutStats.mu.Unlock()

	if result.Success {
		e.checkoutStats.Success++
		if e.options.Verbose && !e.options.Quiet {
			e.log.Debug("项目 %s 检出成�?, result.Project.Name)
		}
	} else {
		e.checkoutStats.Failed++
		e.errResults = append(e.errResults, result.Project.Path)
		if !e.options.Quiet {
			e.log.Error("项目 %s 检出失�?, result.Project.Name)
		}
	}

	if !e.options.Quiet {
		pm.Update(1, result.Project.Name)
	}
}

// checkout 执行本地检�?
func (e *Engine) checkout(allProjects []*project.Project, hyperSyncProjects []*project.Project) error {
	// 如果使用HyperSync，只检出已更改的项�?
	projectsToCheckout := allProjects
	if hyperSyncProjects != nil {
		projectsToCheckout = hyperSyncProjects
	}

	// 只检出有工作树的项目
	var worktreeProjects []*project.Project
	for _, project := range projectsToCheckout {
		if project.Worktree != "" {
			worktreeProjects = append(worktreeProjects, project)
		}
	}

	// 创建进度�?
	pm := progress.NewConsoleReporter()
	if !e.options.Quiet {
		pm.Start(len(worktreeProjects))
	}

	// 处理结果
	processResults := func(results []CheckoutResult) bool {
		ret := true
		for _, result := range results {
			if !result.Success {
				ret = false
				e.errResults = append(e.errResults, result.Project.Path)

				if e.options.FailFast {
					return false
				}
			}
			if !e.options.Quiet {
				pm.Update(1, result.Project.Name)
			}
		}
		return ret
	}

	// 执行检�?
	if len(worktreeProjects) == 0 {
		return nil
	}

	if e.options.JobsCheckout == 1 {
		// 单线程检�?
		results := make([]CheckoutResult, 0, len(worktreeProjects))
		for _, project := range worktreeProjects {
			result := e.checkoutOne(project)
			results = append(results, result)

			if !processResults([]CheckoutResult{result}) {
				break
			}
		}
	} else {
		// 多线程检�?
		jobs := e.options.JobsCheckout

		// 创建工作�?
		var wg sync.WaitGroup
		resultsChan := make(chan CheckoutResult, len(worktreeProjects))

		// 限制并发�?
		semaphore := make(chan struct{}, jobs)

		for _, p := range worktreeProjects {
			wg.Add(1)
			go func(proj *project.Project) {
				defer wg.Done()

				// 获取信号�?
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// 执行检�?
				result := e.checkoutOne(proj)
				resultsChan <- result
			}(p)
		}

		// 等待所有检出完�?
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// 处理结果
		results := make([]CheckoutResult, 0, len(worktreeProjects))
		for result := range resultsChan {
			results = append(results, result)

			if !processResults([]CheckoutResult{result}) && e.options.FailFast {
				break
			}
		}
	}

	if !e.options.Quiet {
		pm.Finish()
	}

	return nil
}

// CheckoutResult 表示检出操作的结果
type CheckoutResult struct {
	Success bool
	Project *project.Project
}

// checkoutOneBranch 检出单个项目的指定分支
func (e *Engine) checkoutOneBranch(project *project.Project) CheckoutResult {
	if !e.options.Quiet {
		e.log.Info("检出项�?%s 的分�?%s", project.Name, e.branchName)
	}

	// 如果是分离模式，检出项目的修订版本
	if e.options.Detach {
		e.log.Debug("项目 %s 使用分离模式检出修订版�?%s", project.Name, project.Revision)
		_, err := project.GitRepo.RunCommand("checkout", project.Revision)
		if err != nil {
			e.log.Error("项目 %s 检出修订版本失�? %v", project.Name, err)
			return CheckoutResult{Success: false, Project: project}
		}
	} else {
		// 否则，创建并检出指定分�?
		e.log.Debug("项目 %s 创建并检出分�?%s", project.Name, e.branchName)

		// 先检查远程分支是否存在冲�?
		output, _ := project.GitRepo.RunCommand("branch", "-r", "--list", fmt.Sprintf("*/%s", e.branchName))
		remoteBranches := strings.Split(strings.TrimSpace(string(output)), "\n")

		if len(remoteBranches) > 1 {
			// 多个远程分支匹配，需要明确指定远程分�?

			// 首先尝试使用项目自身的RemoteName
			if project.RemoteName != "" {
				// 检查项目的远程是否包含该分�?
				hasProjectRemoteBranch := false
				for _, remoteBranch := range remoteBranches {
					remoteBranch = strings.TrimSpace(remoteBranch)
					if strings.HasPrefix(remoteBranch, fmt.Sprintf("%s/%s", project.RemoteName, e.branchName)) {
						hasProjectRemoteBranch = true
						break
					}
				}

				if hasProjectRemoteBranch {
					// 使用项目自身的远�?
					_, err := project.GitRepo.RunCommand("checkout", "--track", fmt.Sprintf("%s/%s", project.RemoteName, e.branchName))
					if err != nil {
						e.log.Error("项目 %s 检出远程分支失�? %v", project.Name, err)
						return CheckoutResult{Success: false, Project: project}
					}
					return CheckoutResult{Success: true, Project: project}
				}
			}

			// 如果项目远程不存在或不包含该分支，尝试使用配置的默认远程
			if e.options.DefaultRemote != "" {
				// 检查默认远程是否包含该分支
				hasDefaultRemoteBranch := false
				for _, remoteBranch := range remoteBranches {
					remoteBranch = strings.TrimSpace(remoteBranch)
					if strings.HasPrefix(remoteBranch, fmt.Sprintf("%s/%s", e.options.DefaultRemote, e.branchName)) {
						hasDefaultRemoteBranch = true
						break
					}
				}

				if hasDefaultRemoteBranch {
					// 使用配置的默认远�?
					_, err := project.GitRepo.RunCommand("checkout", "--track", fmt.Sprintf("%s/%s", e.options.DefaultRemote, e.branchName))
					if err != nil {
						e.log.Error("项目 %s 检出远程分支失�? %v", project.Name, err)
						return CheckoutResult{Success: false, Project: project}
					}
					return CheckoutResult{Success: true, Project: project}
				} else {
					// 默认远程不包含该分支，记录详细信�?
					e.log.Error("项目 %s 检出失�? 默认远程 '%s' 不包含分�?'%s'", project.Name, e.options.DefaultRemote, e.branchName)
				}
			} else {
				// 没有配置默认远程，返回错�?
				e.log.Error("项目 %s 检出失�? 分支 '%s' 匹配多个远程跟踪分支，请使用 --default-remote 指定默认远程", project.Name, e.branchName)
			}

			// 输出可用的远程分支列表，帮助用户选择
			e.log.Info("项目 %s 的可用远程分�?", project.Name)
			for _, remoteBranch := range remoteBranches {
				if remoteBranch != "" {
					e.log.Info("  %s", strings.TrimSpace(remoteBranch))
				}
			}

			return CheckoutResult{Success: false, Project: project}
		} else if len(remoteBranches) == 1 && remoteBranches[0] != "" {
			// 只有一个远程分支匹配，直接检�?
			remoteBranch := strings.TrimSpace(remoteBranches[0])
			_, err := project.GitRepo.RunCommand("checkout", "--track", remoteBranch)
			if err != nil {
				e.log.Error("项目 %s 检出远程分支失�? %v", project.Name, err)
				return CheckoutResult{Success: false, Project: project}
			}
		} else {
			// 没有远程分支匹配，创建新分支
			_, err := project.GitRepo.RunCommand("checkout", "-B", e.branchName)
			if err != nil {
				e.log.Error("项目 %s 创建并检出分支失�? %v", project.Name, err)
				return CheckoutResult{Success: false, Project: project}
			}
		}
	}

	// 如果检出成功，复制钩子脚本到项�?
	repoHooksDir := filepath.Join(e.repoRoot, ".repo", "hooks")
	projectGitDir := filepath.Join(project.Worktree, ".git")

	if err := copyHooksToProject(repoHooksDir, projectGitDir); err != nil {
		e.log.Warn("无法复制钩子脚本到项�?%s: %v", project.Name, err)
		// 不因为钩子复制失败而导致整个检出失�?
	}

	return CheckoutResult{Success: true, Project: project}
}

// checkoutOne 检出单个项�?
func (e *Engine) checkoutOne(project *project.Project) CheckoutResult {
	if !e.options.Quiet {
		if e.log != nil {
			e.log.Info("检出项�?%s", project.Name)
		} else {
			fmt.Printf("检出项�?%s\n", project.Name)
		}
	}

	// 执行本地同步
	success := project.SyncLocalHalf(
		e.options.Detach,
		e.options.ForceSync,
		e.options.ForceOverwrite,
	)

	// 如果检出成功，复制钩子脚本到项�?
	if success {
		// 获取 .repo/hooks 目录路径
		repoHooksDir := filepath.Join(e.repoRoot, ".repo", "hooks")

		// 获取项目�?.git 目录路径
		projectGitDir := filepath.Join(project.Worktree, ".git")

		// 复制钩子脚本
		if err := copyHooksToProject(repoHooksDir, projectGitDir); err != nil && !e.options.Quiet {
			if e.log != nil {
				e.log.Warn("无法复制钩子脚本到项�?%s: %v", project.Name, err)
			} else {
				fmt.Printf("警告: 无法复制钩子脚本到项�?%s: %v\n", project.Name, err)
			}
		}
	} else if !e.options.Quiet {
		if e.log != nil {
			e.log.Error("无法检出项�?%s", project.Name)
		} else {
			fmt.Printf("error: Cannot checkout %s\n", project.Name)
		}
	}

	return CheckoutResult{
		Success: success,
		Project: project,
	}
}

// copyHooksToProject �?.repo/hooks 中的钩子复制到指定项目的 .git/hooks 目录
func copyHooksToProject(repoHooksDir, projectGitDir string) error {
	hooks, err := os.ReadDir(repoHooksDir)
	if err != nil {
		// 如果 .repo/hooks 目录不存在，则忽�?
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("无法读取 .repo/hooks 目录: %w", err)
	}

	projectHooksDir := filepath.Join(projectGitDir, "hooks")
	if err := os.MkdirAll(projectHooksDir, 0755); err != nil {
		return fmt.Errorf("无法创建项目钩子目录 %s: %w", projectHooksDir, err)
	}

	for _, hookEntry := range hooks {
		if hookEntry.IsDir() {
			continue // 跳过子目�?
		}

		hookName := hookEntry.Name()
		srcPath := filepath.Join(repoHooksDir, hookName)
		destPath := filepath.Join(projectHooksDir, hookName)

		// 复制文件内容
		srcFile, err := os.Open(srcPath)
		if err != nil {
			fmt.Printf("警告: 无法打开源钩子文�?%s: %v\n", srcPath, err)
			continue
		}
		defer srcFile.Close()

		destFile, err := os.OpenFile(destPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755) // 使用 0755 权限
		if err != nil {
			fmt.Printf("警告: 无法创建或打开目标钩子文件 %s: %v\n", destPath, err)
			continue
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, srcFile)
		if err != nil {
			fmt.Printf("警告: 无法将钩�?%s 复制�?%s: %v\n", hookName, destPath, err)
			continue
		}

		// 确保目标文件是可执行�?(虽然 OpenFile 已经设置了权限，这里再次确认)
		// �?Windows 上，os.Chmod 可能效果有限，但写入是最佳实�?
		if err := os.Chmod(destPath, 0755); err != nil {
			fmt.Printf("警告: 无法设置钩子 %s 的执行权�? %v\n", destPath, err)
		}
	}
	return nil
}
