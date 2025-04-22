package progress

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Progress 表示进度指示器
type Progress struct {
	title     string
	total     int
	current   int
	lastPrint time.Time
	mu        sync.Mutex
	quiet     bool
}

// New 创建一个新的进度指示器
func New(title string, total int, showOutput bool) *Progress {
	p := &Progress{
		title:     title,
		total:     total,
		current:   0,
		lastPrint: time.Now(),
		quiet:     !showOutput,
	}

	if !p.quiet {
		fmt.Printf("%s: 0/%d 完成\n", p.title, p.total)
	}

	return p
}

// Update 更新进度指示器
func (p *Progress) Update(item string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.current++
	now := time.Now()

	// 每500毫秒或者完成时更新一次显示
	if !p.quiet && (now.Sub(p.lastPrint) > 500*time.Millisecond || p.current == p.total) {
		percent := float64(p.current) / float64(p.total) * 100
		bar := strings.Repeat("=", p.current*50/p.total) + ">"
		fmt.Printf("\r%s: %d/%d 完成 [%-50s] %.1f%% %s", 
			p.title, p.current, p.total, bar, percent, item)
		
		if p.current == p.total {
			fmt.Println()
		}
		
		p.lastPrint = now
	}
}

// End 结束进度指示器
func (p *Progress) End() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.quiet && p.current < p.total {
		fmt.Printf("\r%s: %d/%d 完成\n", p.title, p.current, p.total)
	}
}