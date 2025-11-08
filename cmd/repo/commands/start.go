package commands

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/git"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/progress"
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
	// 确保在repo根目录下执行
	originalDir, err := EnsureRepoRoot(log)
	if err != nil {
		log.Error("查找repo根目录失败: %v", err)
		return fmt.Errorf("failed to locate repo root: %w", err)
	}
	defer RestoreWorkDir(originalDir, log)

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

	// 验证分支名格式（与原生git-repo保持一致）
	if err := git.CheckRefFormat(fmt.Sprintf("heads/%s", branchName)); err != nil {
		log.Error("分支名格式验证失败: %v", err)
		return fmt.Errorf("'%s' is not a valid branch name", branchName)
	}

	// 获取项目列表
	projectNames := args[1:]

	log.Info("开始创建分支'%s'", branchName)

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

	// 获取要处理的项目
	var projects []*project.Project
	if opts.All || len(projectNames) == 0 {
		// 如果指定-all或没有指定项目，则处理所有项
		log.Debug("获取所有项..")
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

	// 使用goroutine池并发创建分支
	log.Info("开始创建分支，并行任务数 %d...", opts.Jobs)

	// 创建manifest项目名到项目的映射，用于访问upstream等信息
	type ManifestProject struct {
		Upstream   string
		DestBranch string
	}
	manifestProjectInfo := make(map[string]ManifestProject)
	for _, proj := range manifest.Projects {
		manifestProjectInfo[proj.Name] = ManifestProject{
			Upstream:   proj.Upstream,
			DestBranch: proj.DestBranch,
		}
	}

	// 创建进度显示器
	prog := progress.NewProgress(fmt.Sprintf("Starting %s", branchName), len(projects), opts.Quiet)

	var wg sync.WaitGroup
	errChan := make(chan error, len(projects))
	resultChan := make(chan string, len(projects))
	sem := make(chan struct{}, opts.Jobs) // 使用信号量控制并发数

	for _, p := range projects {
		p := p // 创建副本避免闭包问题
		wg.Add(1)

		go func() {
			defer wg.Done()
			sem <- struct{}{}        // 获取信号
			defer func() { <-sem }() // 释放信号

			log.Debug("在项%s 中创建分'%s'...", p.Name, branchName)

			// 确定使用的修订版和分支合并目标
			revision := opts.Rev

			// 如果没有指定revision，使用项目的revision
			if revision == "" {
				revision = p.Revision
			}

			// 确定上游分支（用于设置跟踪关系）
			upstream := ""
			if projInfo, exists := manifestProjectInfo[p.Name]; exists {
				upstream = projInfo.Upstream
			}
			if upstream == "" {
				// 如果项目没有指定upstream，使用default的upstream
				if manifest.Default.Upstream != "" {
					upstream = manifest.Default.Upstream
				} else {
					// 否则使用revision作为upstream
					upstream = revision
				}
			}

			// 处理不可变修订版本（SHA1、tag、change）
			// 如果是不可变的修订版本（如SHA1、tag），使用默认修订版本作为上游
			if git.IsImmutable(p.Revision) {
				if manifest.Default.Revision != "" {
					// 使用默认修订版本作为分支合并目标
					log.Debug("项目 %s 的修订版本 %s 是不可变的，使用默认修订版本 %s 作为上游",
						p.Name, p.Revision, manifest.Default.Revision)
					upstream = manifest.Default.Revision
				}
			}

			log.Debug("项目 %s 使用修订版本: %s, 上游分支: %s", p.Name, revision, upstream)

			// 创建分支
			if err := p.GitRepo.CreateBranch(branchName, revision); err != nil {
				log.Error("项目 %s 创建分支失败: %v", p.Name, err)
				errChan <- fmt.Errorf("project %s: %w", p.Name, err)
				stats.increment(false)
				prog.Update(p.Name + " - 失败")
				return
			}

			resultChan <- fmt.Sprintf("项目 %s: 分支 '%s' 创建成功", p.Name, branchName)
			stats.increment(true)
			prog.Update(p.Name)
			log.Debug("项目 %s 分支创建完成", p.Name)
		}()
	}

	// 启动一goroutine 来关闭结果通道
	go func() {
		wg.Wait()
		prog.Finish("") // 完成进度
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
	log.Info("分支创建操作完成，总计: %d，成功 %d，失败 %d", stats.total, stats.success, stats.failed)

	// 如果有错误，返回汇总错误
	if len(errs) > 0 {
		log.Error("有 %d 个项目创建分支失败", len(errs))
		return fmt.Errorf("%d projects failed: %v", len(errs), errors.Join(errs...))
	}

	return nil
}
