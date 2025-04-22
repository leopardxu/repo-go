package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync" // 添加 sync 包导入
	"time"

	"github.com/cix-code/gogo/internal/git"
	"github.com/cix-code/gogo/internal/progress"
	"github.com/cix-code/gogo/internal/project"
)

// fetchMain 执行网络获取
func (e *Engine) fetchMain(hyperSyncProjects []*project.Project) ([]*project.Project, error) {
	// 获取仓库项目
	repoProject := e.manifest.RepoProject
	// 修复 git.NewRunner() 调用
	projectRepoProject := convertManifestProject(repoProject, git.NewRunner())
	
	// 确定要获取的项目
	toFetch := []*project.Project{}
	noFetch := make(map[string]bool)
	
	// 检查仓库项目是否需要更新
	now := time.Now()
	if now.Sub(projectRepoProject.LastFetch) >= 24*time.Hour {
		toFetch = append(toFetch, projectRepoProject)
	}
	
	// 如果使用HyperSync，只获取已更改的项目
	if hyperSyncProjects != nil {
		toFetch = append(toFetch, hyperSyncProjects...)
		for _, project := range hyperSyncProjects {
			noFetch[project.Gitdir] = true
		}
	} else {
		toFetch = append(toFetch, e.projects...)
	}
	
	// 按照获取时间排序
	sort.Slice(toFetch, func(i, j int) bool {
		return e.getFetchTime(toFetch[i]) > e.getFetchTime(toFetch[j])
	})
	
	// 执行获取
	success, fetched := e.fetch(toFetch)
	if !success {
		select {
		case e.errEvent <- struct{}{}:
		default:
		}
	}
	
	// 更新仓库项目
	e.postRepoFetch(projectRepoProject)
	
	// 如果只执行网络同步，则返回
	if e.options.NetworkOnly {
		if !success {
			return nil, fmt.Errorf("由于获取错误退出同步")
		}
		return e.projects, nil
	}
	
	// 迭代获取缺失的项目
	previouslyMissingSet := make(map[string]bool)
	for {
		// 重新加载清单
		if err := e.reloadManifest("", true); err != nil {
			return nil, err
		}
		
		// 获取所有项目
		allProjects, err := e.getProjects()
		if err != nil {
			return nil, err
		}
		
		// 查找缺失的项目
		missing := []*project.Project{}
		for _, project := range allProjects {
			if _, ok := fetched[project.Gitdir]; !ok && !noFetch[project.Gitdir] {
				missing = append(missing, project)
			}
		}
		
		if len(missing) == 0 {
			return allProjects, nil
		}
		
		// 检查是否有新的缺失项目
		missingSet := make(map[string]bool)
		for _, p := range missing {
			missingSet[p.Name] = true
		}
		
		// 如果缺失的项目集合没有变化，则退出循环
		if reflect.DeepEqual(previouslyMissingSet, missingSet) {
			break
		}
		previouslyMissingSet = missingSet
		
		// 获取缺失的项目
		success, newFetched := e.fetch(missing)
		if !success {
			select {
			case e.errEvent <- struct{}{}:
			default:
			}
		}
		
		// 更新已获取的项目集合
		for k, v := range newFetched {
			fetched[k] = v
		}
	}
	
	return e.projects, nil
}

// fetch 执行获取操作
func (e *Engine) fetch(projects []*project.Project) (bool, map[string]bool) {
	ret := true
	fetched := make(map[string]bool)
	
	// 创建进度条
	pm := progress.New("获取中", len(projects), !e.options.Quiet)
	
	// 按对象目录分组项目
	objdirProjectMap := make(map[string][]*project.Project)
	for _, project := range projects {
		objdirProjectMap[project.Objdir] = append(objdirProjectMap[project.Objdir], project)
	}
	
	// 将分组后的项目转换为列表
	projectsList := make([][]*project.Project, 0, len(objdirProjectMap))
	for _, projects := range objdirProjectMap {
		projectsList = append(projectsList, projects)
	}
	
	// 处理结果
	processResults := func(results []FetchResult) bool {
		localRet := true
		for _, result := range results {
			e.setFetchTime(result.Project, result.Duration)
			
			if !result.Success {
				localRet = false
				e.errResults = append(e.errResults, fmt.Sprintf("获取 %s 失败", result.Project.Name))
			} else {
				fetched[result.Project.Gitdir] = true
			}
			
			pm.Update(result.Project.Name)
		}
		
		if !localRet && e.options.FailFast {
			return false
		}
		
		return localRet
	}
	
	// 执行获取
	if len(projectsList) == 1 || e.options.JobsNetwork == 1 {
		// 单线程获取
		for _, projects := range projectsList {
			results := e.fetchProjectList(projects)
			if !processResults(results) {
				ret = false
				break
			}
		}
	} else {
		// 多线程获取
		jobs := e.options.JobsNetwork
		
		// 创建工作池
		var wg sync.WaitGroup
		resultsChan := make(chan []FetchResult, len(projectsList))
		
		// 限制并发数
		semaphore := make(chan struct{}, jobs)
		
		for _, projects := range projectsList {
			wg.Add(1)
			go func(projects []*project.Project) {
				defer wg.Done()
				
				// 获取信号量
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				
				// 执行获取
				results := e.fetchProjectList(projects)
				resultsChan <- results
			}(projects)
		}
		
		// 等待所有获取完成
		go func() {
			wg.Wait()
			close(resultsChan)
		}()
		
		// 处理结果
		for results := range resultsChan {
			if !processResults(results) {
				ret = false
				break
			}
		}
	}
	
	pm.End()
	
	// 执行垃圾回收
	if !e.manifest.IsArchive {
		e.gcProjects(projects)
	}
	
	return ret, fetched
}

// FetchResult 表示获取操作的结果
type FetchResult struct {
	Success  bool
	Project  *project.Project
	Duration time.Duration
}

// fetchProjectList 获取项目列表
func (e *Engine) fetchProjectList(projects []*project.Project) []FetchResult {
	results := make([]FetchResult, 0, len(projects))
	
	for _, project := range projects {
		start := time.Now()
		success := e.fetchOne(project)
		duration := time.Since(start)
		
		results = append(results, FetchResult{
			Success:  success,
			Project:  project,
			Duration: duration,
		})
	}
	
	return results
}

// fetchOne 获取单个项目
func (e *Engine) fetchOne(project *project.Project) bool {
	if !e.options.Quiet {
		fmt.Printf("获取项目 %s\n", project.Name)
	}
	
	// 执行网络同步
	success := project.SyncNetworkHalf(
		e.options.Quiet,
		e.options.CurrentBranch,
		e.options.ForceSync,
		e.options.NoCloneBundle,
		e.options.Tags,
		e.manifest.IsArchive,
		e.options.OptimizedFetch,
		e.options.RetryFetches,
		true, // prune
		e.sshProxy,
		e.manifest.CloneFilter,
		e.manifest.PartialCloneExclude,
	)
	
	if !success && !e.options.Quiet {
		fmt.Printf("错误: 无法从 %s 获取 %s\n", project.RemoteURL, project.Name)
	}
	
	return success
}

// gcProjects 对项目执行垃圾回收
func (e *Engine) gcProjects(projects []*project.Project) {
    // 检查是否需要执行垃圾回收
    needGC := false
    for _, project := range projects {
        if project.NeedGC {
            needGC = true
            break
        }
    }
    
    if !needGC {
        return
    }
    
    if !e.options.Quiet {
        fmt.Println("Garbage collecting...")
    }
    
    // 执行垃圾回收
    for _, project := range projects {
        if project.NeedGC {
            project.GC()
        }
    }
}

// getFetchTime 获取项目的获取时间
func (e *Engine) getFetchTime(project *project.Project) float64 {
    e.fetchTimesLock.Lock()
    defer e.fetchTimesLock.Unlock()
    
    if time, ok := e.fetchTimes[project.Name]; ok {
        return time
    }
    return 0
}

// setFetchTime 设置项目的获取时间
func (e *Engine) setFetchTime(project *project.Project, duration time.Duration) {
    e.fetchTimesLock.Lock()
    defer e.fetchTimesLock.Unlock()
    
    e.fetchTimes[project.Name] = duration.Seconds()
}

// postRepoFetch 处理仓库项目获取后的操作
func (e *Engine) postRepoFetch(repoProject *project.Project) {
    // 更新仓库项目的最后获取时间
    repoProject.LastFetch = time.Now()
    
    // 保存最后获取时间
    filePath := filepath.Join(e.manifest.Subdir, ".repo_fetchtimes.json")
    data, err := json.Marshal(map[string]time.Time{
        "repo": repoProject.LastFetch,
    })
    
    if err == nil {
        os.WriteFile(filePath, data, 0644)
    }
}