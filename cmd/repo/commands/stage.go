package commands

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/logger"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// StageOptions 包含stage命令的选项
type StageOptions struct {
	All             bool
	Interactive     bool
	Verbose         bool
	Quiet           bool
	OuterManifest   bool
	NoOuterManifest bool
	ThisManifestOnly bool
	Patch           bool
	Edit            bool
	Force           bool
	Jobs            int
	Config          *config.Config
	CommonManifestOptions
}

// stageStats 用于统计暂存结果
type stageStats struct {
	mu      sync.Mutex
	total   int
	success int
	failed  int
}

// increment 增加统计计数
func (s *stageStats) increment(success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if success {
		s.success++
	} else {
		s.failed++
	}
}

// StageCmd 返回stage命令
func StageCmd() *cobra.Command {
	opts := &StageOptions{
		Jobs: runtime.NumCPU() * 2,
	}

	cmd := &cobra.Command{
		Use:   "stage [<project>...] [<file>...]",
		Short: "Stage file contents to the index",
		Long:  `Stage file contents to the index (equivalent to 'git add').`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 创建日志记录器
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
			
			return runStage(opts, args, log)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.All, "all", "A", false, "stage all files")
	cmd.Flags().BoolVarP(&opts.Interactive, "interactive", "i", false, "interactive staging")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output including debug logs")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")
	cmd.Flags().BoolVarP(&opts.Patch, "patch", "p", false, "select hunks interactively")
	cmd.Flags().BoolVarP(&opts.Edit, "edit", "e", false, "edit current diff and apply")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "allow adding otherwise ignored files")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of jobs to run in parallel (default: based on number of CPU cores)")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runStage 执行stage命令
func runStage(opts *StageOptions, args []string, log logger.Logger) error {
	// 创建统计对象
	stats := &stageStats{}
	
	if len(args) == 0 && !opts.All {
		log.Error("未指定文件且未使用--all选项")
		return fmt.Errorf("no files specified and --all not used")
	}

	log.Info("开始暂存文件")

	// 加载清单
	log.Debug("正在加载清单文件: %s", opts.Config.ManifestName)
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName, strings.Split(opts.Config.Groups, ","))
	if err != nil {
		log.Error("解析清单失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	log.Debug("成功加载清单，包含 %d 个项目", len(manifest.Projects))

	// 创建项目管理器
	log.Debug("正在初始化项目管理器...")
	manager := project.NewManagerFromManifest(manifest, opts.Config)

	// 确定文件和项目列表
	var files []string
	var projectNames []string

	// 解析参数，区分项目名和文件名
	log.Debug("解析命令行参数...")
	if len(args) > 0 {
		// 检查第一个参数是否是项目名
		projects, err := manager.GetProjectsByNames([]string{args[0]})
		if err == nil && len(projects) > 0 {
			// 第一个参数是项目名
			projectNames = []string{args[0]}
			if len(args) > 1 {
				files = args[1:]
			}
			log.Debug("指定项目: %s, 文件数量: %d", args[0], len(files))
		} else {
			// 所有参数都是文件名
			files = args
			log.Debug("未指定项目，文件数量: %d", len(files))
		}
	}

	// 获取要处理的项目
	var projects []*project.Project
	if len(projectNames) == 0 {
		// 如果没有指定项目，则处理所有项目
		log.Debug("获取所有项目...")
		projects, err = manager.GetProjectsInGroups(nil)
		if err != nil {
			log.Error("获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
		log.Debug("共获取到 %d 个项目", len(projects))
	} else {
		// 否则，只处理指定的项目
		log.Debug("根据名称获取项目: %v", projectNames)
		projects, err = manager.GetProjectsByNames(projectNames)
		if err != nil {
			log.Error("根据名称获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
		log.Debug("共获取到 %d 个项目", len(projects))
	}

	// 构建stage命令选项（实际上是git add命令）
	log.Debug("构建git add命令参数...")
	stageArgs := []string{"add"}
	
	if opts.All {
		stageArgs = append(stageArgs, "--all")
	}
	
	if opts.Interactive {
		stageArgs = append(stageArgs, "--interactive")
	}
	
	if opts.Patch {
		stageArgs = append(stageArgs, "--patch")
	}
	
	if opts.Edit {
		stageArgs = append(stageArgs, "--edit")
	}
	
	if opts.Force {
		stageArgs = append(stageArgs, "--force")
	}
	
	if opts.Verbose {
		stageArgs = append(stageArgs, "--verbose")
	}

	// 添加文件参数
	if len(files) > 0 {
		stageArgs = append(stageArgs, files...)
	}

	// 使用goroutine池并发执行stage
	log.Info("开始暂存文件，并行任务数: %d...", opts.Jobs)
	
	var wg sync.WaitGroup
	errChan := make(chan error, len(projects))
	resultChan := make(chan string, len(projects))
	sem := make(chan struct{}, opts.Jobs) // 使用信号量控制并发数

	for _, p := range projects {
		p := p // 创建副本避免闭包问题
		wg.Add(1)
		
		go func() {
			defer wg.Done()
			sem <- struct{}{} // 获取信号量
			defer func() { <-sem }() // 释放信号量
			
			log.Debug("在项目 %s 中执行git add命令...", p.Name)
			outputBytes, err := p.GitRepo.RunCommand(stageArgs...)
			if err != nil {
				log.Error("项目 %s 暂存失败: %v", p.Name, err)
				errChan <- fmt.Errorf("project %s: %w", p.Name, err)
				stats.increment(false)
				return
			}
			
			output := strings.TrimSpace(string(outputBytes))
			if output != "" {
				resultChan <- fmt.Sprintf("项目 %s:\n%s", p.Name, output)
			} else {
				resultChan <- fmt.Sprintf("项目 %s: 文件暂存成功", p.Name)
			}
			stats.increment(true)
			log.Debug("项目 %s 暂存完成", p.Name)
		}()
	}

	// 启动一个 goroutine 来关闭结果通道
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
	log.Info("暂存操作完成，总计: %d，成功: %d，失败: %d", stats.total, stats.success, stats.failed)

	// 如果有错误，返回汇总错误
	if len(errs) > 0 {
		log.Error("有 %d 个项目暂存失败", len(errs))
		return fmt.Errorf("%d projects failed: %v", len(errs), errors.Join(errs...))
	}

	return nil
}