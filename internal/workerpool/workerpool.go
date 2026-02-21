package workerpool

import (
	"context"
	"sync"
)

// TaskResult 任务执行结果
type TaskResult struct {
	Error error
	Data  interface{}
}

// Task 任务定义
type Task struct {
	Fn   func() (interface{}, error)
	Done chan TaskResult
}

// WorkerPool 工作池
type WorkerPool struct {
	workers     int
	tasks       chan Task
	once        sync.Once
	quit        chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	activeTasks sync.WaitGroup // 追踪活跃任务数
	closed      bool           // 标记 tasks channel 是否已关闭
	closeMu     sync.Mutex     // 保护 closed 标记
}

// New 创建工作池
func New(workers int) *WorkerPool {
	if workers <= 0 {
		workers = 1
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &WorkerPool{
		workers: workers,
		tasks:   make(chan Task, workers*2),
		quit:    make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
	pool.start()
	return pool
}

// start 启动工作协程
// 每个 worker 直接在自己的 goroutine 中执行任务，确保并发度 = workers 数
func (p *WorkerPool) start() {
	for i := 0; i < p.workers; i++ {
		go func() {
			for {
				select {
				case task, ok := <-p.tasks:
					if !ok {
						return
					}
					p.activeTasks.Add(1)
					// 直接在 worker goroutine 中执行任务，不再启动额外 goroutine
					// 这样确保同时执行的任务数不会超过 workers 数
					result, err := task.Fn()
					task.Done <- TaskResult{Error: err, Data: result}
					p.activeTasks.Done()
				case <-p.quit:
					return
				case <-p.ctx.Done():
					return
				}
			}
		}()
	}
}

// Submit 提交任务
func (p *WorkerPool) Submit(fn func() (interface{}, error)) <-chan TaskResult {
	done := make(chan TaskResult, 1)
	task := Task{
		Fn:   fn,
		Done: done,
	}

	select {
	case p.tasks <- task:
	case <-p.ctx.Done():
		result := TaskResult{Error: p.ctx.Err()}
		select {
		case done <- result:
		default:
		}
		close(done)
		return done
	}

	return done
}

// SubmitAndWait 提交任务并等待完成
func (p *WorkerPool) SubmitAndWait(fn func() (interface{}, error)) (interface{}, error) {
	resultChan := p.Submit(fn)
	result := <-resultChan
	return result.Data, result.Error
}

// Wait 等待所有已提交的任务完成
func (p *WorkerPool) Wait() {
	// 等待所有活跃任务完成
	p.activeTasks.Wait()

	// 安全关闭任务通道（仅关闭一次）
	p.closeMu.Lock()
	if !p.closed {
		p.closed = true
		close(p.tasks)
	}
	p.closeMu.Unlock()
}

// Stop 停止工作池
func (p *WorkerPool) Stop() {
	p.once.Do(func() {
		p.cancel()
		close(p.quit)
		p.closeMu.Lock()
		if !p.closed {
			p.closed = true
			close(p.tasks)
		}
		p.closeMu.Unlock()
	})
}

// SubmitBatch 提交一批任务并等待全部完成
func (p *WorkerPool) SubmitBatch(fns []func() (interface{}, error)) []TaskResult {
	results := make([]TaskResult, len(fns))
	var wg sync.WaitGroup

	for i, fn := range fns {
		wg.Add(1)
		go func(index int, f func() (interface{}, error)) {
			defer wg.Done()
			result, err := p.SubmitAndWait(f)
			results[index] = TaskResult{Error: err, Data: result}
		}(i, fn)
	}

	wg.Wait()
	return results
}
