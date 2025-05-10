package repo_sync

import (
	"fmt"
	"sync"

	"github.com/cix-code/gogo/internal/logger"
	"github.com/cix-code/gogo/internal/project"
)

// AbandonResult 表示放弃分支操作的结果
type AbandonResult struct {
	Project *project.Project
	Branch  string
	Success bool
	Error   error
}

// AbandonTopics 支持批量放弃多个项目的本地 topic 分支，并发执行，输出简洁明了的结果
func (e *Engine) AbandonTopics(projects []*project.Project, topic string) []AbandonResult {
	var wg sync.WaitGroup
	jobs := e.options.JobsCheckout
	if jobs < 1 {
		jobs = 1
	}
	semaphore := make(chan struct{}, jobs)
	resultsChan := make(chan AbandonResult, len(projects))

	for _, p := range projects {
		wg.Add(1)
		go func(proj *project.Project) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			branch := topic
			if branch == "" {
				branch, _ = proj.GetCurrentBranch()
			}
			if branch == "" {
				resultsChan <- AbandonResult{Project: proj, Branch: branch, Success: false, Error: fmt.Errorf("未指定分支且当前分支为空")}
				return
			}

			// 放弃本地分支
			if !e.options.Quiet {
				if e.log != nil {
					e.log.Debug("正在删除项目 %s 的分支 %s", proj.Name, branch)
				}
			}
			
			err := proj.DeleteBranch(branch)
			if err != nil {
				if e.log != nil {
					e.log.Error("删除项目 %s 的分支 %s 失败: %v", proj.Name, branch, err)
				}
				resultsChan <- AbandonResult{Project: proj, Branch: branch, Success: false, Error: err}
				return
			}
			
			if !e.options.Quiet && e.log != nil {
				e.log.Debug("成功删除项目 %s 的分支 %s", proj.Name, branch)
			}
			resultsChan <- AbandonResult{Project: proj, Branch: branch, Success: true, Error: nil}
		}(p)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var results []AbandonResult
	for r := range resultsChan {
		results = append(results, r)
	}
	return results
}

// PrintAbandonSummary 输出放弃分支的汇总信息
func PrintAbandonSummary(results []AbandonResult, log logger.Logger) {
	total := len(results)
	success := 0
	failed := 0
	
	// 按项目名称排序输出结果
	for _, r := range results {
		if r.Success {
			success++
			if log != nil {
				log.Info("[OK]    %s: 成功删除分支 %s", r.Project.Name, r.Branch)
			} else {
				fmt.Printf("[OK]    %s: %s\n", r.Project.Name, r.Branch)
			}
		} else {
			failed++
			if log != nil {
				log.Error("[FAIL]  %s: 删除分支 %s 失败 (%v)", r.Project.Name, r.Branch, r.Error)
			} else {
				fmt.Printf("[FAIL]  %s: %s (%v)\n", r.Project.Name, r.Branch, r.Error)
			}
		}
	}
	
	// 输出汇总信息
	if log != nil {
		log.Info("\n共处理项目: %d, 成功: %d, 失败: %d", total, success, failed)
	} else {
		fmt.Printf("\n共处理项目: %d, 成功: %d, 失败: %d\n", total, success, failed)
	}
}