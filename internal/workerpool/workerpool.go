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
	wg          sync.WaitGroup
	once        sync.Once
	quit        chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	activeTasks sync.WaitGroup // 追踪活跃任务数
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

// start 启动工作
func (p *WorkerPool) start() {
	for i := 0; i < p.workers; i++ {
		go func() {
			for {
				select {
				case task, ok := <-p.tasks:
					if !ok {
						return
					}
					p.activeTasks.Add(1) // 增加活跃任务计数
					go func(t Task) {
						defer p.activeTasks.Done() // 完成时减少活跃任务计数
						result, err := t.Fn()
						t.Done <- TaskResult{Error: err, Data: result}
					}(task)
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

// Wait 等待所有任务完成
func (p *WorkerPool) Wait() {
	// 等待所有活跃任务完成
	p.activeTasks.Wait()

	// 然后关闭任务通道并等待工作者退出
	close(p.tasks)
}

// Stop 停止工作池
func (p *WorkerPool) Stop() {
	p.once.Do(func() {
		p.cancel()
		close(p.quit)
		close(p.tasks)
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
