package commands

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/spf13/cobra"
)

// GrepOptions holds the options for the grep command
type GrepOptions struct {
	IgnoreCase      bool
	FixedStrings    bool
	LineNumber      bool
	FilesWithMatches bool
	Quiet           bool
	Verbose         bool
	Jobs            int
	Pattern         string
	Groups          string
	Config          *config.Config
	CommonManifestOptions
}

// grepStats tracks grep execution statistics
type grepStats struct {
	mu      sync.Mutex
	Success int
	Failed  int
	Matches int
}

// GrepCmd creates the grep command
func GrepCmd() *cobra.Command {
	opts := &GrepOptions{}
	cmd := &cobra.Command{
		Use:   "grep <pattern> [<project>...]",
		Short: "Print lines matching a pattern",
		Long:  `Looks for specified patterns in the working tree files of the specified projects.`,
		Args:  cobra.MinimumNArgs(1), // Requires at least the pattern
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg
			opts.Pattern = args[0]
			return runGrep(opts, args[1:])
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&opts.IgnoreCase, "ignore-case", "i", false, "ignore case distinctions")
	cmd.Flags().BoolVarP(&opts.FixedStrings, "fixed-strings", "F", false, "interpret pattern as fixed string")
	cmd.Flags().BoolVarP(&opts.LineNumber, "line-number", "n", false, "prefix matching lines with line number")
	cmd.Flags().BoolVarP(&opts.FilesWithMatches, "files-with-matches", "l", false, "show only file names containing matches")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show detailed output")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "restrict execution to projects in specified groups (comma-separated)")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runGrep executes the grep command logic
func runGrep(opts *GrepOptions, projectNames []string) error {
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
	log.Debug("正在加载清单文件...")
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName, strings.Split(opts.Config.Groups, ","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理�?
	log.Debug("正在创建项目管理�?..")
	manager := project.NewManagerFromManifest(manifest, opts.Config)

	// 获取要处理的项目
	log.Debug("正在获取要处理的项目...")
	var projects []*project.Project
	var groupsArg []string
	if opts.Groups != "" {
		groupsArg = strings.Split(opts.Groups, ",")
	}

	if len(projectNames) == 0 {
		log.Debug("获取所有项�?..")
		projects, err = manager.GetProjectsInGroups(groupsArg)
		if err != nil {
			log.Error("获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		log.Debug("根据名称获取项目: %v", projectNames)
		// 过滤指定的项�?
		filteredProjects, err := manager.GetProjectsByNames(projectNames)
		if err != nil {
			log.Error("根据名称获取项目失败: %v", err)
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

	// 构建 git grep 参数
	log.Debug("构建 git grep 参数...")
	grepArgs := []string{"grep"}
	if opts.IgnoreCase {
		grepArgs = append(grepArgs, "-i")
	}
	if opts.FixedStrings {
		grepArgs = append(grepArgs, "-F")
	}
	if opts.LineNumber {
		grepArgs = append(grepArgs, "-n")
	}
	if opts.FilesWithMatches {
		grepArgs = append(grepArgs, "-l")
	}
	grepArgs = append(grepArgs, "--color=always")
	grepArgs = append(grepArgs, "-e", opts.Pattern)

	// 在每个项目中并发执行 grep
	log.Info("正在 %d 个项目中搜索 '%s'...", len(projects), opts.Pattern)

	type grepResult struct {
		project *project.Project
		output  []byte
		err     error
	}

	// 创建工作�?
	maxWorkers := opts.Jobs
	if maxWorkers <= 0 {
		maxWorkers = 8
	}

	sem := make(chan struct{}, maxWorkers)
	results := make(chan grepResult, len(projects))
	var wg sync.WaitGroup
	stats := grepStats{}

	// 跟踪有工作目录的项目数量
	validProjects := 0

	for _, p := range projects {
		if p.Worktree == "" {
			log.Debug("跳过项目 %s (无工作目�?", p.Name)
			continue
		}

		validProjects++
		wg.Add(1)
		go func(p *project.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			log.Debug("在项�?%s 中执�?grep...", p.Name)
			cmd := exec.Command("git", grepArgs...)
			cmd.Dir = p.Worktree
			output, err := cmd.CombinedOutput()
			results <- grepResult{p, output, err}
		}(p)
	}

	// 关闭结果通道
	go func() {
		wg.Wait()
		close(results)
	}()

	// 处理结果
	var foundMatches bool
	var errors []error

	for res := range results {
		if res.err != nil {
			if exitErr, ok := res.err.(*exec.ExitError); ok {
				if exitErr.ExitCode() == 1 {
					// 退出码 1 表示没有找到匹配项，这不是错�?
					log.Debug("项目 %s 中没有找到匹配项", res.project.Name)
					stats.mu.Lock()
					stats.Success++
					stats.mu.Unlock()
					continue
				}
			}
			log.Error("在项�?%s 中执�?grep 失败: %v", res.project.Name, res.err)
			errors = append(errors, fmt.Errorf("error grepping in %s: %v", res.project.Name, res.err))
			stats.mu.Lock()
			stats.Failed++
			stats.mu.Unlock()
			continue
		}

		if len(res.output) > 0 {
			foundMatches = true
			lines := strings.Split(strings.TrimSpace(string(res.output)), "\n")
			log.Debug("项目 %s 中找�?%d 个匹配项", res.project.Name, len(lines))
			
			stats.mu.Lock()
			stats.Success++
			stats.Matches += len(lines)
			stats.mu.Unlock()
			
			for _, line := range lines {
				fmt.Printf("%s:%s\n", res.project.Name, line)
			}
		} else {
			log.Debug("项目 %s 中没有找到匹配项", res.project.Name)
			stats.mu.Lock()
			stats.Success++
			stats.mu.Unlock()
		}
	}

	// 输出错误信息
	if len(errors) > 0 {
		log.Error("�?%d 个项目中执行 grep 失败", len(errors))
		for _, err := range errors {
			log.Error("%v", err)
		}
	}

	// 输出统计信息
	log.Info("搜索完成. 处理项目: %d, 成功: %d, 失败: %d, 找到匹配�? %d", 
		validProjects, stats.Success, stats.Failed, stats.Matches)

	if !foundMatches && !opts.Quiet {
		log.Info("没有找到匹配�?)
	}

	// 如果有失败的项目，返回错�?
	if stats.Failed > 0 {
		return fmt.Errorf("grep command failed in %d projects", stats.Failed)
	}

	return nil
}
