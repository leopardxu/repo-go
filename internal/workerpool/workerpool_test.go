package workerpool

import (
	"fmt"
	"testing"
	"time"
)

func TestWorkerPool(t *testing.T) {
	pool := New(5)

	doneCount := 0
	tasks := 20
	resChan := make(chan TaskResult, tasks)

	for i := 0; i < tasks; i++ {
		idx := i
		res := pool.Submit(func() (interface{}, error) {
			time.Sleep(time.Millisecond * 10)
			return fmt.Sprintf("result %d", idx), nil
		})
		go func(r <-chan TaskResult) {
			resChan <- <-r
		}(res)
	}

	go func() {
		pool.Wait()
		close(resChan)
	}()

	for res := range resChan {
		if res.Error != nil {
			t.Errorf("Unexpected error: %v", res.Error)
		}
		doneCount++
	}

	if doneCount != tasks {
		t.Errorf("Expected %d tasks done, got %d", tasks, doneCount)
	}
}

func TestWorkerPoolStop(t *testing.T) {
	pool := New(2)

	// submitting tasks
	resChan := make(chan TaskResult, 10)
	for i := 0; i < 10; i++ {
		res := pool.Submit(func() (interface{}, error) {
			time.Sleep(time.Millisecond * 30) // taking long time
			return nil, nil
		})
		go func(r <-chan TaskResult) {
			resChan <- <-r
		}(res)
	}

	time.Sleep(time.Millisecond * 15) // let a few tasks start
	pool.Stop()
	pool.Wait() // ensure Wait behaves properly after Stop

	successCount := 0
	for {
		select {
		case <-resChan:
			successCount++
		default:
			goto CHECK
		}
	}
CHECK:
	if successCount == 10 {
		t.Errorf("Expected fewer than 10 completed tasks due to stop, got 10")
	}
}
