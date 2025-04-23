package commands

import (
	"fmt"
	"sync"
	"errors"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// StageOptions 包含stage命令的选项
type StageOptions struct {
	All        bool
	Interactive bool
	Verbose    bool
	Quiet      bool
	OuterManifest bool
	NoOuterManifest bool
	ThisManifestOnly bool
	Patch      bool
	Edit       bool
	Force      bool
}

// StageCmd 返回stage命令
func StageCmd() *cobra.Command {
	opts := &StageOptions{}

	cmd := &cobra.Command{
		Use:   "stage [<project>...] [<file>...]",
		Short: "Stage file contents to the index",
		Long:  `Stage file contents to the index (equivalent to 'git add').`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStage(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.All, "all", "A", false, "stage all files")
	cmd.Flags().BoolVarP(&opts.Interactive, "interactive", "i", false, "interactive staging")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runStage 执行stage命令
func runStage(opts *StageOptions, args []string) error {
	if len(args) == 0 && !opts.All {
		return fmt.Errorf("no files specified and --all not used")
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

	// 确定文件和项目列表
	var files []string
	var projectNames []string

	// 解析参数，区分项目名和文件名
	// 这里简化处理，实际上可能需要更复杂的逻辑
	if len(args) > 0 {
		// 假设第一个参数是项目名，其余是文件名
		projectNames = []string{args[0]}
		if len(args) > 1 {
			files = args[1:]
		}
	}

	// 获取要处理的项目
	var projects []*project.Project
	if len(projectNames) == 0 {
		// 如果没有指定项目，则处理所有项目
		projects, err = manager.GetProjects(nil) // Use nil instead of ""
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

	fmt.Println("Staging files in projects")

	// 构建stage命令选项（实际上是git add命令）
	stageArgs := []string{"add"}
	
	if opts.All {
		stageArgs = append(stageArgs, "--all")
	}
	
	if opts.Interactive {
		stageArgs = append(stageArgs, "--interactive")
	}
	
	if opts.Patch {
		stageArgs = append(stageArgs, "--patch")
	}
	
	if opts.Edit {
		stageArgs = append(stageArgs, "--edit")
	}
	
	if opts.Force {
		stageArgs = append(stageArgs, "--force")
	}
	
	if opts.Verbose {
		stageArgs = append(stageArgs, "--verbose")
	}

	// 添加文件参数
	if len(files) > 0 {
		stageArgs = append(stageArgs, files...)
	}

	// 使用goroutine池并发执行stage
	var wg sync.WaitGroup
	errChan := make(chan error, len(projects))
	resultChan := make(chan string, len(projects))
	pool := make(chan struct{}, 10) // 限制并发数为10

	for _, p := range projects {
		wg.Add(1)
		pool <- struct{}{}
		
		go func(project *project.Project) {
			defer func() {
				<-pool
				wg.Done()
			}()
			
			output, err := project.GitRepo.RunCommand(stageArgs...)
			if err != nil {
				errChan <- fmt.Errorf("project %s: %w", project.Name, err)
				return
			}
			
			if output != "" {
				resultChan <- fmt.Sprintf("Project %s:\n%s", project.Name, output)
			} else {
				resultChan <- fmt.Sprintf("Project %s: Files staged successfully", project.Name)
			}
		}(p)
	}

	// 等待所有goroutine完成
	wg.Wait()
	close(errChan)
	close(resultChan)

	// 处理错误
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	// 输出结果
	if !opts.Quiet {
		for result := range resultChan {
			fmt.Println(result)
		}
	}

	// 如果有错误，返回汇总错误
	if len(errs) > 0 {
		return fmt.Errorf("%d projects failed: %v", len(errs), errors.Join(errs...))
	}

	return nil
}