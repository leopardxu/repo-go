package repo_sync

import (
	"fmt"
	"io" // 新增导入
	"os" // 新增导入
	"path/filepath" // 新增导入
	"sync"

	"github.com/cix-code/gogo/internal/progress"
	"github.com/cix-code/gogo/internal/project" // Ensure this line exists and is correct
)

// checkout 执行本地检出
func (e *Engine) checkout(allProjects []*project.Project, hyperSyncProjects []*project.Project) error {
	// 如果使用HyperSync，只检出已更改的项目
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
	
	// 创建进度条
	pm := progress.New("检出中", len(worktreeProjects), !e.options.Quiet)
	
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
			pm.Update(result.Project.Name)
		}
		return ret
	}
	
	// 执行检出
	if len(worktreeProjects) == 0 {
		return nil
	}
	
	if e.options.JobsCheckout == 1 {
		// 单线程检出
		results := make([]CheckoutResult, 0, len(worktreeProjects))
		for _, project := range worktreeProjects {
			result := e.checkoutOne(project)
			results = append(results, result)
			
			if !processResults([]CheckoutResult{result}) {
				break
			}
		}
	} else {
		// 多线程检出
		jobs := e.options.JobsCheckout
		
		// 创建工作池
		var wg sync.WaitGroup
		resultsChan := make(chan CheckoutResult, len(worktreeProjects))
		
		// 限制并发数
		semaphore := make(chan struct{}, jobs)
		
		for _, p := range worktreeProjects {
			wg.Add(1)
			go func(proj *project.Project) {
				defer wg.Done()
				
				// 获取信号量
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				
				// 执行检出
				result := e.checkoutOne(proj)
				resultsChan <- result
			}(p)
		}
		
		// 等待所有检出完成
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
	
	pm.End()
	
	return nil
}

// CheckoutResult 表示检出操作的结果
type CheckoutResult struct {
	Success bool
	Project *project.Project // This line requires the import above
}

// checkoutOne 检出单个项目
func (e *Engine) checkoutOne(project *project.Project) CheckoutResult {
	if !e.options.Quiet {
		fmt.Printf("检出项目 %s\n", project.Name)
	}
	
	// 执行本地同步
	success := project.SyncLocalHalf(
		e.options.Detach,
		e.options.ForceSync,
		e.options.ForceOverwrite,
	)
	
	// 如果检出成功，复制钩子脚本到项目
	if success {
		// 获取 .repo/hooks 目录路径
		repoHooksDir := filepath.Join(e.repoRoot, ".repo", "hooks")
		
		// 获取项目的 .git 目录路径
		projectGitDir := filepath.Join(project.Worktree, ".git")
		
		// 复制钩子脚本
		if err := copyHooksToProject(repoHooksDir, projectGitDir); err != nil && !e.options.Quiet {
			fmt.Printf("警告: 无法复制钩子脚本到项目 %s: %v\n", project.Name, err)
		}
	} else if !e.options.Quiet {
		fmt.Printf("error: Cannot checkout %s\n", project.Name)
	}
	
	return CheckoutResult{
		Success: success,
		Project: project,
	}
}

// copyHooksToProject 将 .repo/hooks 中的钩子复制到指定项目的 .git/hooks 目录
func copyHooksToProject(repoHooksDir, projectGitDir string) error {
	hooks, err := os.ReadDir(repoHooksDir)
	if err != nil {
		// 如果 .repo/hooks 目录不存在，则忽略
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
			continue // 跳过子目录
		}

		hookName := hookEntry.Name()
		srcPath := filepath.Join(repoHooksDir, hookName)
		destPath := filepath.Join(projectHooksDir, hookName)

		// 复制文件内容
		srcFile, err := os.Open(srcPath)
		if err != nil {
			fmt.Printf("警告: 无法打开源钩子文件 %s: %v\n", srcPath, err)
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
			fmt.Printf("警告: 无法将钩子 %s 复制到 %s: %v\n", hookName, destPath, err)
			continue
		}

		// 确保目标文件是可执行的 (虽然 OpenFile 已经设置了权限，这里再次确认)
		// 在 Windows 上，os.Chmod 可能效果有限，但写入是最佳实践
		if err := os.Chmod(destPath, 0755); err != nil {
			fmt.Printf("警告: 无法设置钩子 %s 的执行权限: %v\n", destPath, err)
		}
	}
	return nil
}