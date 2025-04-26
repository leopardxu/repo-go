package commands

import (
	"fmt"
	"sync"
	"errors"
	"strings"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// UploadOptions 包含upload命令的选项
type UploadOptions struct {
	Branch        string
	CurrentBranch bool
	Draft         bool
	Force         bool
	DryRun        bool
	PushOption    string
	Reviewers     string
	Topic         string
	NoVerify      bool
	Private       bool
	Wip           bool
	Jobs          int
	Hashtags      string
	HashtagBranch bool
	Labels        string
	CC            string
	NoEmails      bool
	Destination   string
	Yes           bool
	NoCertChecks  bool
	Verbose       bool
	Quiet         bool
	OuterManifest bool
	NoOuterManifest bool
	ThisManifestOnly bool
}

// UploadCmd 返回upload命令
func UploadCmd() *cobra.Command {
	opts := &UploadOptions{}

	cmd := &cobra.Command{
		Use:   "upload [--re --cc] [<project>...]",
		Short: "Upload changes for code review",
		Long:  `Upload changes to the code review system.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpload(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().StringVarP(&opts.Branch, "branch", "b", "", "upload specific branch")
	cmd.Flags().BoolVarP(&opts.CurrentBranch, "current-branch", "c", false, "upload current branch only")
	cmd.Flags().BoolVarP(&opts.Draft, "draft", "d", false, "upload as draft")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "force upload even if there are no changes")
	cmd.Flags().BoolVarP(&opts.DryRun, "dry-run", "n", false, "don't actually upload, just show what would be uploaded")
	cmd.Flags().StringVarP(&opts.PushOption, "push-option", "o", "", "push option for upload")
	cmd.Flags().StringVarP(&opts.Reviewers, "reviewers", "r", "", "request reviews from these people")
	cmd.Flags().StringVarP(&opts.Topic, "topic", "t", "", "topic for the change")
	cmd.Flags().BoolVar(&opts.NoVerify, "no-verify", false, "bypass pre-upload hook")
	cmd.Flags().BoolVar(&opts.Private, "private", false, "upload as private")
	cmd.Flags().BoolVar(&opts.Wip, "wip", false, "upload as work in progress")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	cmd.Flags().StringVar(&opts.Hashtags, "hashtag", "", "add hashtags (comma delimited) to the review")
	cmd.Flags().BoolVar(&opts.HashtagBranch, "hashtag-branch", false, "add local branch name as a hashtag")
	cmd.Flags().StringVar(&opts.Labels, "label", "", "add a label when uploading")
	cmd.Flags().StringVar(&opts.CC, "cc", "", "also send email to these email addresses")
	cmd.Flags().StringVar(&opts.Destination, "destination", "", "submit for review on this target branch")
	cmd.Flags().BoolVar(&opts.NoEmails, "no-emails", false, "do not send e-mails on upload")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "answer yes to all safe prompts")
	cmd.Flags().BoolVar(&opts.NoCertChecks, "no-cert-checks", false, "disable verifying ssl certs (unsafe)")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runUpload 执行upload命令
func runUpload(opts *UploadOptions, args []string) error {
	fmt.Println("Uploading changes for code review")

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

	// 获取要处理的项目
	var projects []*project.Project
	if len(args) == 0 {
		// 如果没有指定项目，则处理所有项目
		projects, err = manager.GetProjects(nil)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	// 构建上传选项
	uploadArgs := []string{"push"}
	
	// 添加目标分支
	if opts.Branch != "" {
		uploadArgs = append(uploadArgs, "origin", opts.Branch)
	}
	
	// 添加其他选项
	if opts.Draft {
		uploadArgs = append(uploadArgs, "--draft")
	}
	
	if opts.NoVerify {
		uploadArgs = append(uploadArgs, "--no-verify")
	}
	
	if opts.PushOption != "" {
		uploadArgs = append(uploadArgs, "--push-option="+opts.PushOption)
	}
	
	if opts.Topic != "" {
		uploadArgs = append(uploadArgs, "--topic="+opts.Topic)
	}
	
	if opts.Hashtags != "" {
		uploadArgs = append(uploadArgs, "--hashtag="+opts.Hashtags)
	}
	
	if opts.HashtagBranch {
		uploadArgs = append(uploadArgs, "--hashtag-branch")
	}
	
	if opts.Labels != "" {
		uploadArgs = append(uploadArgs, "--label="+opts.Labels)
	}
	
	if opts.CC != "" {
		uploadArgs = append(uploadArgs, "--cc="+opts.CC)
	}
	
	if opts.Destination != "" {
		uploadArgs = append(uploadArgs, "--destination="+opts.Destination)
	}
	
	if opts.NoEmails {
		uploadArgs = append(uploadArgs, "--no-emails")
	}
	
	if opts.Private {
		uploadArgs = append(uploadArgs, "--private")
	}
	
	if opts.Wip {
		uploadArgs = append(uploadArgs, "--wip")
	}
	
	if opts.Yes {
		uploadArgs = append(uploadArgs, "--yes")
	}
	
	if opts.NoCertChecks {
		uploadArgs = append(uploadArgs, "--no-cert-checks")
	}

	// 创建goroutine池和工作通道
	errChan := make(chan error, len(projects))
	sem := make(chan struct{}, opts.Jobs)
	var wg sync.WaitGroup

	// 并发上传每个项目
	for _, p := range projects {
		p := p
		wg.Add(1)
		go func() {
			sem <- struct{}{}
			defer func() {
				<-sem
				wg.Done()
			}()

			// 如果指定了--current-branch，检查当前分支
			if opts.CurrentBranch {
				currentBranch, err := p.GitRepo.CurrentBranch()
				if err != nil {
					errChan <- fmt.Errorf("failed to get current branch of project %s: %w", p.Name, err)
					return
				}
				
				// 如果当前分支是清单中指定的分支，跳过
				if currentBranch == p.Revision {
					if !opts.Quiet {
						fmt.Printf("Skipping project %s (current branch is manifest branch)\n", p.Name)
					}
					return
				}
			}
			
			// 检查是否有更改
			hasChanges, err := p.GitRepo.HasChangesToPush("origin")
			if err != nil {
				errChan <- fmt.Errorf("failed to check if project %s has changes: %w", p.Name, err)
				return
			}
			
			if !hasChanges && !opts.Force {
				if !opts.Quiet {
					fmt.Printf("Skipping project %s (no changes to upload)\n", p.Name)
				}
				return
			}
			
			if !opts.Quiet {
				fmt.Printf("Uploading changes from project %s\n", p.Name)
			}
			
			// 如果是模拟运行，不实际上传
			if opts.DryRun {
				fmt.Printf("Would upload changes from project %s with command: git %s\n", p.Name, uploadArgs)
				return
			}
			
			// 执行上传命令
			outputBytes, err := p.GitRepo.RunCommand(uploadArgs...)
			if err != nil {
				errChan <- fmt.Errorf("failed to upload changes from project %s: %w\n%s", p.Name, err, string(outputBytes))
				return
			}
			
			if !opts.Quiet {
				fmt.Printf("Successfully uploaded changes from project %s\n", p.Name)
				output := strings.TrimSpace(string(outputBytes))
				if output != "" {
					fmt.Println(output)
				}
			}
		}()
	}

	// 等待所有goroutine完成
	wg.Wait()
	close(errChan)

	// 收集错误
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		if !opts.Quiet {
			fmt.Printf("Upload completed with %d errors\n", len(errs))
		}
		return fmt.Errorf("encountered %d errors while uploading: %v", len(errs), errors.Join(errs...))
	}

	if !opts.Quiet {
		fmt.Println("Upload completed successfully")
	}
	return nil
}