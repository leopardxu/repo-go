package commands

import (
	"fmt"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/spf13/cobra"
)

// InfoOptions 包含info命令的选项
type InfoOptions struct {
	Diff             bool
	Overview         bool
	CurrentBranch    bool
	NoCurrentBranch  bool
	LocalOnly        bool
	Verbose          bool
	Quiet            bool
	OuterManifest    bool
	NoOuterManifest  bool
	ThisManifestOnly bool
	Config           *config.Config // <-- Add this field
	CommonManifestOptions
}

// infoStats 用于统计info命令的执行结果
type infoStats struct {
	mu      sync.Mutex
	success int
	failed  int
}

// InfoCmd 返回info命令
func InfoCmd() *cobra.Command {
	opts := &InfoOptions{}

	cmd := &cobra.Command{
		Use:   "info [-dl] [-o [-c]] [<project>...]",
		Short: "Get info on the manifest branch, current branch or unmerged branches",
		Long:  `Show detailed information about projects including branch info.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfo(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.Diff, "diff", "d", false, "show full info and commit diff including remote branches")
	cmd.Flags().BoolVarP(&opts.Overview, "overview", "o", false, "show overview of all local commits")
	cmd.Flags().BoolVarP(&opts.CurrentBranch, "current-branch", "c", false, "consider only checked out branches")
	cmd.Flags().BoolVar(&opts.NoCurrentBranch, "no-current-branch", false, "consider all local branches")
	cmd.Flags().BoolVarP(&opts.LocalOnly, "local-only", "l", false, "disable all remote operations")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runInfo 执行info命令
// runInfo executes the info command logic
func runInfo(opts *InfoOptions, args []string) error {
	// 初始化日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	log.Info("Starting info command")

	// 加载配置
	cfg, err := config.Load() // 声明err
	if err != nil {
		log.Error("Failed to load config: %v", err)
		return err
	}
	opts.Config = cfg // 分配加载的配置

	// 加载manifest
	log.Debug("Loading manifest from %s", cfg.ManifestName)
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ",")) // 重用err
	if err != nil {
		log.Error("Failed to parse manifest: %v", err)
		return err
	}

	// 创建项目管理器
	manager := project.NewManagerFromManifest(manifest, cfg)

	// 声明projects变量
	var projects []*project.Project

	// 获取要操作的项目
	if len(args) == 0 {
		log.Debug("Getting all projects")
		projects, err = manager.GetProjectsInGroups(nil) // 使用=，使用nil
		if err != nil {
			log.Error("Failed to get projects: %v", err)
			return err
		}
	} else {
		log.Debug("Getting projects by names: %v", args)
		projects, err = manager.GetProjectsByNames(args) // 使用=
		if err != nil {
			log.Error("Failed to get projects by name: %v", err)
			return err
		}
	}

	log.Info("Found %d projects to process", len(projects))

	// 并发获取项目信息
	type infoResult struct {
		Project *project.Project
		Output  string
		Err     error
	}

	results := make(chan infoResult, len(projects))
	sem := make(chan struct{}, 8) // 控制并发数
	var wg sync.WaitGroup
	stats := &infoStats{}

	for _, p := range projects {
		wg.Add(1)
		sem <- struct{}{}
		go func(proj *project.Project) {
			defer func() {
				<-sem
				wg.Done()
			}()

			var output string
			var err error
			var outputBytes []byte

			log.Debug("Processing project %s", proj.Name)

			// 根据选项显示不同信息
			switch {
			case opts.Diff:
				log.Debug("Getting diff for project %s", proj.Name)
				outputBytes, err = proj.GitRepo.RunCommand("log", "--oneline", "HEAD..@{upstream}")
				if err == nil {
					output = strings.TrimSpace(string(outputBytes))
				}
			case opts.Overview:
				log.Debug("Getting overview for project %s", proj.Name)
				outputBytes, err = proj.GitRepo.RunCommand("log", "--oneline", "-10")
				if err == nil {
					output = strings.TrimSpace(string(outputBytes))
				}
			case opts.CurrentBranch:
				log.Debug("Getting current branch for project %s", proj.Name)
				outputBytes, err = proj.GitRepo.RunCommand("rev-parse", "--abbrev-ref", "HEAD")
				if err == nil {
					output = strings.TrimSpace(string(outputBytes))
				}
			default:
				log.Debug("Getting status for project %s", proj.Name)
				outputBytes, err = proj.GitRepo.RunCommand("status", "--short")
				if err == nil {
					output = strings.TrimSpace(string(outputBytes))
				}
			}

			results <- infoResult{Project: proj, Output: output, Err: err}
		}(p)
	}

	// 等待所有goroutine完成后关闭结果通道
	go func() {
		wg.Wait()
		close(results)
	}()

	// 收集并显示结果
	for res := range results {
		if res.Err != nil {
			stats.mu.Lock()
			stats.failed++
			stats.mu.Unlock()

			log.Error("Error getting info for %s: %v", res.Project.Name, res.Err)
			continue
		}

		stats.mu.Lock()
		stats.success++
		stats.mu.Unlock()

		if res.Output != "" {
			log.Info("--- %s ---\n%s", res.Project.Name, res.Output)
		} else if !opts.Quiet {
			log.Info("--- %s ---\n(No changes)", res.Project.Name)
		}
	}

	// 显示统计信息
	log.Info("Info command completed: %d successful, %d failed", stats.success, stats.failed)

	if stats.failed > 0 {
		return fmt.Errorf("%d projects failed", stats.failed)
	}

	return nil
}

// showDiff 显示完整信息和提交差异
func showDiff(p *project.Project) {
	fmt.Println("Commit differences:")

	// 获取本地和远程分支之间的差异
	outputBytes, err := p.GitRepo.RunCommand("log", "--oneline", "HEAD..@{upstream}")
	if err != nil {
		fmt.Printf("Error getting commit diff: %v\n", err)
		return
	}

	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No differences found")
	}
}

// showLocalBranches 显示本地分支信息
func showLocalBranches(p *project.Project) {
	fmt.Println("Local branches:")

	outputBytes, err := p.GitRepo.RunCommand("branch")
	if err != nil {
		fmt.Printf("Error getting local branches: %v\n", err)
		return
	}

	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No local branches found")
	}
}

// showRemoteBranches 显示远程分支信息
func showRemoteBranches(p *project.Project) {
	fmt.Println("Remote branches:")

	outputBytes, err := p.GitRepo.RunCommand("branch", "-r")
	if err != nil {
		fmt.Printf("Error getting remote branches: %v\n", err)
		return
	}
	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No remote branches found")
	}
}

// showCurrentBranchInfo 显示当前分支信息
func showCurrentBranchInfo(p *project.Project) {
	fmt.Println("Current branch info:")

	// 获取当前分支的最近提
	outputBytes, err := p.GitRepo.RunCommand("log", "-1", "--oneline")
	if err != nil {
		fmt.Printf("Error getting current branch info: %v\n", err)
		return
	}
	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No commit info found")
	}
}

// showAllBranches 显示所有分支信
func showAllBranches(p *project.Project) {
	fmt.Println("All branches:")

	outputBytes, err := p.GitRepo.RunCommand("branch", "-a")
	if err != nil {
		fmt.Printf("Error getting all branches: %v\n", err)
		return
	}

	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No branches found")
	}
}

// showCommitOverview 显示所有本地提交的概览
func showCommitOverview(p *project.Project) {
	fmt.Println("Commit overview:")

	outputBytes, err := p.GitRepo.RunCommand("log", "--oneline", "-10")
	if err != nil {
		fmt.Printf("Error getting commit overview: %v\n", err)
		return
	}

	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No commits found")
	}
}

// showBasicInfo 显示基本信息
func showBasicInfo(p *project.Project) {
	// 获取最近的提交
	outputBytes, err := p.GitRepo.RunCommand("log", "-1", "--oneline")
	if err != nil {
		fmt.Printf("Error getting latest commit: %v\n", err)
		return
	}

	output := strings.TrimSpace(string(outputBytes))
	fmt.Printf("Latest commit: %s\n", output)

	// 获取未提交的更改
	outputBytes, err = p.GitRepo.RunCommand("status", "--short")
	if err != nil {
		fmt.Printf("Error getting status: %v\n", err)
		return
	}

	output = strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println("Uncommitted changes:")
		fmt.Println(output)
	} else {
		fmt.Println("Working directory clean")
	}
}
