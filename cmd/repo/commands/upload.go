package commands

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/logger"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
	"runtime"
)

// UploadOptions 包含upload命令的选项
type UploadOptions struct {
	Branch          string
	CurrentBranch   bool
	Draft           bool
	Force           bool
	DryRun          bool
	PushOption      string
	Reviewers       string
	Topic           string
	NoVerify        bool
	Private         bool
	Wip             bool
	Jobs            int
	Hashtags        string
	HashtagBranch   bool
	Labels          string
	CC              string
	NoEmails        bool
	Destination     string
	Yes             bool
	NoCertChecks    bool
	Verbose         bool
	Quiet           bool
	OuterManifest   bool
	NoOuterManifest bool
	ThisManifestOnly bool
	// 添加配置字段，避免重复加载
	Config          *config.Config
}

// uploadStats 用于统计上传结果
type uploadStats struct {
	mu      sync.Mutex
	total   int
	success int
	failed  int
}

// increment 增加计数
func (s *uploadStats) increment(success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if success {
		s.success++
	} else {
		s.failed++
	}
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
	cmd.Flags().StringVarP(&opts.Branch, "branch", "b", "", "上传指定分支")
	cmd.Flags().BoolVarP(&opts.CurrentBranch, "current-branch", "c", false, "仅上传当前分支")
	cmd.Flags().BoolVarP(&opts.Draft, "draft", "d", false, "上传为草稿状态")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "强制上传，即使没有变更")
	cmd.Flags().BoolVarP(&opts.DryRun, "dry-run", "n", false, "不实际上传，仅显示将要上传的内容")
	cmd.Flags().StringVarP(&opts.PushOption, "push-option", "o", "", "上传的推送选项")
	cmd.Flags().StringVarP(&opts.Reviewers, "reviewers", "r", "", "请求这些人进行代码审查")
	cmd.Flags().StringVarP(&opts.Topic, "topic", "t", "", "变更的主题")
	cmd.Flags().BoolVar(&opts.NoVerify, "no-verify", false, "绕过上传前钩子")
	cmd.Flags().BoolVar(&opts.Private, "private", false, "上传为私有状态")
	cmd.Flags().BoolVar(&opts.Wip, "wip", false, "上传为进行中状态")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", runtime.NumCPU()*2, "并行运行的任务数量")
	cmd.Flags().StringVar(&opts.Hashtags, "hashtag", "", "添加标签（逗号分隔）到审查中")
	cmd.Flags().BoolVar(&opts.HashtagBranch, "hashtag-branch", false, "将本地分支名添加为标签")
	cmd.Flags().StringVar(&opts.Labels, "label", "", "上传时添加标签")
	cmd.Flags().StringVar(&opts.CC, "cc", "", "同时发送邮件给这些邮箱地址")
	cmd.Flags().StringVar(&opts.Destination, "destination", "", "提交到此目标分支进行审查")
	cmd.Flags().BoolVar(&opts.NoEmails, "no-emails", false, "上传时不发送邮件")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "对所有安全提示回答是")
	cmd.Flags().BoolVar(&opts.NoCertChecks, "no-cert-checks", false, "禁用SSL证书验证（不安全）")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "显示详细输出，包括调试信息")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "仅显示错误信息")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "从最外层清单开始操作")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "不操作外层清单")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "仅操作此（子）清单")

	return cmd
}

// runUpload 执行upload命令
func runUpload(opts *UploadOptions, args []string) error {
	// 创建日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	log.Info("开始上传代码变更进行审查")

	// 加载配置
	var err error
	if opts.Config == nil {
		opts.Config, err = config.Load()
		if err != nil {
			log.Error("加载配置失败: %v", err)
			return fmt.Errorf("加载配置失败: %w", err)
		}
	}

	// 加载清单
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName, strings.Split(opts.Config.Groups, ","))
	if err != nil {
		log.Error("解析清单失败: %v", err)
		return fmt.Errorf("解析清单失败: %w", err)
	}

	// 创建项目管理器
	manager := project.NewManagerFromManifest(manifest, opts.Config)

	// 获取要处理的项目
	var projects []*project.Project
	if len(args) == 0 {
		// 如果没有指定项目，则处理所有项目
		log.Debug("未指定项目，将处理所有项目")
		projects, err = manager.GetProjectsInGroups(nil)
		if err != nil {
			log.Error("获取所有项目失败: %v", err)
			return fmt.Errorf("获取所有项目失败: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		log.Debug("将处理指定的项目: %v", args)
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			log.Error("获取指定项目失败: %v", err)
			return fmt.Errorf("获取指定项目失败: %w", err)
		}
	}

	log.Info("共有 %d 个项目需要处理", len(projects))

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

	log.Debug("上传命令参数: git %s", strings.Join(uploadArgs, " "))

	// 创建统计对象
	stats := &uploadStats{}

	// 创建错误通道和工作通道
	errChan := make(chan error, len(projects))
	sem := make(chan struct{}, opts.Jobs)
	var wg sync.WaitGroup

	log.Info("开始并行处理项目，并发数: %d", opts.Jobs)

	// 并发上传每个项目
	for _, p := range projects {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			log.Debug("处理项目: %s", p.Name)

			// 如果指定了--current-branch，检查当前分支
			if opts.CurrentBranch {
				currentBranch, err := p.GitRepo.CurrentBranch()
				if err != nil {
					errMsg := fmt.Sprintf("获取项目 %s 的当前分支失败: %v", p.Name, err)
					log.Error(errMsg)
					errChan <- fmt.Errorf(errMsg)
					stats.increment(false)
					return
				}

				// 如果当前分支是清单中指定的分支，跳过
				if currentBranch == p.Revision {
					log.Info("跳过项目 %s (当前分支是清单分支)", p.Name)
					stats.increment(true) // 视为成功，因为这是预期行为
					return
				}
			}

			// 检查是否有更改
			hasChanges, err := p.GitRepo.HasChangesToPush("origin")
			if err != nil {
				errMsg := fmt.Sprintf("检查项目 %s 是否有变更失败: %v", p.Name, err)
				log.Error(errMsg)
				errChan <- fmt.Errorf(errMsg)
				stats.increment(false)
				return
			}

			if !hasChanges && !opts.Force {
				log.Info("跳过项目 %s (没有变更需要上传)", p.Name)
				stats.increment(true) // 视为成功，因为这是预期行为
				return
			}

			log.Info("正在上传项目 %s 的变更", p.Name)

			// 如果是模拟运行，不实际上传
			if opts.DryRun {
				log.Info("模拟运行: 将上传项目 %s 的变更，命令: git %s", p.Name, strings.Join(uploadArgs, " "))
				stats.increment(true)
				return
			}

			// 执行上传命令
			outputBytes, err := p.GitRepo.RunCommand(uploadArgs...)
			if err != nil {
				errMsg := fmt.Sprintf("上传项目 %s 的变更失败: %v\n%s", p.Name, err, string(outputBytes))
				log.Error(errMsg)
				errChan <- fmt.Errorf(errMsg)
				stats.increment(false)
				return
			}

			log.Info("成功上传项目 %s 的变更", p.Name)
			output := strings.TrimSpace(string(outputBytes))
			if output != "" {
				log.Debug("上传输出:\n%s", output)
			}
			stats.increment(true)
		}()
	}

	// 等待所有goroutine完成
	log.Debug("等待所有上传任务完成")
	wg.Wait()
	close(errChan)

	// 收集错误
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	// 输出统计信息
	log.Info("上传完成，总计: %d, 成功: %d, 失败: %d", stats.total, stats.success, stats.failed)

	if len(errs) > 0 {
		log.Error("上传过程中遇到 %d 个错误", len(errs))
		return errors.Join(errs...)
	}

	log.Info("所有项目上传成功完成")
	return nil
}