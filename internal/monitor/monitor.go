// Package monitor 提供资源监控功能
// 用于实时显示系统内存和进程 CPU/内存占用情况
package monitor

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// ResourceMonitor 资源监控器
// 定期采集并显示系统和进程的资源使用情况
type ResourceMonitor struct {
	// interval 采样间隔
	interval time.Duration

	// proc 当前进程
	proc *process.Process

	// stopCh 停止信号通道
	stopCh chan struct{}

	// wg 等待监控 goroutine 退出
	wg sync.WaitGroup

	// running 是否正在运行
	running bool
	mu      sync.Mutex
}

// Stats 资源统计快照
type Stats struct {
	// 系统内存 (字节)
	SystemMemTotal     uint64
	SystemMemUsed      uint64
	SystemMemAvailable uint64
	SystemMemPercent   float64

	// 进程资源
	ProcessMemRSS     uint64  // 常驻内存集 (物理内存)
	ProcessMemVMS     uint64  // 虚拟内存大小
	ProcessCPUPercent float64 // CPU 占用百分比

	// Go 运行时内存
	GoMemAlloc      uint64 // 已分配堆内存
	GoMemTotalAlloc uint64 // 累计分配的内存
	GoMemSys        uint64 // 从系统获取的内存
	GoNumGoroutine  int    // goroutine 数量
}

// New 创建资源监控器
// interval: 采样间隔（建议 1-5 秒）
func New(interval time.Duration) (*ResourceMonitor, error) {
	// 获取当前进程
	pid := int32(os.Getpid())
	proc, err := process.NewProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("获取进程信息失败: %w", err)
	}

	return &ResourceMonitor{
		interval: interval,
		proc:     proc,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start 启动后台监控
// 在后台 goroutine 中定期打印资源使用情况
func (m *ResourceMonitor) Start(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.run(ctx)
}

// Stop 停止监控
func (m *ResourceMonitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	close(m.stopCh)
	m.wg.Wait()
}

// run 监控循环
func (m *ResourceMonitor) run(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// 首次打印表头
	m.printHeader()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			stats, err := m.Collect()
			if err != nil {
				continue
			}
			m.printStats(stats)
		}
	}
}

// Collect 采集一次资源快照
func (m *ResourceMonitor) Collect() (*Stats, error) {
	stats := &Stats{}

	// 1. 系统内存
	vmem, err := mem.VirtualMemory()
	if err == nil {
		stats.SystemMemTotal = vmem.Total
		stats.SystemMemUsed = vmem.Used
		stats.SystemMemAvailable = vmem.Available
		stats.SystemMemPercent = vmem.UsedPercent
	}

	// 2. 进程 CPU 和内存
	cpuPercent, err := m.proc.CPUPercent()
	if err == nil {
		// gopsutil 返回多核累加值（N核最大N×100%），除以核心数归一化到 0-100%
		numCPU := float64(runtime.NumCPU())
		if numCPU > 0 {
			stats.ProcessCPUPercent = cpuPercent / numCPU
		} else {
			stats.ProcessCPUPercent = cpuPercent
		}
	}

	memInfo, err := m.proc.MemoryInfo()
	if err == nil {
		stats.ProcessMemRSS = memInfo.RSS
		stats.ProcessMemVMS = memInfo.VMS
	}

	// 3. Go 运行时统计
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	stats.GoMemAlloc = memStats.Alloc
	stats.GoMemTotalAlloc = memStats.TotalAlloc
	stats.GoMemSys = memStats.Sys
	stats.GoNumGoroutine = runtime.NumGoroutine()

	return stats, nil
}

// printHeader 打印表头
func (m *ResourceMonitor) printHeader() {
	fmt.Print("\n📊 资源监控: ")
}

// printStats 打印资源统计（单行覆盖）
func (m *ResourceMonitor) printStats(s *Stats) {
	timeStr := time.Now().Format("15:04:05")
	// 使用 \r 回到行首，覆盖显示
	fmt.Printf("\r📊 [%s] 系统: %.1fGB/%.0f%% | 进程: %.1fMB | CPU: %.1f%%    ",
		timeStr,
		float64(s.SystemMemUsed)/(1024*1024*1024),
		s.SystemMemPercent,
		float64(s.ProcessMemRSS)/(1024*1024),
		s.ProcessCPUPercent,
	)
}

// PrintFinal 打印最终统计
func (m *ResourceMonitor) PrintFinal(s *Stats) {
	// 换行，结束单行覆盖模式
	fmt.Println()
	fmt.Println()
	fmt.Println("📊 最终资源统计:")
	fmt.Printf("   系统内存: %.1f GB / %.1f GB (%.1f%% 已用)\n",
		float64(s.SystemMemUsed)/(1024*1024*1024),
		float64(s.SystemMemTotal)/(1024*1024*1024),
		s.SystemMemPercent,
	)
	fmt.Printf("   进程内存: %.1f MB (RSS), %.1f MB (VMS)\n",
		float64(s.ProcessMemRSS)/(1024*1024),
		float64(s.ProcessMemVMS)/(1024*1024),
	)
	fmt.Printf("   Go 堆内存: %.1f MB, Goroutines: %d\n",
		float64(s.GoMemAlloc)/(1024*1024),
		s.GoNumGoroutine,
	)
}

// FormatBytes 格式化字节数为人类可读格式
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
