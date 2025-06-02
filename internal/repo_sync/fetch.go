package repo_sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/leopardxu/repo-go/internal/progress"
	"github.com/leopardxu/repo-go/internal/project" // Keep this import
)

// Removed the sync.Project struct definition

// fetchMain 执行网络获取
func (e *Engine) fetchMain(projects []*project.Project) error { // projects parameter is likely the hyperSyncProjects list or nil

	// Get the full list of projects managed by the engine
	allManagedProjects, err := e.getProjects()
	if err != nil {
		// Handle error getting projects if necessary, though getProjects caches it
		return fmt.Errorf("failed to get managed projects in fetchMain: %w", err)
	}

	// Determine the actual list of projects to fetch
	toFetch := []*project.Project{}
	noFetch := make(map[string]bool)

	// Decide which projects to fetch based on HyperSync or fetching all
	if e.options.HyperSync && len(projects) > 0 { // 'projects' here are the hyperSyncProjects
		toFetch = append(toFetch, projects...)
		for _, p := range projects {
			noFetch[p.Gitdir] = true // Mark hyper-synced projects to avoid redundant fetches later
		}
		// Optionally, add repo project if needed and not already in hyperSyncProjects
		// repoProj := e.findRepoProject(allManagedProjects) // Need a helper to find it
		// if repoProj != nil && !noFetch[repoProj.Gitdir] {
		//     // Check fetch time condition if needed
		//     toFetch = append(toFetch, repoProj)
		// }

	} else {
		// Fetch all managed projects if not HyperSync
		toFetch = append(toFetch, allManagedProjects...)
	}

	// Remove the specific repoProject handling based on convertManifestProject
	// The repo project should be part of allManagedProjects if it exists

	// 按照获取时间排序
	sort.Slice(toFetch, func(i, j int) bool {
		// 修改比较逻辑，使�?After �?Before 方法比较时间
		return e.getFetchTime(toFetch[i]).After(e.getFetchTime(toFetch[j]))
	})

	// 执行获取
	success, fetched := e.fetch(toFetch) // fetch already works with []*project.Project
	if !success {
		select {
		case e.errEvent <- fmt.Errorf("fetch failed"):
		default:
		}
	}

	// Update repo project fetch time if applicable
	repoProj := e.findRepoProject(allManagedProjects) // Need a helper to find it
	if repoProj != nil {
		// Ensure postRepoFetch works correctly with project.Project
		e.postRepoFetch(repoProj)
	}

	// 如果只执行网络同步，则返�?
	if e.options.NetworkOnly {
		if !success {
			return fmt.Errorf("由于获取错误退出同�?)
		}
		return nil
	}

	// 迭代获取缺失的项�?
	previouslyMissingSet := make(map[string]bool)
	for {
		// 重新加载清单
		if err := e.reloadManifest("", true, e.options.Groups); err != nil {
			return err
		}

		// 获取所有项�?(reloads manifest and projects)
		currentAllProjects, err := e.getProjects() // Use a different variable name
		if err != nil {
			return err
		}

		// 查找缺失的项�?
		missing := []*project.Project{}
		for _, p := range currentAllProjects { // Iterate over the reloaded list
			if _, ok := fetched[p.Gitdir]; !ok && !noFetch[p.Gitdir] {
				missing = append(missing, p)
			}
		}

		if len(missing) == 0 {
			return nil // Successfully fetched all required projects
		}

		// 检查是否有新的缺失项目
		missingSet := make(map[string]bool)
		for _, p := range missing {
			missingSet[p.Name] = true
		}

		// 如果缺失的项目集合没有变化，则退出循�?(avoid infinite loop)
		if reflect.DeepEqual(previouslyMissingSet, missingSet) {
			fmt.Println("Warning: Could not fetch all projects, missing set did not change.")
			break // Or return an error
		}
		previouslyMissingSet = missingSet

		// 获取缺失的项�?
		success, newFetched := e.fetch(missing)
		// 修改 errEvent 发送的类型，从 struct{}{} 改为错误类型
		if !success {
			select {
			case e.errEvent <- fmt.Errorf("fetch failed"):
			default:
			}
		}

		// 更新已获取的项目集合
		for k, v := range newFetched {
			fetched[k] = v
		}
	}

	// If the loop broke due to no change in missingSet, return an error
	if len(previouslyMissingSet) > 0 {
		return fmt.Errorf("failed to fetch all required projects")
	}

	return nil
}

// Helper function to find the repo project (implementation needed)
func (e *Engine) findRepoProject(projects []*project.Project) *project.Project {
	// Logic to identify the repository project within the list
	// This might involve checking the path or name against manifest info
	// Placeholder implementation:
	if e.manifest != nil && e.manifest.RepoProject != nil {
		repoManifestPath := e.manifest.RepoProject.Path
		for _, p := range projects {
			if p.Path == repoManifestPath {
				return p
			}
		}
	}
	return nil // Or handle error if repo project expected but not found
}

// Remove the duplicate definition below:
/*
// Ensure postRepoFetch uses project.Project
func (e *Engine) postRepoFetch(repoProject *project.Project) {
    // 更新仓库项目的最后获取时�?
    repoProject.LastFetch = time.Now()

    // 保存最后获取时�?
    // Ensure manifest.Subdir is accessible or calculated correctly
    // filePath := filepath.Join(e.manifest.Subdir, ".repo_fetchtimes.json")
    // Need to determine the correct path for storing fetch times
    // Perhaps relative to the top-level worktree or .repo directory
    // Example: filePath := filepath.Join(e.config.WorkDir, ".repo", ".repo_fetchtimes.json")
    // This needs careful consideration based on project structure.
    // For now, let's comment out the file writing part if unsure.

    // data, err := json.Marshal(map[string]time.Time{
    //     "repo": repoProject.LastFetch,
    // })
    //
    // if err == nil {
    //     // os.WriteFile(filePath, data, 0644) // Commented out until path is confirmed
    // } else {
    //     // Log error marshalling JSON?
    // }
}
*/

// fetch 执行获取操作
func (e *Engine) fetch(projects []*project.Project) (bool, map[string]bool) {
	ret := true
	fetched := make(map[string]bool)

	// 创建进度�?
	var pm progress.Reporter
	if !e.options.Quiet {
		pm = progress.New(len(projects))
	}

	// 按对象目录分组项�?
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

			// 修改 Update 调用，添加当前索引参�?
			if !e.options.Quiet && pm != nil {
				pm.Update(len(fetched), result.Project.Name)
			}
		}

		if !localRet && e.options.FailFast {
			return false
		}

		return localRet
	}

	// 执行获取
	if len(projectsList) == 1 || e.options.JobsNetwork == 1 {
		// 单线程获�?
		for _, projects := range projectsList {
			results := e.fetchProjectList(projects)
			if !processResults(results) {
				ret = false
				break
			}
		}
	} else {
		// 多线程获�?
		jobs := e.options.JobsNetwork

		// 创建工作�?
		var wg sync.WaitGroup
		resultsChan := make(chan []FetchResult, len(projectsList))

		// 限制并发�?
		semaphore := make(chan struct{}, jobs)

		for _, projects := range projectsList {
			wg.Add(1)
			go func(projects []*project.Project) {
				defer wg.Done()

				// 获取信号�?
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// 执行获取
				results := e.fetchProjectList(projects)
				resultsChan <- results
			}(projects)
		}

		// 等待所有获取完�?
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

	// 修改 End 调用�?Finish
	if !e.options.Quiet && pm != nil {
		pm.Finish()
	}

	// 执行垃圾回收
	if !e.manifest.IsArchive {
		e.gcProjects(projects)
	}

	return ret, fetched
}

// FetchResult 表示获取操作的结�?
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
		fmt.Printf("错误: 无法�?%s 获取 %s\n", project.RemoteURL, project.Name)
	}

	return success
}

// gcProjects 对项目执行垃圾回�?
func (e *Engine) gcProjects(projects []*project.Project) {
	// 检查是否需要执行垃圾回�?
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

// getFetchTime 获取项目的获取时�?
func (e *Engine) getFetchTime(project *project.Project) time.Time {
	e.fetchTimesLock.Lock()
	defer e.fetchTimesLock.Unlock()

	if time, ok := e.fetchTimes[project.Name]; ok {
		return time
	}
	return time.Time{} // 返回零值时�?
}

// setFetchTime 设置项目的获取时�?
func (e *Engine) setFetchTime(project *project.Project, duration time.Duration) {
	e.fetchTimesLock.Lock()
	defer e.fetchTimesLock.Unlock()

	e.fetchTimes[project.Name] = time.Now() // 存储当前时间而不是秒�?
}

// postRepoFetch 处理仓库项目获取后的操作
func (e *Engine) postRepoFetch(repoProject *project.Project) {
	// 更新仓库项目的最后获取时�?
	repoProject.LastFetch = time.Now()

	// 保存最后获取时�?
	filePath := filepath.Join(e.manifest.Subdir, ".repo_fetchtimes.json")
	data, err := json.Marshal(map[string]time.Time{
		"repo": repoProject.LastFetch,
	})

	if err == nil {
		os.WriteFile(filePath, data, 0644)
	}
}
