package repo_sync

import (
	"time"
	"github.com/leopardxu/repo-go/internal/config"
)

// Options 包含同步选项
type Options struct {
	NetworkOnly     bool
	LocalOnly       bool
	CurrentBranch   bool
	Detach          bool
	Force           bool
	NoTags          bool
	Prune           bool
	Jobs            int
	JobsNetwork     int
	JobsCheckout    int
	SmartSync       bool
	SmartTag        string
	UseSuperproject bool
	HyperSync       bool
	Verbose         bool
	Quiet           bool
	Tags            bool
	GitLFS          bool // 添加 GitLFS 字段
	ForceSync       bool
	ForceOverwrite  bool
	ForceRemoveDirty bool // 添加 ForceRemoveDirty 字段
	FailFast        bool
	HTTPTimeout     time.Duration
	ManifestServerUsername string
	ManifestServerPassword string
	Groups          []string // 修改为字符串数组
	Debug           bool // 添加 Debug 字段
	OptimizedFetch  bool // 添加 OptimizedFetch 字段
	RetryFetches    int  // 添加 RetryFetches 字段
	NoCloneBundle   bool // 添加 NoCloneBundle 字段
	Depth           int  // 添加 Depth 字段
	FetchSubmodules bool // 添加 FetchSubmodules 字段
	NoManifestUpdate bool // 添加 NoManifestUpdate 字段
	DryRun          bool // 添加 DryRun 字段，用于模拟执行但不实际修�?
	Config          *config.Config // 添加 Config 字段，用于存储配置信�?
	DefaultRemote   string // 添加 DefaultRemote 字段，用于指定默认远�?
}
