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

// StatusOptions 包含status命令的选项
type StatusOptions struct {
	CommonManifestOptions
	Jobs              int
	Orphans           bool
	Quiet             bool
	Verbose           bool
	Branch            bool
	Config            *config.Config
}

// statusStats 用于统计状态检查结果
type statusStats struct {
	mu      sync.Mutex
	total   int
	success int
	failed  int
}

// increment 增加统计计数
func (s *statusStats) increment(success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if success {
		s.success++
	} else {
		s.failed++
	}
}

// StatusCmd 返回status命令
func StatusCmd() *cobra.Command {
	opts := &StatusOptions{
		Jobs: runtime.NumCPU() * 2,
	}

	cmd := &cobra.Command{
		Use:   "status [<project>...]",
		Short: "Show the working tree status",
		Long:  `Show the status of the working tree. This includes projects with uncommitted changes, projects with unpushed commits, and projects on different branches than specified in the manifest.`,
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
			
			return runStatus(opts, args, log)
		},
	}

	// 添加命令行选项
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of jobs to run in parallel (default: based on number of CPU cores)")
	cmd.Flags().BoolVarP(&opts.Orphans, "orphans", "o", false, "include objects in working directory outside of repo projects")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output including debug logs")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runStatus 执行status命令
func runStatus(opts *StatusOptions, args []string, log logger.Logger) error {
	// 创建统计对象
	stats := &statusStats{}
	
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Error("加载配置失败: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}
	opts.Config = cfg

	// 加载清单
	log.Debug("正在加载清单文件: %s", cfg.ManifestName)
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
	if err != nil {
		log.Error("解析清单失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	log.Debug("成功加载清单，包含 %d 个项目", len(manifest.Projects))

	// 创建项目管理器
	log.Debug("正在初始化项目管理器...")
	manager := project.NewManagerFromManifest(manifest, cfg)

	// 获取要处理的项目
	var projects []*project.Project

	if len(args) == 0 {
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
		log.Debug("根据名称获取项目: %v", args)
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			log.Error("根据名称获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
		log.Debug("共获取到 %d 个项目", len(projects))
	}

	// 使用goroutine池并发获取项目状态
	log.Info("开始检查项目状态，并行任务数: %d...", opts.Jobs)
	
	type statusResult struct {
		Project *project.Project
		Status  string
		Err     error
	}

	var wg sync.WaitGroup
	results := make(chan statusResult, len(projects))
	sem := make(chan struct{}, opts.Jobs) // 使用信号量控制并发数

	for _, p := range projects {
		p := p // 创建副本避免闭包问题
		wg.Add(1)
		
		go func() {
			defer wg.Done()
			sem <- struct{}{} // 获取信号量
			defer func() { <-sem }() // 释放信号量
			
			log.Debug("正在检查项目 %s 的状态...", p.Name)
			
			status, err := p.GetStatus()
			if err != nil {
				log.Error("获取项目 %s 状态失败: %v", p.Name, err)
				stats.increment(false)
			} else {
				stats.increment(true)
				log.Debug("项目 %s 状态检查完成", p.Name)
			}
			
			results <- statusResult{
				Project: p,
				Status:  status,
				Err:     err,
			}
		}()
	}

	// 启动一个 goroutine 来关闭结果通道
	go func() {
		wg.Wait()
		close(results)
	}()

	// 处理结果
	var errs []error
	for res := range results {
		if res.Err != nil {
			errs = append(errs, fmt.Errorf("项目 %s: %w", res.Project.Name, res.Err))
			continue
		}
		
		log.Info("项目 %s: %s", res.Project.Name, res.Status)
	}

	// 显示统计信息
	log.Info("状态检查操作完成，总计: %d，成功: %d，失败: %d", stats.total, stats.success, stats.failed)

	// 如果有错误，返回汇总错误
	if len(errs) > 0 {
		log.Error("有 %d 个项目状态检查失败", len(errs))
		return fmt.Errorf("%d projects failed: %v", len(errs), errors.Join(errs...))
	}

	return nil
}