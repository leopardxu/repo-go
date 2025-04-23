package repo_sync

import (
	"context"
	"sync"

	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"golang.org/x/sync/errgroup"
)

// Sync 执行仓库同步
func (e *Engine) Sync() error {
	// 加载清单但不打印日志
	if err := e.loadManifestSilently(); err != nil {
		return err
	}

	// 使用goroutine池控制并发
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(e.options.Jobs)

	var wg sync.WaitGroup
	for _, p := range e.projects {
		p := p
		wg.Add(1)
		g.Go(func() error {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return e.syncProject(p)
			}
		})
	}

	wg.Wait()
	return g.Wait()
}

// loadManifestSilently 静默加载清单
func (e *Engine) loadManifestSilently() error {
	parser := manifest.NewParser()
	m, err := parser.ParseFromFile(e.config.ManifestName)
	if err != nil {
		return err
	}
	e.manifest = m
	return nil
}

// syncProject 同步单个项目
func (e *Engine) syncProject(p *project.Project) error {
	// 实现项目同步逻辑
	return nil
}