package commands

import (
	"fmt"
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
		Use:   "download [<project>...]",
		Short: "Download project changes from the remote server",
		Long:  `Downloads changes for the specified projects from their remote repositories.`,
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

// runDownload executes the download command
func runDownload(opts *DownloadOptions, projectNames []string) error {
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
	log.Debug("Loading configuration")
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
			_, err := proj.GitRepo.RunCommand("fetch", "--prune")
			results <- downloadResult{Name: proj.Name, Err: err}
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
