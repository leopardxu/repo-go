package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/logger"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// ForallOptions holds the options for the forall command
type ForallOptions struct {
	Command      string
	Parallel     bool
	Jobs         int
	IgnoreErrors bool
	Quiet        bool
	Verbose      bool
	Groups       string
	Config       *config.Config
	CommonManifestOptions
}

// ForallCmd creates the forall command
func ForallCmd() *cobra.Command {
	opts := &ForallOptions{}
	cmd := &cobra.Command{
		Use:   "forall [<project>...] -c <command> [<arg>...]",
		Short: "Run a shell command in each project",
		Long:  `Executes the same shell command in the working directory of each specified project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg

			// Separate project names from the command and its arguments
			projectNames := args
			commandIndex := cmd.ArgsLenAtDash()
			if commandIndex != -1 {
				projectNames = args[:commandIndex]
				opts.Command = strings.Join(args[commandIndex:], " ")
			}

			if opts.Command == "" {
				return fmt.Errorf("command (-c) is required")
			}

			return runForall(opts, projectNames)
		},
	}

	// Add flags
	cmd.Flags().StringVarP(&opts.Command, "command", "c", "", "command and arguments to execute")
	cmd.Flags().BoolVarP(&opts.Parallel, "parallel", "p", false, "run commands in parallel")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel (if -p is specified)")
	cmd.Flags().BoolVar(&opts.IgnoreErrors, "ignore-errors", false, "continue executing even if a command fails")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show commands being executed")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "restrict execution to projects in specified groups (comma-separated)")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// forallStats tracks command execution statistics
type forallStats struct {
	mu      sync.Mutex
	Success int
	Failed  int
}

// runForall executes the forall command logic
func runForall(opts *ForallOptions, projectNames []string) error {
	// 初始化日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	// 加载清单
	log.Debug("Loading manifest file")
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName, strings.Split(opts.Config.Groups, ","))
	if err != nil {
		log.Error("Failed to parse manifest: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	log.Debug("Creating project manager")
	manager := project.NewManagerFromManifest(manifest, opts.Config)

	// 获取要处理的项目
	log.Debug("Getting projects to operate on")
	var projects []*project.Project
	var groupsArg []string
	if opts.Groups != "" {
		groupsArg = strings.Split(opts.Groups, ",")
	}

	if len(projectNames) == 0 {
		projects, err = manager.GetProjectsInGroups(groupsArg)
		if err != nil {
			log.Error("Failed to get projects: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 过滤指定的项目
		filteredProjects, err := manager.GetProjectsByNames(projectNames)
		if err != nil {
			log.Error("Failed to get projects by name: %v", err)
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
		if len(groupsArg) > 0 {
			for _, p := range filteredProjects {
				if p.IsInAnyGroup(groupsArg) {
					projects = append(projects, p)
				}
			}
		} else {
			projects = filteredProjects
		}
	}

	// 执行命令
	log.Info("Executing command '%s' in %d projects", opts.Command, len(projects))

	type forallResult struct {
		Name string
		Err  error
	}

	// 设置并发控制
	maxConcurrency := opts.Jobs
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}

	// 如果不是并行模式，将并发数设为1
	if !opts.Parallel {
		maxConcurrency = 1
	}

	// 创建通道和等待组
	sem := make(chan struct{}, maxConcurrency)
	results := make(chan forallResult, len(projects))
	var wg sync.WaitGroup
	stats := forallStats{}

	// 并发执行命令
	for _, p := range projects {
		if p.Worktree == "" { // 跳过没有工作目录的项目
			log.Debug("Skipping project %s (no worktree)", p.Name)
			continue
		}

		wg.Add(1)
		go func(proj *project.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			log.Debug("Executing command in project %s", proj.Name)
			cmd := exec.Command("sh", "-c", opts.Command)
			cmd.Dir = proj.Worktree
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Run()
			results <- forallResult{Name: proj.Name, Err: err}
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
			errMsg := fmt.Sprintf("Error in %s: %v", res.Name, res.Err)
			log.Error(errMsg)
			stats.mu.Lock()
			stats.Failed++
			stats.mu.Unlock()
			if !opts.IgnoreErrors {
				return fmt.Errorf("command failed in project %s", res.Name)
			}
		} else {
			log.Debug("Command executed successfully in project %s", res.Name)
			stats.mu.Lock()
			stats.Success++
			stats.mu.Unlock()
		}
	}

	// 输出统计信息
	log.Info("Command execution complete. Success: %d, Failed: %d", stats.Success, stats.Failed)

	// 如果有失败的项目，返回错误
	if stats.Failed > 0 {
		return fmt.Errorf("forall command failed in %d projects", stats.Failed)
	}

	return nil
}