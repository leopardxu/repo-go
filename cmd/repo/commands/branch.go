package commands

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/spf13/cobra"
)

// BranchOptions holds the options for the branch command
type BranchOptions struct {
	All       bool
	Current   bool
	Color     string
	List      bool
	Verbose   bool
	SetUpstream string
	Jobs      int
	Quiet     bool
	Config    *config.Config // <-- Add this field
	CommonManifestOptions
}

// BranchCmd creates the branch command
func BranchCmd() *cobra.Command {
	opts := &BranchOptions{}

	cmd := &cobra.Command{
		Use:   "branches [<project>...]",
		Short: "View current topic branches",
		Long:  `Summarizes the currently available topic branches.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBranch(opts, args)
		},
	}

	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "show all branches")
	cmd.Flags().BoolVar(&opts.Current, "current", false, "consider only the current branch")
	cmd.Flags().StringVar(&opts.Color, "color", "auto", "control color usage: auto, always, never")
	cmd.Flags().BoolVarP(&opts.List, "list", "l", false, "list branches")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show hash and subject, give twice for upstream branch")
	cmd.Flags().StringVar(&opts.SetUpstream, "set-upstream", "", "set upstream for git pull/fetch")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &CommonManifestOptions{})

	return cmd
}

// runBranch executes the branch command logic
func runBranch(opts *BranchOptions, args []string) error {
	// 初始化日志系�?
	log := logger.NewDefaultLogger()
	if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	log.Debug("正在加载配置文件...")
	cfg, err := config.Load()
	if err != nil {
		log.Error("加载配置文件失败: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}
	
	log.Debug("正在解析清单文件 %s...", cfg.ManifestName)
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups,","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	
	log.Debug("正在初始化项目管理器...")
	manager := project.NewManagerFromManifest(manifestObj, cfg)

	var projects []*project.Project
	if len(args) == 0 {
		log.Debug("获取所有项�?..")
		projects, err = manager.GetProjectsInGroups(nil)
		if err != nil {
			log.Error("获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
		log.Debug("共获取到 %d 个项�?, len(projects))
	} else {
		log.Debug("根据名称获取项目: %v", args)
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			log.Error("根据名称获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects by names: %w", err)
		}
		log.Debug("共获取到 %d 个项�?, len(projects))
	}

	type branchResult struct {
		ProjectName string
		CurrentBranch string
		Branches []string
		Err error
	}
	
	log.Info("正在获取项目分支信息，并行任务数: %d...", opts.Jobs)
	
	results := make(chan branchResult, len(projects))
	sem := make(chan struct{}, opts.Jobs)
	var wg sync.WaitGroup
	
	for _, p := range projects {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			
			log.Debug("获取项目 %s 的分支信�?..", p.Name)
			
			currentBranchBytes, err := p.GitRepo.RunCommand("rev-parse", "--abbrev-ref", "HEAD")
			if err != nil {
				log.Error("获取项目 %s 的当前分支失�? %v", p.Name, err)
				results <- branchResult{ProjectName: p.Name, Err: err}
				return
			}
			
			branchArgs := []string{"branch", "--list"}
			if opts.All {
				branchArgs = append(branchArgs, "-a")
			}
			
			branchesOutputBytes, err := p.GitRepo.RunCommand(branchArgs...)
			if err != nil {
				log.Error("获取项目 %s 的分支列表失�? %v", p.Name, err)
				results <- branchResult{ProjectName: p.Name, Err: err}
				return
			}
			
			currentBranch := strings.TrimSpace(string(currentBranchBytes))
			branches := strings.Split(strings.TrimSpace(string(branchesOutputBytes)), "\n")
			
			log.Debug("项目 %s 当前分支: %s, 共有 %d 个分�?, p.Name, currentBranch, len(branches))
			results <- branchResult{ProjectName: p.Name, CurrentBranch: currentBranch, Branches: branches}
		}()
	}
	// 启动一�?goroutine 来关闭结果通道
	go func() {
		wg.Wait()
		close(results)
	}()
	
	branchInfo := make(map[string][]string)
	currentBranches := make(map[string]bool)
	successCount := 0
	failCount := 0
	
	// 收集结果
	for res := range results {
		if res.Err != nil {
			failCount++
			log.Error("获取项目 %s 的分支信息失�? %v", res.ProjectName, res.Err)
			continue
		}
		
		successCount++
		currentBranches[res.CurrentBranch] = true
		
		for _, branch := range res.Branches {
			branch = strings.TrimSpace(branch)
			if branch == "" {
				continue
			}
			
			// 处理分支名称，移除前导的 '*' 或空�?
			if strings.HasPrefix(branch, "* ") {
				branch = strings.TrimPrefix(branch, "* ")
			} else if strings.HasPrefix(branch, "  ") {
				branch = strings.TrimPrefix(branch, "  ")
			}
			
			branchInfo[branch] = append(branchInfo[branch], res.ProjectName)
		}
	}
	
	log.Debug("共处�?%d 个项目，成功: %d，失�? %d", len(projects), successCount, failCount)
	// 对分支名称进行排序，以便有序显示
	var branchNames []string
	for branch := range branchInfo {
		branchNames = append(branchNames, branch)
	}
	sort.Strings(branchNames)
	
	// 显示分支信息
	if !opts.Quiet {
		log.Info("分支信息汇�?")
		
		for _, branch := range branchNames {
			projs := branchInfo[branch]
			prefix := " "
			if currentBranches[branch] {
				prefix = "*"
			}
			
			if len(projs) == len(projects) {
				log.Info("%s %-30s | 所有项�?, prefix, branch)
			} else {
				log.Info("%s %-30s | 在项�? %s", prefix, branch, strings.Join(projs, ", "))
			}
		}
		
		log.Info("\n共有 %d 个分�?, len(branchNames))
	}
	
	return nil
}
