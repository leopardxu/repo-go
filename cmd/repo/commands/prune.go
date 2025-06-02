package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/git"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/spf13/cobra"
)

// PruneOptions 包含prune命令的选项
type PruneOptions struct {
	Force            bool
	DryRun           bool
	Verbose          bool
	Quiet            bool
	Jobs             int
	OuterManifest    bool
	NoOuterManifest  bool
	ThisManifestOnly bool
}

// pruneStats 用于统计prune命令的执行结果
type pruneStats struct {
	mu      sync.Mutex
	success int
	failed  int
	total   int
}

// PruneCmd 返回prune命令
func PruneCmd() *cobra.Command {
	opts := &PruneOptions{}

	cmd := &cobra.Command{
		Use:   "prune [<project>...]",
		Short: "Prune (delete) already merged topics",
		Long:  `Prune (delete) already merged topics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "force pruning even if there are local changes")
	cmd.Flags().BoolVarP(&opts.DryRun, "dry-run", "n", false, "don't actually prune, just show what would be pruned")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runPrune 执行prune命令
func runPrune(opts *PruneOptions, args []string) error {
	// 初始化日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	log.Info("开始清理不在清单中的项目")

	// 加载配置
	log.Debug("正在加载配置...")
	cfg, err := config.Load()
	if err != nil {
		log.Error("加载配置失败: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 加载清单
	log.Debug("正在解析清单文件...")
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理�?
	log.Debug("正在创建项目管理�?..")
	manager := project.NewManagerFromManifest(manifestObj, cfg)

	var projects []*project.Project

	// 获取项目列表
	log.Debug("正在获取项目列表...")
	if len(args) == 0 {
		log.Debug("获取所有项目")
		projects, err = manager.GetProjectsInGroups(nil)
		if err != nil {
			log.Error("获取所有项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		log.Debug("获取指定的项�? %v", args)
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			log.Error("获取指定项目失败: %v", err)
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
	}

	// 创建项目路径映射
	log.Debug("创建项目路径映射...")
	projectPaths := make(map[string]bool)
	for _, p := range projects {
		projectPaths[p.Path] = true
	}

	// 获取工作目录中的所有目�?
	log.Debug("获取工作目录...")
	workDir, err := os.Getwd()
	if err != nil {
		log.Error("获取工作目录失败: %v", err)
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	log.Debug("读取工作目录内容...")
	entries, err := os.ReadDir(workDir)
	if err != nil {
		log.Error("读取工作目录失败: %v", err)
		return fmt.Errorf("failed to read working directory: %w", err)
	}

	// 查找不在清单中的项目
	log.Debug("查找不在清单中的项目...")
	var prunedProjects []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// 跳过.repo目录
		if entry.Name() == ".repo" {
			continue
		}

		// 检查目录是否是Git仓库
		gitDir := filepath.Join(workDir, entry.Name(), ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			continue
		}

		// 检查目录是否在清单�?
		if !projectPaths[entry.Name()] {
			prunedProjects = append(prunedProjects, entry.Name())
		}
	}

	// 如果没有要删除的项目，直接返�?
	if len(prunedProjects) == 0 {
		log.Info("没有需要清理的项目")
		return nil
	}

	// 显示要删除的项目
	log.Info("找到 %d 个需要清理的项目:", len(prunedProjects))
	for _, name := range prunedProjects {
		log.Info("  %s", name)
	}

	// 如果是模拟运行，直接返回
	if opts.DryRun {
		log.Info("模拟运行，没有实际清理任何项目")
		return nil
	}

	// 创建统计对象
	stats := &pruneStats{total: len(prunedProjects)}

	// 并发删除项目
	log.Debug("开始并发清理项�?..")
	errChan := make(chan error, len(prunedProjects))
	var wg sync.WaitGroup

	// 设置并发控制
	maxWorkers := opts.Jobs
	if maxWorkers <= 0 {
		maxWorkers = 8
	}
	log.Debug("设置并发数为: %d", maxWorkers)
	sem := make(chan struct{}, maxWorkers)

	for _, name := range prunedProjects {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			projectPath := filepath.Join(workDir, name)

			// 如果启用了详细模式，显示更多信息
			log.Debug("正在清理项目 %s...", name)

			// 如果不是强制模式，检查项目是否有本地修改
			if !opts.Force {
				repo := git.NewRepository(projectPath, git.NewRunner())
				clean, err := repo.IsClean()
				if err != nil {
					log.Error("检查项�?%s 是否干净失败: %v", name, err)
					errChan <- fmt.Errorf("failed to check if project %s is clean: %w", name, err)

					// 更新统计信息
					stats.mu.Lock()
					stats.failed++
					stats.mu.Unlock()
					return
				}

				if !clean {
					log.Warn("项目 %s 有本地修改，跳过 (使用 --force 强制清理)", name)

					// 更新统计信息
					stats.mu.Lock()
					stats.failed++
					stats.mu.Unlock()
					return
				}
			}

			// 删除项目目录
			log.Debug("删除项目目录: %s", projectPath)
			if err := os.RemoveAll(projectPath); err != nil {
				log.Error("删除项目 %s 失败: %v", name, err)
				errChan <- fmt.Errorf("failed to remove project %s: %w", name, err)

				// 更新统计信息
				stats.mu.Lock()
				stats.failed++
				stats.mu.Unlock()
				return
			}

			log.Info("已清理项�?%s", name)

			// 更新统计信息
			stats.mu.Lock()
			stats.success++
			stats.mu.Unlock()
		}(name)
	}

	// 等待所有goroutine完成
	log.Debug("等待所有清理任务完�?..")
	wg.Wait()
	close(errChan)

	// 收集所有错�?
	var errs []error
	for err := range errChan {
		if err != nil {
			errs = append(errs, err)
		}
	}

	// 输出统计信息
	log.Info("清理完成: 总计 %d 个项目, 成功 %d 个, 失败 %d 个",
		stats.total, stats.success, stats.failed)

	if len(errs) > 0 {
		log.Error("清理过程中遇到 %d 个错误", len(errs))
		return fmt.Errorf("encountered %d errors during pruning", len(errs))
	}

	return nil
}
