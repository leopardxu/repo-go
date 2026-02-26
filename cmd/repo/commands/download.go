package commands

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/spf13/cobra"
)

// DownloadOptions holds the options for the download command
type DownloadOptions struct {
	CherryPick bool
	Revert     bool
	FFOnly     bool
	Verbose    bool
	Quiet      bool
	Jobs       int
	Config     *config.Config
	CommonManifestOptions
}

// DownloadCmd creates the download command
func DownloadCmd() *cobra.Command {
	opts := &DownloadOptions{}
	cmd := &cobra.Command{
		Use:   "download [<project>...] [<change>...]",
		Short: "Download project changes from the remote server",
		Long:  `Downloads changes for the specified projects from their remote repositories. If change IDs are provided, they will be cherry-picked.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDownload(opts, args)
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&opts.CherryPick, "cherry-pick", "c", false, "download and cherry-pick specific changes")
	cmd.Flags().BoolVarP(&opts.Revert, "revert", "r", false, "download and revert specific changes")
	cmd.Flags().BoolVarP(&opts.FFOnly, "ff-only", "f", false, "only allow fast-forward when merging")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// loadDownloadConfig loads the configuration
func loadDownloadConfig() (*config.Config, error) {
	return config.Load()
}

// loadDownloadManifest loads the manifest file
func loadDownloadManifest(cfg *config.Config) (*manifest.Manifest, error) {
	parser := manifest.NewParser()
	return parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
}

// getDownloadProjects gets the list of projects to download
func getDownloadProjects(manager *project.Manager, projectNames []string) ([]*project.Project, error) {
	if len(projectNames) == 0 {
		return manager.GetProjectsInGroups(nil)
	}
	return manager.GetProjectsByNames(projectNames)
}

// downloadResult represents the result of a download operation
type downloadResult struct {
	Name string
	Err  error
}

// downloadStats tracks download statistics
type downloadStats struct {
	mu      sync.Mutex
	Success int
	Failed  int
}

// changeInfo represents a Gerrit change to download
type ChangeInfo struct {
	Project     string `json:"project"`
	Branch      string `json:"branch"`
	ChangeID    string `json:"id"`
	Subject     string `json:"subject"`
	Status      string `json:"status"`
	URL         string `json:"url"`
	CreatedOn   int    `json:"created_on"`
	LastUpdated int    `json:"last_updated"`
	Number      int    `json:"number"`
	PatchID     string
}

// parseChangeArg parses a change argument in the format "<change-id>[/<patch-id>]"
func parseChangeArg(arg string) (*ChangeInfo, error) {

	parts := strings.Split(arg, "/")
	changeID := parts[0]
	patchID := ""
	if len(parts) > 1 {
		patchID = parts[1]
	}

	return &ChangeInfo{
		ChangeID: changeID,
		PatchID:  patchID,
	}, nil
}

// isChangeArg checks if an argument is a change ID
func isChangeArg(arg string) bool {
	// 检查是否是change-id格式
	changeIDPattern := regexp.MustCompile(`^([a-zA-Z0-9_-]+~[a-zA-Z0-9_-]+~)?I[0-9a-f]{40}(/\d+)?$`)
	if changeIDPattern.MatchString(arg) {
		return true
	}

	// 检查是否是数字格式的change number
	changeNumberPattern := regexp.MustCompile(`^\d+(/\d+)?$`)
	return changeNumberPattern.MatchString(arg)
}

// runDownload executes the download command
func runDownload(opts *DownloadOptions, args []string) error {
	// 初始化日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	// 加载配置
	fmt.Println("Loading configuration")
	cfg, err := loadDownloadConfig()
	if err != nil {
		log.Error("Failed to load config: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}
	opts.Config = cfg

	// 加载清单
	log.Debug("Parsing manifest file")
	mf, err := loadDownloadManifest(cfg)
	if err != nil {
		log.Error("Failed to parse manifest: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	log.Debug("Creating project manager")
	manager := project.NewManagerFromManifest(mf, cfg)

	// 分离项目名称和变更ID
	projectNames := []string{}
	changes := []*ChangeInfo{}

	for _, arg := range args {
		if isChangeArg(arg) {
			change, err := parseChangeArg(arg)
			if err != nil {
				log.Error("Failed to parse change argument: %v", err)
				return fmt.Errorf("failed to parse change argument: %w", err)
			}
			changes = append(changes, change)
		} else {
			projectNames = append(projectNames, arg)
		}
	}

	// 如果有变更ID，执行cherry-pick
	if len(changes) > 0 {
		return downloadAndCherryPickChanges(opts, manager, changes, projectNames)
	}

	// 否则执行普通的fetch
	return downloadProjects(opts, manager, projectNames)
}

// downloadProjects downloads the specified projects
func downloadProjects(opts *DownloadOptions, manager *project.Manager, projectNames []string) error {
	log := logger.Global

	// 获取要处理的项目
	log.Debug("Getting projects to download")
	projects, err := getDownloadProjects(manager, projectNames)
	if err != nil {
		log.Error("Failed to get projects: %v", err)
		return fmt.Errorf("failed to get projects: %w", err)
	}

	log.Info("Downloading %d project(s)", len(projects))

	// 设置并发控制
	maxConcurrency := opts.Jobs
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}

	// 创建通道和等待组
	sem := make(chan struct{}, maxConcurrency)
	results := make(chan downloadResult, len(projects))
	var wg sync.WaitGroup
	stats := downloadStats{}

	// 并发下载项目
	for _, p := range projects {
		wg.Add(1)
		go func(proj *project.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			log.Debug("Downloading project %s", proj.Name)

			// 先获取远程更新
			_, err := proj.GitRepo.RunCommand("fetch", "--prune")
			if err != nil {
				results <- downloadResult{Name: proj.Name, Err: fmt.Errorf("fetch failed: %w", err)}
				return
			}

			// 获取当前分支
			branchOutput, err := proj.GitRepo.RunCommand("rev-parse", "--abbrev-ref", "HEAD")
			if err != nil {
				results <- downloadResult{Name: proj.Name, Err: fmt.Errorf("get current branch failed: %w", err)}
				return
			}
			currentBranch := strings.TrimSpace(string(branchOutput))

			// 如果当前分支不是HEAD（分离状态），则尝试pull更新
			if currentBranch != "HEAD" {
				// 获取远程跟踪分支
				remoteOutput, err := proj.GitRepo.RunCommand("config", "--get", fmt.Sprintf("branch.%s.remote", currentBranch))
				if err == nil {
					remote := strings.TrimSpace(string(remoteOutput))
					if remote != "" {
						// 尝试merge远程分支的更新
						_, err = proj.GitRepo.RunCommand("merge", fmt.Sprintf("%s/%s", remote, currentBranch), "--ff-only")
						if err != nil {
							// fast-forward失败，可能是因为有本地提交，尝试普通merge
							_, err = proj.GitRepo.RunCommand("merge", fmt.Sprintf("%s/%s", remote, currentBranch))
							if err != nil {
								log.Warn("Failed to merge %s/%s into %s: %v", remote, currentBranch, proj.Name, err)
								// fetch成功也算成功，merge失败只记录警告
							}
						}
					}
				}
			}

			results <- downloadResult{Name: proj.Name, Err: nil}
		}(p)
	}

	// 关闭结果通道
	go func() {
		wg.Wait()
		close(results)
	}()

	// 处理结果
	for res := range results {
		if res.Err != nil {
			log.Error("Error downloading for %s: %v", res.Name, res.Err)
			stats.mu.Lock()
			stats.Failed++
			stats.mu.Unlock()
		} else {
			log.Info("Downloaded for %s", res.Name)
			stats.mu.Lock()
			stats.Success++
			stats.mu.Unlock()
		}
	}

	// 输出统计信息
	log.Info("Download complete. Success: %d, Failed: %d", stats.Success, stats.Failed)

	// 如果有失败的项目，返回错
	if stats.Failed > 0 {
		return fmt.Errorf("%d projects failed to download", stats.Failed)
	}

	return nil
}

// downloadAndCherryPickChanges downloads and cherry-picks the specified changes
func downloadAndCherryPickChanges(opts *DownloadOptions, manager *project.Manager, changes []*ChangeInfo, projectNames []string) error {
	log := logger.Global

	// 每次只支持一个项目的一个 changeID/patchID 的操作
	if len(changes) > 1 {
		log.Warn("当前只支持每次处理一个变更，将只处理第一个变更: %s", changes[0].ChangeID)
	}

	log.Info("Downloading and cherry-picking change")

	// 只处理第一个变更
	if len(changes) > 0 {
		change := changes[0]
		log.Info("Processing change %s (patch %s)", change.ChangeID, change.PatchID)

		// 获取变更详细信息
		changeDetail, err := change.GetChangeDetail(change.ChangeID, change.PatchID)
		if err != nil {
			log.Error("Failed to get change details: %v", err)
			return fmt.Errorf("failed to get change details: %w", err)
		}

		// 设置项目名称
		change.Project = changeDetail.Project
		log.Info("Change belongs to project: %s", change.Project)

		// 获取项目
		proj := manager.GetProject(change.Project)
		if proj == nil {
			log.Error("Project not found: %s", change.Project)
			return fmt.Errorf("project not found: %s", change.Project)
		}

		// 构建修订版本引用
		var ref string
		if change.PatchID != "" {
			// 如果指定了补丁ID，使用refs/changes/xx/CHANGEID/PATCHID格式
			lastTwo := change.ChangeID
			if len(lastTwo) > 2 {
				lastTwo = lastTwo[len(lastTwo)-2:]
			}
			ref = fmt.Sprintf("refs/changes/%s/%s/%s", lastTwo, change.ChangeID, change.PatchID)
		} else {
			// 否则使用refs/changes/xx/CHANGEID/1格式（默认第一个补丁集）
			lastTwo := change.ChangeID
			if len(lastTwo) > 2 {
				lastTwo = lastTwo[len(lastTwo)-2:]
			}
			ref = fmt.Sprintf("refs/changes/%s/%s/1", lastTwo, change.ChangeID)
		}

		// 获取远程名称
		remoteName := proj.RemoteName

		// 获取修订版本
		log.Info("Fetching %s from %s", ref, remoteName)
		_, err = proj.GitRepo.RunCommand("fetch", remoteName, ref)
		if err != nil {
			log.Error("Failed to fetch change: %v", err)
			return fmt.Errorf("failed to fetch change: %w", err)
		}

		// 执行cherry-pick
		log.Info("Cherry-picking FETCH_HEAD")
		_, err = proj.GitRepo.RunCommand("cherry-pick", "FETCH_HEAD")
		if err != nil {
			// 检查是否是空提交的情况
			// 获取cherry-pick的状态
			statusOutput, statusErr := proj.GitRepo.RunCommand("status", "--porcelain")
			if statusErr == nil && len(statusOutput) == 0 {
				// 工作区干净，可能是空提交，尝试跳过
				log.Warn("Cherry-pick resulted in empty commit, skipping")
				_, skipErr := proj.GitRepo.RunCommand("cherry-pick", "--skip")
				if skipErr != nil {
					log.Error("Failed to skip empty cherry-pick: %v", skipErr)
					return fmt.Errorf("failed to cherry-pick change (empty commit): %w", err)
				}
				log.Info("Skipped empty cherry-pick for change %s", change.ChangeID)
			} else {
				log.Error("Failed to cherry-pick change: %v", err)
				return fmt.Errorf("failed to cherry-pick change: %w", err)
			}
		}

		log.Info("Successfully cherry-picked change %s to project %s", change.ChangeID, change.Project)
	} else {
		log.Error("No changes specified for cherry-pick")
		return fmt.Errorf("no changes specified for cherry-pick")
	}

	return nil
}

// GetChangeDetail 获取变更详细信息，包括修订版本信息
func (c *ChangeInfo) GetChangeDetail(changeID string, patchID string) (*ChangeInfo, error) {
	logger.Debug("获取变更详细信息: %s, 补丁ID: %s", changeID, patchID)

	// 尝试通过SSH协议获取变更信息
	change, err := c.getChangeViaSSH(changeID)
	if err != nil {
		return nil, err
	}

	return change, nil
}

// getChangeViaSSH 通过SSH协议获取变更信息
func (c *ChangeInfo) getChangeViaSSH(changeID string) (*ChangeInfo, error) {
	logger.Debug("通过SSH协议获取变更信息: %s", changeID)

	cmd := exec.Command("ssh", "gerrit", "gerrit", "query", "--format=JSON", changeID)
	output, err := cmd.Output()
	if err != nil {
		// 提供更明确的错误信息，指导用户如何手动执行SSH命令
		return nil, fmt.Errorf("执行SSH命令失败: %w，请使用 ssh 协议 ssh gerrit gerrit query --format=JSON %s 输出json，然后执行后续逻辑", err, changeID)
	}

	// 解析JSON输出
	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("SSH命令没有输出")
	}

	// 第一行包含变更信息
	var change ChangeInfo
	if err := json.Unmarshal([]byte(lines[0]), &change); err != nil {
		return nil, fmt.Errorf("解析SSH输出失败: %w", err)
	}

	// 检查是否找到变更
	if change.ChangeID == "" {
		return nil, fmt.Errorf("未找到变更: %s", changeID)
	}

	return &change, nil
}
