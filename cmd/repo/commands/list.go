package commands

import (
	"fmt"
	// 删除未使用的 os 包
	"strings" // 添加缺少的 strings 包
	"regexp"
	"path/filepath"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
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
}

// ListCmd 返回list命令
func ListCmd() *cobra.Command {
	opts := &ListOptions{}

	cmd := &cobra.Command{
		Use:   "list [-f] [<project>...]",
		Short: "List projects and their associated directories",
		Long:  `List all projects; pass '.' to list the project for the cwd.

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

	return cmd
}

// runList 执行list命令
// runList executes list command
func runList(opts *ListOptions, args []string) error {
	if !opts.Quiet {
		fmt.Println("Listing projects")
	}

	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 加载清单
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName,strings.Split(cfg.Groups,","))
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	manager := project.NewManager(manifest, cfg)

	// 使用通道和goroutine池并发获取项目
	type projectResult struct {
		Projects []*project.Project
		Err      error
	}

	resultChan := make(chan projectResult, 1)

	go func() {
		var projects []*project.Project
		var err error
		
		if len(args) == 0 {
			var groupsArg []string
			if opts.Groups != "" {
				groupsArg = []string{opts.Groups}
			}
			projects, err = manager.GetProjects(groupsArg)
		} else {
			projects, err = manager.GetProjectsByNames(args)
			if err != nil && opts.MissingOK {
				if !opts.Quiet {
					fmt.Printf("Warning: %v\n", err)
				}
				err = nil
			}
		}
		resultChan <- projectResult{projects, err}
	}()

	result := <-resultChan
	if result.Err != nil {
		return fmt.Errorf("failed to get projects: %w", result.Err)
	}
	projects := result.Projects

	// 并发过滤和输出项目
	maxWorkers := 8
	sem := make(chan struct{}, maxWorkers)
	errChan := make(chan error, 1)
	done := make(chan struct{})

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
		projects = filterProjects(projects, func(p *project.Project) bool {
			return strings.HasPrefix(p.Path, opts.PathPrefix)
		})
	}

	// 正则表达式过滤
	if opts.Regex != "" {
		regex, err := regexp.Compile(opts.Regex)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		projects = filterProjects(projects, func(p *project.Project) bool {
			return regex.MatchString(p.Name) || regex.MatchString(p.Path)
		})
	}

	// 并发输出项目信息
	for _, p := range projects {
		sem <- struct{}{}
		go func(p *project.Project) {
			defer func() { <-sem }()

			var output string
			path := p.Path
			if opts.RelativeTo != "" {
				relPath, err := filepath.Rel(opts.RelativeTo, p.Path)
				if err == nil {
					path = relPath
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
					output = p.Path
				}
			default:
				output = path
			}

			fmt.Println(output)
		}(p)
	}

	// 等待所有goroutine完成
	go func() {
		for i := 0; i < maxWorkers; i++ {
			sem <- struct{}{}
		}
		close(done)
	}()

	select {
	case <-done:
	case err := <-errChan:
		return err
	}

	return nil
}