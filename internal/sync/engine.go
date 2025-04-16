package sync

import (
	"fmt"
	"sync"

	"github.com/cix-code/gogo/internal/project"
)

// Options 同步选项
type Options struct {
	Jobs           int
	JobsNetwork    int
	JobsCheckout   int
	CurrentBranch  bool
	Detach         bool
	ForceSync      bool
	ForceRemoveDirty bool
	ForceOverwrite bool
	LocalOnly      bool
	NetworkOnly    bool
	Prune          bool
	Quiet          bool
	SmartSync      bool
	Tags           bool
	NoCloneBundle  bool
	FetchSubmodules bool
	NoTags         bool
	OptimizedFetch bool
	RetryFetches   int
	Groups         []string
	FailFast       bool
	NoManifestUpdate bool
	UseSuperproject bool
	HyperSync      bool
	SmartTag       string
	OuterManifest  bool
	ThisManifestOnly bool
}

// Engine 同步引擎
type Engine struct {
	Projects []*project.Project
	Options  *Options
}

// NewEngine 创建同步引擎
func NewEngine(projects []*project.Project, options *Options) *Engine {
	return &Engine{
		Projects: projects,
		Options:  options,
	}
}

// Run 执行同步
func (e *Engine) Run() error {
	// 创建工作池
	sem := make(chan struct{}, e.Options.Jobs)
	var wg sync.WaitGroup
	
	// 错误收集
	errChan := make(chan error, len(e.Projects))
	
	// 并行同步
	for _, p := range e.Projects {
		wg.Add(1)
		go func(p *project.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			
			if err := e.syncProject(p); err != nil {
				errChan <- fmt.Errorf("failed to sync project %s: %w", p.Name, err)
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
		return NewMultiError(errors)
	}
	
	return nil
}

// syncProject 同步单个项目
func (e *Engine) syncProject(p *project.Project) error {
	// 创建同步选项
	opts := project.SyncOptions{
		Depth:       0, // 使用配置中的深度
		Force:       e.Options.ForceSync,
		LocalOnly:   e.Options.LocalOnly,
		NetworkOnly: e.Options.NetworkOnly,
		Prune:       e.Options.Prune,
		Tags:        e.Options.Tags,
		Detach:      e.Options.Detach,
		Current:     e.Options.CurrentBranch,
		DryRun:      false, // 暂不支持
		Quiet:       e.Options.Quiet,
	}
	
	// 执行同步
	return p.Sync(opts)
}

// MultiError 表示多个错误
type MultiError struct {
	Errors []error
}

// NewMultiError 创建多错误
func NewMultiError(errors []error) *MultiError {
	return &MultiError{Errors: errors}
}

// Error 实现error接口
func (m *MultiError) Error() string {
	if len(m.Errors) == 1 {
		return m.Errors[0].Error()
	}
	
	return fmt.Sprintf("%d errors occurred during sync", len(m.Errors))
}