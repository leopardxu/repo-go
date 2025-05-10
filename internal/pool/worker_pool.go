package pool

import (
	"sync"
)

// WorkerPool 工作池
type WorkerPool struct {
	workers int
	tasks   chan func()
	wg      sync.WaitGroup
}

// NewWorkerPool 创建工作池
func NewWorkerPool(workers int) *WorkerPool {
	pool := &WorkerPool{
		workers: workers,
		tasks:   make(chan func(), workers*2),
	}
	pool.start()
	return pool
}

// start 启动工作池
func (p *WorkerPool) start() {
	for i := 0; i < p.workers; i++ {
		go func() {
			for task := range p.tasks {
				task()
			}
		}()
	}
}

// Submit 提交任务
func (p *WorkerPool) Submit(task func()) {
	p.wg.Add(1)
	p.tasks <- func() {
		defer p.wg.Done()
		task()
	}
}

// Wait 等待所有任务完成
func (p *WorkerPool) Wait() {
	p.wg.Wait()
}

// Stop 停止工作池
func (p *WorkerPool) Stop() {
	close(p.tasks)
}