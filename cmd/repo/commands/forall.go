package commands

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// ForallOptions 包含forall命令的选项
type ForallOptions struct {
	Command        string
	Project       string
	Verbose       bool
	Jobs          int
	Regex         bool
	InverseRegex  bool
	Groups        string
	AbortOnErrors bool
	Interactive   bool
	Quiet         bool
	ShowHeaders   bool
	IgnoreMissing bool
	OuterManifest bool
	NoOuterManifest bool
	ThisManifestOnly bool
}

// ForallCmd 返回forall命令
func ForallCmd() *cobra.Command {
	opts := &ForallOptions{
		Jobs: runtime.NumCPU() * 2,
	}

	cmd := &cobra.Command{
		Use:   "forall [<project>...] -c <command> [<arg>...]",
		Short: "Run a shell command in each project",
		Long:  `Run a shell command in each project.

The -r option allows running the command only on projects matching regex or
wildcard expression.

By default, projects are processed non-interactively in parallel. If you want to
run interactive commands, make sure to pass --interactive to force --jobs 1.
While the processing order of projects is not guaranteed, the order of project
output is stable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForall(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().StringVarP(&opts.Command, "command", "c", "", "command (and arguments) to execute")
	cmd.Flags().StringVarP(&opts.Project, "project", "p", "", "run command on the specified project only")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.ShowHeaders, "show-headers", "s", false, "show project headers before output")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of parallel jobs")
	cmd.Flags().BoolVarP(&opts.Regex, "regex", "r", false, "execute the command only on projects matching regex or wildcard expression")
	cmd.Flags().BoolVarP(&opts.InverseRegex, "inverse-regex", "i", false, "execute the command only on projects not matching regex or wildcard expression")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "execute the command only on projects matching the specified groups")
	cmd.Flags().BoolVarP(&opts.AbortOnErrors, "abort-on-errors", "e", false, "abort if a command exits unsuccessfully")
	cmd.Flags().BoolVar(&opts.IgnoreMissing, "ignore-missing", false, "silently skip & do not exit non-zero due missing checkouts")
	cmd.Flags().BoolVar(&opts.Interactive, "interactive", false, "force interactive usage")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	// 必需的选项
	cmd.MarkFlagRequired("command")

	return cmd
}

// runForall 执行forall命令
func runForall(opts *ForallOptions, args []string) error {
	fmt.Printf("Running command in projects: %s\n", opts.Command)

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
	manager := project.NewManager(manifest, cfg)

	// 获取要处理的项目
	var projects []*project.Project
	if opts.Project != "" {
		// 如果指定了--project选项，则只处理该项目
		projects, err = manager.GetProjectsByNames([]string{opts.Project})
		if err != nil {
			return fmt.Errorf("failed to get project: %w", err)
		}
	} else if len(args) > 0 {
		// 如果指定了项目列表，则处理这些项目
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，处理所有项目
		projects, err = manager.GetProjects("")
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	// 创建工作池
	sem := make(chan struct{}, opts.Jobs)
	var wg sync.WaitGroup
	
	// 错误收集
	errChan := make(chan error, len(projects))
	
	// 并行执行命令
	for _, p := range projects {
		wg.Add(1)
		go func(p *project.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			
			if err := executeCommand(p, opts.Command, opts.Verbose); err != nil {
				errChan <- fmt.Errorf("failed to execute command in project %s: %w", p.Name, err)
			}
		}(p)
	}
	
	wg.Wait()
	close(errChan)
	
	// 处理错误
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}
	
	if len(errors) > 0 {
		for _, err := range errors {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return fmt.Errorf("%d errors occurred during command execution", len(errors))
	}

	fmt.Println("Command execution completed successfully")
	return nil
}

// executeCommand 在项目中执行命令
func executeCommand(p *project.Project, command string, verbose bool) error {
	// 获取项目目录
	projectDir := p.Path
	
	// 创建命令
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = projectDir
	
	// 设置环境变量
	cmd.Env = append(os.Environ(),
		"REPO_PROJECT="+p.Name,
		"REPO_PATH="+p.Path,
		"REPO_REMOTE="+p.RemoteName,
		"REPO_REVISION="+p.Revision,
	)
	
	// 如果是详细模式，显示输出
	if verbose {
		fmt.Printf("=== Project %s ===\n", p.Name)
		
		// 捕获输出
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("command failed: %w\n%s", err, output)
		}
		
		// 显示输出
		if len(output) > 0 {
			fmt.Println(strings.TrimSpace(string(output)))
		}
	} else {
		// 否则，只执行命令
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("command failed: %w", err)
		}
	}
	
	return nil
}