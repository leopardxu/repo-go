package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/leopardxu/repo-go/internal/logger"
)

// DisplayMode 进度显示模式
type DisplayMode int

const (
	// ModeNormal 标准模式 - 显示进度条
	ModeNormal DisplayMode = iota
	// ModeVerbose 详细模式 - 显示每个项目的详细信息
	ModeVerbose
	// ModeSilent 静默模式 - 不显示进度
	ModeSilent
)

// Reporter 进度报告接口
type Reporter interface {
	// Start 开始进度报告
	Start(total int)
	// Update 更新进度
	Update(current int, message string)
	// UpdateWithSpeed 更新进度并显示速度
	UpdateWithSpeed(current int, message string, itemsPerSecond float64)
	// Finish 完成进度报告
	Finish()
	// SetMode 设置显示模式
	SetMode(mode DisplayMode)
	// SetOutput 设置输出目标
	SetOutput(w io.Writer)
}

// ConsoleReporter 控制台进度报告
type ConsoleReporter struct {
	total       int
	current     int
	startTime   time.Time
	lastUpdate  time.Time
	mode        DisplayMode
	output      io.Writer
	lastMessage string
	mu          sync.Mutex
}

// NewConsoleReporter 创建控制台进度报告
func NewConsoleReporter() *ConsoleReporter {
	return &ConsoleReporter{
		mode:   ModeNormal,
		output: os.Stdout,
	}
}

// SetMode 设置显示模式
func (r *ConsoleReporter) SetMode(mode DisplayMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mode = mode
}

// SetOutput 设置输出目标
func (r *ConsoleReporter) SetOutput(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output = w
}

// Start 开始进度报告
func (r *ConsoleReporter) Start(total int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.total = total
	r.current = 0
	r.startTime = time.Now()
	r.lastUpdate = time.Now()

	if r.mode == ModeSilent {
		logger.Debug("开始同步%d 个项目", total)
		return
	}

	fmt.Fprintf(r.output, "开始同步%d 个项目\n", total)
}

// Update 更新进度
func (r *ConsoleReporter) Update(current int, message string) {
	r.UpdateWithSpeed(current, message, 0)
}

// UpdateWithSpeed 更新进度并显示速度
func (r *ConsoleReporter) UpdateWithSpeed(current int, message string, itemsPerSecond float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.current = current
	r.lastMessage = message

	// 如果是静默模式，只记录日�?
	if r.mode == ModeSilent {
		// �?0个项目或者间隔超�?秒记录一次日�?
		if current%10 == 0 || time.Since(r.lastUpdate) > 5*time.Second {
			logger.Debug("进度: %d/%d (%0.1f%%) %s",
				current, r.total, float64(current)/float64(r.total)*100, message)
			r.lastUpdate = time.Now()
		}
		return
	}

	// 详细模式，每个项目单独一�?
	if r.mode == ModeVerbose {
		fmt.Fprintf(r.output, "[%d/%d] %s\n", current, r.total, message)
		return
	}

	// 标准模式，显示进度条
	// 计算进度百分�?
	percent := float64(current) / float64(r.total) * 100

	// 计算预估剩余时间
	elapsed := time.Since(r.startTime)
	var eta time.Duration
	if current > 0 {
		eta = time.Duration(float64(elapsed) / float64(current) * float64(r.total-current))
	}

	// 构建进度�?
	progressWidth := 20
	completedWidth := int(float64(progressWidth) * float64(current) / float64(r.total))
	progressBar := "["
	for i := 0; i < progressWidth; i++ {
		if i < completedWidth {
			progressBar += "="
		} else if i == completedWidth {
			progressBar += ">"
		} else {
			progressBar += " "
		}
	}
	progressBar += "]"

	// 显示速度信息
	speedInfo := ""
	if itemsPerSecond > 0 {
		speedInfo = fmt.Sprintf(" %.1f项/秒", itemsPerSecond)
	}

	// 清除当前行并显示进度
	fmt.Fprintf(r.output, "\r%s %3.0f%% %d/%d %s (预计剩余: %s)%s     ",
		progressBar, percent, current, r.total, message, formatDuration(eta), speedInfo)
}

// Finish 完成进度报告
func (r *ConsoleReporter) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := time.Since(r.startTime)

	if r.mode == ModeSilent {
		logger.Info("完成同步 %d 个项目，耗时: %s", r.total, formatDuration(elapsed))
		return
	}

	// 计算平均速度
	itemsPerSecond := float64(r.total) / elapsed.Seconds()
	speedInfo := ""
	if r.total > 0 && elapsed.Seconds() > 0 {
		speedInfo = fmt.Sprintf("，平均%.1f 项/秒", itemsPerSecond)
	}

	fmt.Fprintf(r.output, "\r完成同步 %d 个项目，耗时: %s%s                      \n",
		r.total, formatDuration(elapsed), speedInfo)
}

// formatDuration 格式化时间
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "不到1秒"
	}

	seconds := int(d.Seconds())
	minutes := seconds / 60
	seconds = seconds % 60

	if minutes < 60 {
		return fmt.Sprintf("%d分%d秒", minutes, seconds)
	}

	hours := minutes / 60
	minutes = minutes % 60

	return fmt.Sprintf("%d时%d分%d秒", hours, minutes, seconds)
}

// New 创建默认的进度报告器
func New(total int) Reporter {
	reporter := NewConsoleReporter()
	reporter.Start(total)
	return reporter
}

// NewWithMode 创建指定模式的进度报告器
func NewWithMode(total int, mode DisplayMode) Reporter {
	reporter := NewConsoleReporter()
	reporter.SetMode(mode)
	reporter.Start(total)
	return reporter
}

// FormatBytes 格式化字节大�?
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// BuildProgressBar 构建自定义进度条
func BuildProgressBar(current, total int, width int) string {
	if width <= 0 {
		width = 20
	}

	completedWidth := 0
	if total > 0 {
		completedWidth = int(float64(width) * float64(current) / float64(total))
	}

	bar := strings.Builder{}
	bar.WriteString("[")

	for i := 0; i < width; i++ {
		if i < completedWidth {
			bar.WriteString("=")
		} else if i == completedWidth {
			bar.WriteString(">")
		} else {
			bar.WriteString(" ")
		}
	}

	bar.WriteString("]")
	return bar.String()
}
