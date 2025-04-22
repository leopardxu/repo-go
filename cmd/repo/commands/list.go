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
	manifest, err := parser.ParseFromFile(cfg.ManifestName)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	manager := project.NewManager(manifest, cfg) // Use loaded cfg
	// 获取要处理的项目
	var projects []*project.Project // Declare projects variable
	if len(args) == 0 {
		// 如果没有指定项目，则处理所有项目
		var groupsArg []string
		if opts.Groups != "" { // Use opts.Groups
			groupsArg = []string{opts.Groups}
		}
		projects, err = manager.GetProjects(groupsArg) // Assign to projects
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		projects, err = manager.GetProjectsByNames(args) // Assign to projects
		if err != nil {
			if !opts.MissingOK {
				return fmt.Errorf("failed to get projects: %w", err)
			}
			// 如果设置了missing-ok，则忽略错误
			if !opts.Quiet {
				fmt.Printf("Warning: %v\n", err)
			}
		}
	}

	// 过滤路径前缀
	if opts.PathPrefix != "" {
		filteredProjects := []*project.Project{}
		for _, p := range projects {
			if strings.HasPrefix(p.Path, opts.PathPrefix) {
				filteredProjects = append(filteredProjects, p)
			}
		}
		projects = filteredProjects
	}

	// 过滤正则表达式
	if opts.Regex != "" {
		filteredProjects := []*project.Project{}
		regex, err := regexp.Compile(opts.Regex)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		for _, p := range projects {
			if regex.MatchString(p.Name) || regex.MatchString(p.Path) {
				filteredProjects = append(filteredProjects, p)
			}
		}
		projects = filteredProjects
	}

	// 显示项目列表
	for _, p := range projects {
		var output string
		
		path := p.Path
		if opts.RelativeTo != "" {
			relPath, err := filepath.Rel(opts.RelativeTo, p.Path)
			if err == nil {
				path = relPath
			}
		}
		
		if opts.Path {
			// 显示项目目录
			output = path
		} else if opts.Name {
			// 只显示项目名
			output = p.Name
		} else if opts.URL {
			// 显示项目URL
			output = p.RemoteName
		} else if opts.FullName {
			// 显示项目名和目录
			output = fmt.Sprintf("%s : %s", p.Name, path)
		} else if opts.FullPath {
			// 显示完整路径
			absPath, err := filepath.Abs(p.Path)
			if err == nil {
				output = absPath
			} else {
				output = p.Path
			}
		} else {
			// 默认显示项目路径
			output = path
		}
		
		fmt.Println(output)
	}

	return nil
}