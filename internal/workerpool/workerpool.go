package workerpool

import (
	"sync"
)

// WorkerPool 工作�?
type WorkerPool struct {
	workers int
	tasks   chan func()
	wg      sync.WaitGroup
	once    sync.Once
	quit    chan struct{}
}

// New 创建工作�?
func New(workers int) *WorkerPool {
	if workers <= 0 {
		workers = 1
	}

	pool := &WorkerPool{
		workers: workers,
		tasks:   make(chan func(), workers*2),
		quit:    make(chan struct{}),
	}
	pool.start()
	return pool
}

// start 启动工作�?
func (p *WorkerPool) start() {
	for i := 0; i < p.workers; i++ {
		go func() {
			for {
				select {
				case task, ok := <-p.tasks:
					if !ok {
						return
					}
					task()
				case <-p.quit:
					return
				}
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

// Wait 等待所有任务完�?
func (p *WorkerPool) Wait() {
	p.wg.Wait()
}

// Stop 停止工作�?
func (p *WorkerPool) Stop() {
	p.once.Do(func() {
		close(p.quit)
		close(p.tasks)
	})
}
