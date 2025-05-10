package commands

import (
	"fmt"
	"strings"
	"sync"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/logger"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// DiffOptions holds the options for the diff command
type DiffOptions struct {
	Quiet   bool
	Verbose bool
	Config  *config.Config
	CommonManifestOptions
}

// 加载配置
func loadConfig() (*config.Config, error) {
	return config.Load()
}

// 解析清单
func loadManifest(cfg *config.Config) (*manifest.Manifest, error) {
	parser := manifest.NewParser()
	return parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
}

// 获取项目列表
func getProjects(manager *project.Manager, projectNames []string) ([]*project.Project, error) {
	if len(projectNames) == 0 {
		return manager.GetProjectsInGroups(nil)
	}
	return manager.GetProjectsByNames(projectNames)
}

// 并发执行diff操作
type diffResult struct {
	Name   string
	Output string
	Err    error
}

func runDiff(opts *DiffOptions, projectNames []string) error {
	// 初始化日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	log.Debug("加载配置")
	cfg, err := loadConfig()
	if err != nil {
		log.Error("加载配置失败: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}
	opts.Config = cfg

	log.Debug("解析清单文件")
	mf, err := loadManifest(cfg)
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	log.Debug("获取项目管理器")
	manager := project.NewManagerFromManifest(mf, cfg)
	log.Debug("获取项目列表")
	projects, err := getProjects(manager, projectNames)
	if err != nil {
		log.Error("获取项目列表失败: %v", err)
		return fmt.Errorf("failed to get projects: %w", err)
	}

	log.Info("开始对 %d 个项目执行 diff 操作", len(projects))

	maxConcurrency := 8
	sem := make(chan struct{}, maxConcurrency)
	results := make(chan diffResult, len(projects))
	var wg sync.WaitGroup

	// 并发执行diff操作
	for _, p := range projects {
		wg.Add(1)
		sem <- struct{}{}
		go func(proj *project.Project) {
			defer wg.Done()
			defer func() { <-sem }()
			log.Debug("对项目 %s 执行 diff 操作", proj.Name)
			outBytes, err := proj.GitRepo.RunCommand("diff")
			out := string(outBytes)
			results <- diffResult{Name: proj.Name, Output: out, Err: err}
		}(p)
	}

	// 等待所有diff操作完成并关闭结果通道
	go func() {
		wg.Wait()
		close(results)
	}()

	// 处理结果
	successCount := 0
	errorCount := 0

	for res := range results {
		if res.Err != nil {
			errorCount++
			log.Error("项目 %s 执行 diff 失败: %v", res.Name, res.Err)
			continue
		}

		successCount++
		if res.Output != "" {
			log.Info("--- %s ---\n%s", res.Name, res.Output)
		} else if !opts.Quiet {
			log.Info("--- %s ---\n(无变更)", res.Name)
		}
	}

	log.Info("diff 操作完成: %d 成功, %d 失败", successCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("diff failed for %d projects", errorCount)
	}

	return nil
}

// DiffCmd creates the diff command
func DiffCmd() *cobra.Command {
	opts := &DiffOptions{}
	cmd := &cobra.Command{
		Use:   "diff [<project>...]",
		Short: "Show changes between commit, working tree, etc",
		Long:  `Shows changes between the working tree and the index or a commit.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(opts, args)
		},
	}

	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)
	return cmd
}