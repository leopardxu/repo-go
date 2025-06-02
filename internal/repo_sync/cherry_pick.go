package repo_sync

import (
	"fmt"
	"sync"

	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/progress"
	"github.com/leopardxu/repo-go/internal/project"
)

// 添加cherry-pick统计信息
type cherryPickStats struct {
	Success int
	Failed  int
	mu      sync.Mutex
}

// SetCommitHash 设置要cherry-pick的提交哈�?
func (e *Engine) SetCommitHash(commitHash string) {
	e.commitHash = commitHash
}

// GetCherryPickStats 获取cherry-pick操作的统计信�?
func (e *Engine) GetCherryPickStats() (int, int) {
	return e.cherryPickStats.Success, e.cherryPickStats.Failed
}

// CherryPickCommit 在指定项目中应用cherry-pick
func (e *Engine) CherryPickCommit(projects []*project.Project) error {
	if e.log == nil {
		e.log = logger.NewDefaultLogger()
		if e.options.Verbose {
			e.log.SetLevel(logger.LogLevelDebug)
		} else if e.options.Quiet {
			e.log.SetLevel(logger.LogLevelError)
		}
	}

	if e.commitHash == "" {
		return fmt.Errorf("commit hash is not specified")
	}

	e.log.Info("开始在 %d 个项目中应用 cherry-pick '%s'", len(projects), e.commitHash)

	// 初始化统计信�?
	e.cherryPickStats = &cherryPickStats{}

	// 只在有工作树的项目中应用
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

	// 执行cherry-pick
	if len(worktreeProjects) == 0 {
		e.log.Info("没有可应用的项目")
		return nil
	}

	if e.options.Jobs == 1 {
		// 单线程执�?
		e.log.Debug("使用单线程模式应�?cherry-pick")
		for _, project := range worktreeProjects {
			result := e.cherryPickOne(project)
			e.processCherryPickResult(result, pm)
		}
	} else {
		// 多线程执�?
		e.log.Debug("使用多线程模式应�?cherry-pick，并发数: %d", e.options.Jobs)
		
		// 创建工作�?
		var wg sync.WaitGroup
		resultsChan := make(chan CherryPickResult, len(worktreeProjects))
		
		// 限制并发�?
		semaphore := make(chan struct{}, e.options.Jobs)
		
		for _, p := range worktreeProjects {
			wg.Add(1)
			go func(proj *project.Project) {
				defer wg.Done()
				
				// 获取信号�?
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				
				// 执行cherry-pick
				result := e.cherryPickOne(proj)
				resultsChan <- result
			}(p)
		}
		
		// 等待所有操作完�?
		go func() {
			wg.Wait()
			close(resultsChan)
		}()
		
		// 处理结果
		for result := range resultsChan {
			e.processCherryPickResult(result, pm)
		}
	}
	
	if !e.options.Quiet {
		pm.Finish()
	}
	
	e.log.Info("Cherry-pick '%s' 完成: %d 成功, %d 失败", e.commitHash, e.cherryPickStats.Success, e.cherryPickStats.Failed)
	
	if e.cherryPickStats.Failed > 0 {
		return fmt.Errorf("Cherry-pick 失败: %d 个项目出�?, e.cherryPickStats.Failed)
	}
	
	return nil
}

// processCherryPickResult 处理cherry-pick结果
func (e *Engine) processCherryPickResult(result CherryPickResult, pm *progress.ConsoleReporter) {
	e.cherryPickStats.mu.Lock()
	defer e.cherryPickStats.mu.Unlock()
	
	if result.Success {
		e.cherryPickStats.Success++
		if e.options.Verbose && !e.options.Quiet {
			e.log.Debug("项目 %s cherry-pick 成功", result.Project.Name)
		}
	} else {
		e.cherryPickStats.Failed++
		e.errResults = append(e.errResults, result.Project.Path)
		if !e.options.Quiet {
			e.log.Error("项目 %s cherry-pick 失败: %v", result.Project.Name, result.Error)
		}
	}
	
	if !e.options.Quiet {
		pm.Update(1, result.Project.Name)
	}
}

// CherryPickResult 表示cherry-pick操作的结�?
type CherryPickResult struct {
	Success bool
	Project *project.Project
	Error   error
}

// cherryPickOne 在单个项目中应用cherry-pick
func (e *Engine) cherryPickOne(project *project.Project) CherryPickResult {
	if !e.options.Quiet {
		e.log.Info("在项�?%s 中应�?cherry-pick %s", project.Name, e.commitHash)
	}
	
	// 执行git cherry-pick命令
	_, err := project.GitRepo.RunCommand("cherry-pick", e.commitHash)
	if err != nil {
		e.log.Error("项目 %s cherry-pick 失败: %v", project.Name, err)
		return CherryPickResult{Success: false, Project: project, Error: err}
	}
	
	return CherryPickResult{Success: true, Project: project}
}
