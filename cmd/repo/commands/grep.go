package commands

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// GrepOptions 包含grep命令的选项
type GrepOptions struct {
	LineNumber       bool
	NameOnly         bool
	Count            bool
	Color            string
	Extended         bool
	IgnoreCase       bool
	All              bool
	Fixed            bool
	Invert           bool
	Jobs             int
	Verbose          bool
	Quiet            bool
	Cached           bool
	Revision         string
	WordRegexp       bool
	Text             bool
	BinaryFiles      bool
	BasicRegexp      bool
	AllMatch         bool
	Context          int
	BeforeContext    int
	AfterContext     int
	FilesWithoutMatch bool
	OuterManifest    bool
	NoOuterManifest  bool
	ThisManifestOnly bool
}

// GrepCmd 返回grep命令
func GrepCmd() *cobra.Command {
	opts := &GrepOptions{}

	cmd := &cobra.Command{
		Use:   "grep [<project>...] <pattern>",
		Short: "Print lines matching a pattern",
		Long:  `Search for pattern in all projects or specified projects.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGrep(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.LineNumber, "line-number", "n", false, "Prefix the line number to matching lines")
	cmd.Flags().BoolVarP(&opts.NameOnly, "name-only", "l", false, "Show only file names containing matching lines")
	cmd.Flags().BoolVarP(&opts.Count, "count", "c", false, "Show the number of matches instead of matching lines")
	cmd.Flags().StringVar(&opts.Color, "color", "auto", "Control color usage: auto, always, never")
	cmd.Flags().BoolVarP(&opts.Extended, "extended-regexp", "E", false, "Use POSIX extended regexp for patterns")
	cmd.Flags().BoolVarP(&opts.IgnoreCase, "ignore-case", "i", false, "Ignore case differences")
	cmd.Flags().BoolVarP(&opts.All, "text", "a", false, "Process binary files as if they were text")
	cmd.Flags().BoolVarP(&opts.Fixed, "fixed-strings", "F", false, "Use fixed strings (not regexp) for pattern")
	cmd.Flags().BoolVarP(&opts.Invert, "invert-match", "v", false, "Select non-matching lines")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "Number of jobs to run in parallel")
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "Show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "Only show errors")
	cmd.Flags().BoolVar(&opts.Cached, "cached", false, "Search the index, instead of the work tree")
	cmd.Flags().StringVarP(&opts.Revision, "revision", "r", "", "Search TREEish, instead of the work tree")
	cmd.Flags().BoolVarP(&opts.WordRegexp, "word-regexp", "w", false, "Match the pattern only at word boundaries")
	cmd.Flags().BoolVar(&opts.BinaryFiles, "binary-files", false, "Don't match the pattern in binary files")
	cmd.Flags().BoolVarP(&opts.BasicRegexp, "basic-regexp", "G", false, "Use POSIX basic regexp for patterns (default)")
	cmd.Flags().BoolVar(&opts.AllMatch, "all-match", false, "Limit match to lines that have all patterns")
	cmd.Flags().IntVar(&opts.Context, "context", 0, "Show CONTEXT lines around match")
	cmd.Flags().IntVar(&opts.BeforeContext, "before-context", 0, "Show CONTEXT lines before match")
	cmd.Flags().IntVar(&opts.AfterContext, "after-context", 0, "Show CONTEXT lines after match")
	cmd.Flags().BoolVar(&opts.FilesWithoutMatch, "files-without-match", false, "Show only file names not containing matching lines")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "Operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "Do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "Only operate on this (sub)manifest")

	return cmd
}

// runGrep 执行grep命令
func runGrep(opts *GrepOptions, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("pattern required")
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
	manager := project.NewManager(manifest, cfg)

	// 确定搜索模式和项目列表
	var patterns []string
	var projectNames []string

	// 解析参数
	for i := 0; i < len(args); i++ {
		if args[i] == "-e" {
			if i+1 >= len(args) {
				return fmt.Errorf("option -e requires an argument")
			}
			patterns = append(patterns, args[i+1])
			i++
		} else if strings.HasPrefix(args[i], "-e") {
			patterns = append(patterns, args[i][2:])
		} else if i == len(args)-1 {
			patterns = append(patterns, args[i])
		} else {
			projectNames = append(projectNames, args[i])
		}
	}

	if len(patterns) == 0 {
		return fmt.Errorf("no pattern specified")
	}

	// 获取要处理的项目
	var projects []*project.Project
	if len(projectNames) == 0 {
		// 如果没有指定项目，则处理所有项目
		projects, err = manager.GetProjects("")
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		projects, err = manager.GetProjectsByNames(projectNames)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	if !opts.Quiet {
		fmt.Printf("Searching for patterns in projects\n")
	}

	// 构建grep命令选项
	grepArgs := []string{"grep"}
	
	if opts.LineNumber {
		grepArgs = append(grepArgs, "--line-number")
	}
	
	if opts.NameOnly {
		grepArgs = append(grepArgs, "--name-only")
	}
	
	if opts.Count {
		grepArgs = append(grepArgs, "--count")
	}
	
	if opts.Color != "auto" {
		grepArgs = append(grepArgs, "--color="+opts.Color)
	}
	
	if opts.Extended {
		grepArgs = append(grepArgs, "--extended-regexp")
	}
	
	if opts.IgnoreCase {
		grepArgs = append(grepArgs, "--ignore-case")
	}
	
	if opts.All {
		grepArgs = append(grepArgs, "--text")
	}
	
	if opts.Fixed {
		grepArgs = append(grepArgs, "--fixed-strings")
	}
	
	if opts.Invert {
		grepArgs = append(grepArgs, "--invert-match")
	}
	
	if opts.WordRegexp {
		grepArgs = append(grepArgs, "--word-regexp")
	}
	
	if opts.BinaryFiles {
		grepArgs = append(grepArgs, "--binary-files=without-match")
	}
	
	if opts.BasicRegexp {
		grepArgs = append(grepArgs, "--basic-regexp")
	}
	
	if opts.AllMatch {
		grepArgs = append(grepArgs, "--all-match")
	}
	
	if opts.Context > 0 {
		grepArgs = append(grepArgs, fmt.Sprintf("--context=%d", opts.Context))
	}
	
	if opts.BeforeContext > 0 {
		grepArgs = append(grepArgs, fmt.Sprintf("--before-context=%d", opts.BeforeContext))
	}
	
	if opts.AfterContext > 0 {
		grepArgs = append(grepArgs, fmt.Sprintf("--after-context=%d", opts.AfterContext))
	}
	
	if opts.FilesWithoutMatch {
		grepArgs = append(grepArgs, "--files-without-match")
	}

	// 添加搜索模式
	for _, pattern := range patterns {
		grepArgs = append(grepArgs, "-e", pattern)
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
			
			if !opts.Quiet {
				fmt.Printf("\nProject %s:\n", p.Name)
			}
			
			// 执行grep命令
			output, err := p.GitRepo.RunCommand(grepArgs...)
			if err != nil {
				// 忽略错误，因为git grep在没有匹配时会返回非零退出码
				// 但我们仍然想继续处理其他项目
			}
			
			if output != "" {
				fmt.Println(output)
			} else if !opts.Quiet {
				fmt.Println("No matches found")
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
		return fmt.Errorf("%d errors occurred during grep execution", len(errors))
	}

	return nil
}