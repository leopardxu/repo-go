package sync

import (
	"fmt"
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
	
	if !success && !e.options.Quiet {
		fmt.Printf("error: Cannot checkout %s\n", project.Name)
	}
	
	return CheckoutResult{
		Success: success,
		Project: project,
	}
}