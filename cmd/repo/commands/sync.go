package commands

import (
	"fmt"
	"runtime"
	"strings"
	"os"
	"sync"
	"path/filepath"
	
	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/git"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/repo_sync"
	"github.com/spf13/cobra"
)

// SyncOptions 包含sync命令的选项
type SyncOptions struct {
	Jobs           int
	JobsNetwork    int
	JobsCheckout   int
	CurrentBranch  bool
	NoCurrentBranch bool
	Detach         bool
	ForceSync      bool
	ForceRemoveDirty bool
	ForceOverwrite bool
	LocalOnly      bool
	NetworkOnly    bool
	Prune          bool
	Quiet          bool
	Verbose        bool // 是否显示详细日志
	SmartSync      bool
	Tags           bool
	NoCloneBundle  bool
	FetchSubmodules bool
	NoTags         bool
	OptimizedFetch bool
	RetryFetches   int
	Groups         string
	FailFast       bool
	NoManifestUpdate bool
	ManifestServerUsername string
	ManifestServerPassword string
	UseSuperproject        bool
	NoUseSuperproject      bool
	HyperSync              bool
	SmartTag               string
	NoThisManifestOnly     bool
	Config                 *config.Config
	CommonManifestOptions
}

// SyncCmd 返回sync命令
func SyncCmd() *cobra.Command {
	opts := &SyncOptions{
		Jobs:         runtime.NumCPU() * 2,
		RetryFetches: 3,
	}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Update working tree to the latest revision",
		Long:  `Synchronize the local repository with the remote repositories.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of parallel jobs")
	cmd.Flags().IntVar(&opts.JobsNetwork, "jobs-network", opts.Jobs, "number of network jobs to run in parallel")
	cmd.Flags().IntVar(&opts.JobsCheckout, "jobs-checkout", opts.Jobs, "number of local checkout jobs to run in parallel")
	cmd.Flags().BoolVarP(&opts.CurrentBranch, "current-branch", "c", false, "fetch only current branch")
	cmd.Flags().BoolVar(&opts.NoCurrentBranch, "no-current-branch", false, "fetch all branches from server")
	cmd.Flags().BoolVarP(&opts.Detach, "detach", "d", false, "detach projects back to manifest revision")
	cmd.Flags().BoolVarP(&opts.ForceSync, "force-sync", "f", false, "overwrite local changes")
	cmd.Flags().BoolVar(&opts.ForceRemoveDirty, "force-remove-dirty", false, "force remove projects with uncommitted modifications")
	cmd.Flags().BoolVar(&opts.ForceOverwrite, "force-overwrite", false, "force cleanup local uncommitted changes")
	cmd.Flags().BoolVarP(&opts.LocalOnly, "local-only", "l", false, "only update working tree, don't fetch")
	cmd.Flags().BoolVar(&opts.NoManifestUpdate, "no-manifest-update", false, "use the existing manifest checkout as-is")
	cmd.Flags().BoolVar(&opts.NoManifestUpdate, "nmu", false, "use the existing manifest checkout as-is")
	cmd.Flags().BoolVarP(&opts.NetworkOnly, "network-only", "n", false, "fetch only, don't update working tree")
	cmd.Flags().BoolVarP(&opts.Prune, "prune", "p", false, "delete projects not in manifest")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "be quiet")
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "show detailed output")
	cmd.Flags().BoolVarP(&opts.SmartSync, "smart-sync", "s", false, "smart sync using manifest from the latest known good build")
	cmd.Flags().BoolVarP(&opts.Tags, "tags", "t", false, "fetch tags")
	cmd.Flags().BoolVar(&opts.NoCloneBundle, "no-clone-bundle", false, "disable use of /clone.bundle on HTTP/HTTPS")
	cmd.Flags().BoolVar(&opts.FetchSubmodules, "fetch-submodules", false, "fetch submodules")
	cmd.Flags().BoolVar(&opts.NoTags, "no-tags", false, "don't fetch tags")
	cmd.Flags().BoolVar(&opts.OptimizedFetch, "optimized-fetch", false, "only fetch projects fixed to sha1 if revision does not exist locally")
	cmd.Flags().IntVar(&opts.RetryFetches, "retry-fetches", opts.RetryFetches, "number of times to retry fetches")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "restrict to projects matching the specified groups")
	cmd.Flags().BoolVar(&opts.FailFast, "fail-fast", false, "stop syncing after first error is hit")
	cmd.Flags().BoolVar(&opts.UseSuperproject, "use-superproject", false, "use the manifest superproject to sync projects")
	cmd.Flags().BoolVar(&opts.NoUseSuperproject, "no-use-superproject", false, "disable use of manifest superprojects")
	cmd.Flags().BoolVar(&opts.HyperSync, "hyper-sync", false, "only update projects changed on git server")
	cmd.Flags().StringVar(&opts.SmartTag, "smart-tag", "", "smart sync using manifest from a known tag")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")
	cmd.Flags().BoolVar(&opts.NoThisManifestOnly, "all-manifests", false, "operate on this manifest and its submanifests")
	cmd.Flags().StringVarP(&opts.ManifestServerUsername, "manifest-server-username", "u", "", "username to authenticate with the manifest server")
	cmd.Flags().StringVarP(&opts.ManifestServerPassword, "manifest-server-password", "w", "", "password to authenticate with the manifest server")

	return cmd
}

// runSync 执行sync命令
func runSync(opts *SyncOptions, args []string) error {
    // Load config
    cfg, err := config.Load()
    if err != nil {
        return fmt.Errorf("failed to load config: %w", err)
    }
    opts.Config = cfg

    // 检查 manifest.xml 文件是否存在
    manifestPath := filepath.Join(cfg.RepoRoot, ".repo", "manifest.xml")
    if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
        return fmt.Errorf("manifest.xml文件不存在，请先运行 'repo init' 命令")
    }

    // Load manifest
    parser := manifest.NewParser()
    manifestObj, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
    if err != nil {
        return fmt.Errorf("failed to parse manifest: %w", err)
    }

    // Create project manager
    manager := project.NewManager(manifestObj, cfg)

    var projects []*project.Project
    if len(args) == 0 {
        projects, err = manager.GetProjects(nil)
    } else {
        projects, err = manager.GetProjectsByNames(args)
    }
    
    if err != nil {
        return fmt.Errorf("获取项目失败: %w", err)
    }
    
    // 过滤项目列表，根据groups参数
    if opts.Groups != "" {
        groups := strings.Split(opts.Groups, ",")
        projects = filterProjectsByGroups(projects, groups)
    }

    // 创建同步引擎
    engine := repo_sync.NewEngine(projects, &repo_sync.Options{
        Jobs:           opts.Jobs,
        JobsNetwork:    opts.JobsNetwork,
        JobsCheckout:   opts.JobsCheckout,
        CurrentBranch:  opts.CurrentBranch && !opts.NoCurrentBranch,
        Detach:         opts.Detach,
        ForceSync:      opts.ForceSync,
        ForceRemoveDirty: opts.ForceRemoveDirty,
        ForceOverwrite: opts.ForceOverwrite,
        LocalOnly:      opts.LocalOnly,
        NetworkOnly:    opts.NetworkOnly,
        Prune:          opts.Prune,
        Quiet:          opts.Quiet,
        Verbose:        opts.Verbose,
        SmartSync:      opts.SmartSync,
        Tags:           opts.Tags && !opts.NoTags,
        NoCloneBundle:  opts.NoCloneBundle,
        FetchSubmodules: opts.FetchSubmodules,
        OptimizedFetch: opts.OptimizedFetch,
        RetryFetches:   opts.RetryFetches,
        Groups:         nil, // 已在前面处理过groups
        FailFast:       opts.FailFast,
        NoManifestUpdate: opts.NoManifestUpdate,
        UseSuperproject: opts.UseSuperproject && !opts.NoUseSuperproject,
        HyperSync:      opts.HyperSync,
        SmartTag:       opts.SmartTag,
        ManifestServerUsername: opts.ManifestServerUsername,
        ManifestServerPassword: opts.ManifestServerPassword,
    }, manifestObj, cfg)
    
    // 执行同步前检查并克隆缺失的项目
    if !opts.Quiet {
        fmt.Println("检查项目目录状态...")
    }
    
    var wg sync.WaitGroup
    sem := make(chan struct{}, opts.JobsNetwork)
    for _, p := range projects {
        if _, err := os.Stat(p.Worktree); os.IsNotExist(err) {
            wg.Add(1)
            go func(proj *project.Project) {
                defer wg.Done()
                sem <- struct{}{}
                defer func() { <-sem }()
                
                if !opts.Quiet {
                    fmt.Printf("正在克隆缺失项目: %s\n", proj.Name)
                }
                // 创建项目目录
                if err := os.MkdirAll(filepath.Dir(proj.Worktree), 0755); err != nil {
                    fmt.Printf("创建项目目录失败 %s: %v\n", proj.Name, err)
                    return
                }
                
                // 检查remoteURL是否有效
					if proj.RemoteURL == "" {
						if !opts.Quiet {
							fmt.Printf("无法获取项目 %s 的远程URL，请检查manifest配置\n", proj.Name)
						}
						return
					}
					
					// 验证URL格式 - 支持HTTP/HTTPS/SSH/Git协议和本地路径
					isValidURL := strings.HasPrefix(proj.RemoteURL, "http://") || 
					   strings.HasPrefix(proj.RemoteURL, "https://") || 
					   strings.HasPrefix(proj.RemoteURL, "ssh://") || 
					   strings.HasPrefix(proj.RemoteURL, "git@") ||
					   strings.HasPrefix(proj.RemoteURL, "file://") ||
					   strings.HasPrefix(proj.RemoteURL, "/") ||
					   strings.HasPrefix(proj.RemoteURL, "./") ||
					   strings.HasPrefix(proj.RemoteURL, "../")
					   
					if !isValidURL {
						// 如果不是有效的URL格式，尝试从配置中获取基础URL
						if cfg.ManifestURL != "" {
							baseURL := cfg.GetRemoteURL()
							if baseURL != "" {
								proj.RemoteURL = baseURL + "/" + proj.Name + ".git"
								isValidURL = true
							}
						}
						
						if !isValidURL && !opts.Quiet {
							fmt.Printf("项目 %s 的远程URL格式无效: %s\n", proj.Name, proj.RemoteURL)
							return
						}
					}
					
					// 只在非静默模式下输出调试信息
					if !opts.Quiet {
						fmt.Printf("验证项目 %s 的远程URL: %s\n", proj.Name, proj.RemoteURL)
					}
                
                // 使用GitRepo.Clone方法克隆项目
                if err := proj.GitRepo.Clone(proj.RemoteURL, git.CloneOptions{
                    Branch: proj.Revision,
                }); err != nil {
                    // 只在非静默模式下输出错误信息
                    if !opts.Quiet {
                        // 如果是详细模式，输出完整错误信息
                        if opts.Verbose {
                            if gitErr, ok := err.(*git.CommandError); ok {
                                fmt.Printf("克隆项目 %s 失败:\n错误: %v\n输出: %s\n错误输出: %s\n", 
                                    proj.Name, gitErr.Err, gitErr.Stdout, gitErr.Stderr)
                            } else {
                                fmt.Printf("克隆项目 %s 失败: %v\n", proj.Name, err)
                            }
                        } else {
                            // 非详细模式下只输出简短错误信息
                            fmt.Printf("克隆项目 %s 失败: %v\n", proj.Name, err)
                        }
                    }
                }
            }(p)
        }
    }
    wg.Wait()
    
    // 执行同步
    if !opts.Quiet {
        fmt.Println("开始同步项目...")
    }
    
    // 执行同步并检查结果
    if err := engine.Sync(); err != nil {
        if !opts.Quiet {
            // 增强错误输出，提供更详细的信息
            fmt.Printf("同步完成，但有错误: %v\n", err)
            
            // 显示错误详情
            if len(engine.Errors()) > 0 {
                fmt.Println("错误详情:")
                for i, errMsg := range engine.Errors() {
                    fmt.Printf("  错误 %d: %s\n", i+1, errMsg)
                    
                    // 针对exit status 128错误提供额外信息
                    if strings.Contains(errMsg, "exit status 128") {
                        fmt.Println("  可能的原因:")
                        fmt.Println("    - 远程仓库不存在或无法访问")
                        fmt.Println("    - 没有访问权限")
                        fmt.Println("    - 网络连接问题")
                        fmt.Println("    - Git配置问题")
                        fmt.Println("  建议解决方案:")
                        fmt.Println("    - 检查网络连接")
                        fmt.Println("    - 验证远程仓库URL是否正确")
                        fmt.Println("    - 确认您有访问权限")
                        fmt.Println("    - 尝试手动执行git命令以获取更详细的错误信息")
                    }
                }
            }
        }
        return fmt.Errorf("同步失败: %w", err)
    }
    
    // 验证同步结果
    if !opts.Quiet {
        fmt.Println("正在验证同步结果...")
        for _, p := range projects {
            if _, err := os.Stat(p.Worktree); os.IsNotExist(err) {
                return fmt.Errorf("项目目录 %q 不存在，请检查项目配置", p.Worktree)
            }
            if _, err := p.GitRepo.RunCommand("rev-parse", "HEAD"); err != nil {
                return fmt.Errorf("项目 %s 同步验证失败: %w (工作目录: %q)", p.Name, err, p.Worktree)
            }
        }
        fmt.Println("同步成功完成")
    }
    return nil
}
    // filterProjectsByGroups 根据组过滤项目列表
func filterProjectsByGroups(projects []*project.Project, groups []string) []*project.Project {
    var filtered []*project.Project
    for _, p := range projects {
        if p.IsInAnyGroup(groups) {
            filtered = append(filtered, p)
        }
    }
    return filtered
}