package commands

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/spf13/cobra"
)

// ListOptions 包含list命令的选项
type ListOptions struct {
	Path        bool
	Name        bool
	URL         bool
	FullName    bool
	FullPath    bool
	Groups      string
	MissingOK   bool
	PathPrefix  string
	Regex       string
	RelativeTo  string
	AllProjects bool
	Verbose     bool
	Quiet       bool
	Jobs        int
	Config      *config.Config
}

// listStats 用于统计list命令的执行结果
type listStats struct {
	mu      sync.Mutex
	success int
	failed  int
}

// ListCmd 返回list命令
func ListCmd() *cobra.Command {
	opts := &ListOptions{}

	cmd := &cobra.Command{
		Use:   "list [-f] [<project>...]",
		Short: "List projects and their associated directories",
		Long: `List all projects; pass '.' to list the project for the cwd.

By default, only projects that currently exist in the checkout are shown. If you
want to list all projects (using the specified filter settings), use the --all
option. If you want to show all projects regardless of the manifest groups, then
also pass --groups all.

This is similar to running: repo forall -c 'echo "$REPO_PATH : $REPO_PROJECT"'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.Path, "path", "p", false, "display only the path of the repository")
	cmd.Flags().BoolVarP(&opts.Name, "name", "n", false, "display only the name of the repository")
	cmd.Flags().BoolVarP(&opts.URL, "url", "u", false, "display the fetch url instead of name")
	cmd.Flags().BoolVar(&opts.FullName, "full-name", false, "show project name and directory")
	cmd.Flags().BoolVarP(&opts.FullPath, "fullpath", "f", false, "display the full work tree path instead of the relative path")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "filter projects by groups")
	cmd.Flags().BoolVar(&opts.MissingOK, "missing-ok", false, "don't exit with an error if a project doesn't exist")
	cmd.Flags().StringVar(&opts.PathPrefix, "path-prefix", "", "limit to projects with path prefix")
	cmd.Flags().StringVarP(&opts.Regex, "regex", "r", "", "filter the project list based on regex or wildcard matching of strings")
	cmd.Flags().StringVar(&opts.RelativeTo, "relative-to", "", "display paths relative to this one (default: top of repo client checkout)")
	cmd.Flags().BoolVarP(&opts.AllProjects, "all", "a", false, "show projects regardless of checkout state")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")

	return cmd
}

// runList 执行list命令
func runList(opts *ListOptions, args []string) error {
	// 初始化日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	log.Info("开始列出项目")

	// 加载配置
	log.Debug("正在加载配置...")
	cfg, err := config.Load()
	if err != nil {
		log.Error("加载配置失败: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}
	opts.Config = cfg

	// 加载清单
	log.Debug("正在解析清单文件...")
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	log.Debug("正在创建项目管理器...")
	manager := project.NewManagerFromManifest(manifestObj, cfg)

	// 获取要处理的项目
	log.Debug("正在获取项目列表...")
	var projects []*project.Project
	var groupsArg []string
	if opts.Groups != "" {
		groupsArg = strings.Split(opts.Groups, ",")
		log.Debug("按组过滤项目: %v", groupsArg)
	}

	if len(args) == 0 {
		log.Debug("获取所有项目")
		projects, err = manager.GetProjectsInGroups(groupsArg)
		if err != nil {
			log.Error("获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		log.Debug("根据名称获取项目: %v", args)
		projects, err = manager.GetProjectsByNames(args)
		if err != nil && opts.MissingOK {
			log.Warn("获取项目警告: %v", err)
			err = nil
		} else if err != nil {
			log.Error("获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
	}

	log.Info("找到 %d 个项目", len(projects))

	// 过滤函数
	filterProjects := func(projects []*project.Project, filterFunc func(*project.Project) bool) []*project.Project {
		filtered := make([]*project.Project, 0, len(projects))
		for _, p := range projects {
			if filterFunc(p) {
				filtered = append(filtered, p)
			}
		}
		return filtered
	}

	// 路径前缀过滤
	if opts.PathPrefix != "" {
		log.Debug("按路径前缀过滤: %s", opts.PathPrefix)
		projects = filterProjects(projects, func(p *project.Project) bool {
			return strings.HasPrefix(p.Path, opts.PathPrefix)
		})
		log.Debug("过滤后剩余 %d 个项目", len(projects))
	}

	// 正则表达式过�?
	if opts.Regex != "" {
		log.Debug("按正则表达式过滤: %s", opts.Regex)
		regex, err := regexp.Compile(opts.Regex)
		if err != nil {
			log.Error("无效的正则表达式: %v", err)
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		projects = filterProjects(projects, func(p *project.Project) bool {
			return regex.MatchString(p.Name) || regex.MatchString(p.Path)
		})
		log.Debug("过滤后剩余 %d 个项目", len(projects))
	}

	// 设置并发控制
	maxConcurrency := opts.Jobs
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}

	// 创建通道和等待组
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	stats := &listStats{}

	log.Debug("开始处理项目信�?..")

	// 并发输出项目信息
	for _, p := range projects {
		wg.Add(1)
		sem <- struct{}{}
		go func(p *project.Project) {
			defer func() {
				<-sem
				wg.Done()
			}()

			var output string
			path := p.Path
			if opts.RelativeTo != "" {
				relPath, err := filepath.Rel(opts.RelativeTo, p.Path)
				if err == nil {
					path = relPath
				} else {
					log.Debug("计算相对路径失败: %v", err)
				}
			}

			switch {
			case opts.Path:
				output = path
			case opts.Name:
				output = p.Name
			case opts.URL:
				output = p.RemoteName
			case opts.FullName:
				output = fmt.Sprintf("%s : %s", p.Name, path)
			case opts.FullPath:
				absPath, err := filepath.Abs(p.Path)
				if err == nil {
					output = absPath
				} else {
					log.Debug("计算绝对路径失败: %v", err)
					output = p.Path
				}
			default:
				output = path
			}

			// 输出项目信息
			fmt.Println(output)

			// 更新统计信息
			stats.mu.Lock()
			stats.success++
			stats.mu.Unlock()
		}(p)
	}

	// 等待所有goroutine完成
	log.Debug("等待所有处理完�?..")
	wg.Wait()

	// 输出统计信息
	log.Info("列出完成，共处理 %d 个项目", stats.success)

	return nil
}
