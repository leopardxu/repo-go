package commands

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/spf13/cobra"
)

// StartOptions 包含start命令的选项
type StartOptions struct {
	All              bool
	Rev              string
	Branch           string
	Jobs             int
	Verbose          bool
	Quiet            bool
	OuterManifest    bool
	NoOuterManifest  bool
	ThisManifestOnly bool
	HEAD             bool
	Config           *config.Config
	CommonManifestOptions
}

// startStats 用于统计分支创建结果
type startStats struct {
	mu      sync.Mutex
	total   int
	success int
	failed  int
}

// increment 增加统计计数
func (s *startStats) increment(success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if success {
		s.success++
	} else {
		s.failed++
	}
}

// StartCmd 返回start命令
func StartCmd() *cobra.Command {
	opts := &StartOptions{
		Jobs: runtime.NumCPU() * 2,
	}

	cmd := &cobra.Command{
		Use:   "start <branch_name> [<project>...]",
		Short: "Start a new branch for development",
		Long:  `Create a new branch for development based on the current manifest.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 创建日志记录�?
			log := logger.NewDefaultLogger()
			
			// 根据选项设置日志级别
			if opts.Quiet {
				log.SetLevel(logger.LogLevelError)
			} else if opts.Verbose {
				log.SetLevel(logger.LogLevelDebug)
			} else {
				log.SetLevel(logger.LogLevelInfo)
			}
			
			// 加载配置
			cfg, err := config.Load()
			if err != nil {
				log.Error("加载配置失败: %v", err)
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg
			
			return runStart(opts, args, log)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVar(&opts.All, "all", false, "start branch in all projects")
	cmd.Flags().StringVarP(&opts.Rev, "rev", "r", "", "start branch from the specified revision")
	cmd.Flags().StringVarP(&opts.Branch, "branch", "b", "", "specify an alternate branch name")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of jobs to run in parallel (default: based on number of CPU cores)")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output including debug logs")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)
	cmd.Flags().BoolVar(&opts.HEAD, "HEAD", false, "abbreviation for --rev HEAD")

	return cmd
}

// runStart 执行start命令
func runStart(opts *StartOptions, args []string, log logger.Logger) error {
	// 创建统计对象
	stats := &startStats{}
	
	if opts.HEAD {
		opts.Rev = "HEAD"
	}
	
	// 获取分支名称
	branchName := args[0]
	if opts.Branch != "" {
		branchName = opts.Branch
	}

	// 获取项目列表
	projectNames := args[1:]

	log.Info("开始创建分�?'%s'", branchName)

	// 加载清单
	log.Debug("正在加载清单文件: %s", opts.Config.ManifestName)
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName, strings.Split(opts.Config.Groups, ","))
	if err != nil {
		log.Error("解析清单失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	log.Debug("成功加载清单，包�?%d 个项�?, len(manifest.Projects))

	// 创建项目管理�?
	log.Debug("正在初始化项目管理器...")
	manager := project.NewManagerFromManifest(manifest, opts.Config)

	// 获取要处理的项目
	var projects []*project.Project
	if opts.All || len(projectNames) == 0 {
		// 如果指定�?-all或没有指定项目，则处理所有项�?
		log.Debug("获取所有项�?..")
		projects, err = manager.GetProjectsInGroups(nil)
		if err != nil {
			log.Error("获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
		log.Debug("共获取到 %d 个项�?, len(projects))
	} else {
		// 否则，只处理指定的项�?
		log.Debug("根据名称获取项目: %v", projectNames)
		projects, err = manager.GetProjectsByNames(projectNames)
		if err != nil {
			log.Error("根据名称获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
		log.Debug("共获取到 %d 个项�?, len(projects))
	}

	// 使用goroutine池并发创建分�?
	log.Info("开始创建分支，并行任务�? %d...", opts.Jobs)
	
	var wg sync.WaitGroup
	errChan := make(chan error, len(projects))
	resultChan := make(chan string, len(projects))
	sem := make(chan struct{}, opts.Jobs) // 使用信号量控制并发数

	for _, p := range projects {
		p := p // 创建副本避免闭包问题
		wg.Add(1)
		
		go func() {
			defer wg.Done()
			sem <- struct{}{} // 获取信号�?
			defer func() { <-sem }() // 释放信号�?
			
			log.Debug("在项�?%s 中创建分�?'%s'...", p.Name, branchName)
			
			// 确定使用的修订版�?
			revision := opts.Rev
			if revision == "" {
				revision = p.Revision
			}
			log.Debug("项目 %s 使用修订版本: %s", p.Name, revision)
			
			// 创建分支
			if err := p.GitRepo.CreateBranch(branchName, revision); err != nil {
				log.Error("项目 %s 创建分支失败: %v", p.Name, err)
				errChan <- fmt.Errorf("project %s: %w", p.Name, err)
				stats.increment(false)
				return
			}
			
			resultChan <- fmt.Sprintf("项目 %s: 分支 '%s' 创建成功", p.Name, branchName)
			stats.increment(true)
			log.Debug("项目 %s 分支创建完成", p.Name)
		}()
	}

	// 启动一�?goroutine 来关闭结果通道
	go func() {
		wg.Wait()
		close(errChan)
		close(resultChan)
	}()

	// 处理错误
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	// 输出结果
	for result := range resultChan {
		log.Info(result)
	}

	// 显示统计信息
	log.Info("分支创建操作完成，总计: %d，成�? %d，失�? %d", stats.total, stats.success, stats.failed)

	// 如果有错误，返回汇总错�?
	if len(errs) > 0 {
		log.Error("�?%d 个项目创建分支失�?, len(errs))
		return fmt.Errorf("%d projects failed: %v", len(errs), errors.Join(errs...))
	}

	return nil
}
