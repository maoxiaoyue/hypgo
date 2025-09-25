package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// ResourceMetrics 資源指標結構
type ResourceMetrics struct {
	// 記憶體相關
	MemoryUsedBefore uint64 `json:"memory_used_before"` // 開始前記憶體使用量 (bytes)
	MemoryUsedAfter  uint64 `json:"memory_used_after"`  // 結束後記憶體使用量 (bytes)
	MemoryAllocated  uint64 `json:"memory_allocated"`   // 期間分配的記憶體 (bytes)
	MemoryFreed      uint64 `json:"memory_freed"`       // 期間釋放的記憶體 (bytes)

	// CPU 相關
	CPUTimeBefore int64   `json:"cpu_time_before"` // 開始前 CPU 時間 (nanoseconds)
	CPUTimeAfter  int64   `json:"cpu_time_after"`  // 結束後 CPU 時間 (nanoseconds)
	CPUUsage      float64 `json:"cpu_usage"`       // CPU 使用率 (%)

	// Goroutine 相關
	GoroutinesBefore  int `json:"goroutines_before"`  // 開始前 goroutine 數量
	GoroutinesAfter   int `json:"goroutines_after"`   // 結束後 goroutine 數量
	GoroutinesCreated int `json:"goroutines_created"` // 期間創建的 goroutine 數量

	// 時間相關
	StartTime time.Time     `json:"start_time"` // 開始時間
	EndTime   time.Time     `json:"end_time"`   // 結束時間
	Duration  time.Duration `json:"duration"`   // 執行時長

	// GC 相關
	GCCountBefore uint32        `json:"gc_count_before"` // 開始前 GC 次數
	GCCountAfter  uint32        `json:"gc_count_after"`  // 結束後 GC 次數
	GCPauseTotal  time.Duration `json:"gc_pause_total"`  // GC 暫停總時間
}

// Measurement 性能測量器
type Measurement struct {
	name            string
	startMetrics    *runtime.MemStats
	endMetrics      *runtime.MemStats
	startTime       time.Time
	endTime         time.Time
	startCPUTime    int64
	endCPUTime      int64
	startGoroutines int
	endGoroutines   int
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
	isRunning       bool
	results         *ResourceMetrics
}

// NewMeasurement 創建新的性能測量器
func NewMeasurement(name string) *Measurement {
	ctx, cancel := context.WithCancel(context.Background())
	return &Measurement{
		name:   name,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 開始測量
func (m *Measurement) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return
	}

	m.isRunning = true
	m.startTime = time.Now()

	// 記錄開始時的記憶體狀態
	m.startMetrics = &runtime.MemStats{}
	runtime.GC() // 強制進行一次 GC，確保測量準確性
	runtime.ReadMemStats(m.startMetrics)

	// 記錄開始時的 goroutine 數量
	m.startGoroutines = runtime.NumGoroutine()

	// 記錄開始時的 CPU 時間（使用 monotonic clock）
	m.startCPUTime = time.Now().UnixNano()
}

// Stop 停止測量並返回結果
func (m *Measurement) Stop() *ResourceMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return m.results
	}

	m.endTime = time.Now()
	m.endCPUTime = time.Now().UnixNano()

	// 記錄結束時的記憶體狀態
	m.endMetrics = &runtime.MemStats{}
	runtime.ReadMemStats(m.endMetrics)

	// 記錄結束時的 goroutine 數量
	m.endGoroutines = runtime.NumGoroutine()

	// 計算結果
	m.calculateResults()

	m.isRunning = false
	m.cancel()

	return m.results
}

// calculateResults 計算測量結果
func (m *Measurement) calculateResults() {
	duration := m.endTime.Sub(m.startTime)

	m.results = &ResourceMetrics{
		// 記憶體指標
		MemoryUsedBefore: m.startMetrics.Alloc,
		MemoryUsedAfter:  m.endMetrics.Alloc,
		MemoryAllocated:  m.endMetrics.TotalAlloc - m.startMetrics.TotalAlloc,
		MemoryFreed:      m.endMetrics.Frees - m.startMetrics.Frees,

		// CPU 指標
		CPUTimeBefore: m.startCPUTime,
		CPUTimeAfter:  m.endCPUTime,
		CPUUsage:      m.calculateCPUUsage(duration),

		// Goroutine 指標
		GoroutinesBefore:  m.startGoroutines,
		GoroutinesAfter:   m.endGoroutines,
		GoroutinesCreated: m.endGoroutines - m.startGoroutines,

		// 時間指標
		StartTime: m.startTime,
		EndTime:   m.endTime,
		Duration:  duration,

		// GC 指標
		GCCountBefore: m.startMetrics.NumGC,
		GCCountAfter:  m.endMetrics.NumGC,
		GCPauseTotal:  time.Duration(m.endMetrics.PauseTotalNs - m.startMetrics.PauseTotalNs),
	}
}

// calculateCPUUsage 計算 CPU 使用率（簡化版本）
func (m *Measurement) calculateCPUUsage(duration time.Duration) float64 {
	if duration == 0 {
		return 0
	}

	// 這是一個簡化的計算方式
	// 實際的 CPU 使用率需要更複雜的計算
	cpuTime := time.Duration(m.endCPUTime - m.startCPUTime)
	return float64(cpuTime) / float64(duration) * 100
}

// GetResults 取得測量結果
func (m *Measurement) GetResults() *ResourceMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.results
}

// IsRunning 檢查是否正在測量
func (m *Measurement) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isRunning
}

// Reset 重置測量器
func (m *Measurement) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		m.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel
	m.isRunning = false
	m.results = nil
	m.startMetrics = nil
	m.endMetrics = nil
}

// ToJSON 將結果轉換為 JSON 格式
func (m *Measurement) ToJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.results == nil {
		return nil, fmt.Errorf("no measurement results available")
	}

	return json.MarshalIndent(m.results, "", "  ")
}

// PrintResults 打印測量結果
func (m *Measurement) PrintResults() {
	if m.results == nil {
		fmt.Printf("Measurement '%s': No results available\n", m.name)
		return
	}

	fmt.Printf("\n=== Measurement Results for '%s' ===\n", m.name)
	fmt.Printf("執行時長: %v\n", m.results.Duration)
	fmt.Printf("開始時間: %s\n", m.results.StartTime.Format("2006-01-02 15:04:05.000"))
	fmt.Printf("結束時間: %s\n", m.results.EndTime.Format("2006-01-02 15:04:05.000"))

	fmt.Printf("\n--- 記憶體使用 ---\n")
	fmt.Printf("開始前記憶體: %s\n", formatBytes(m.results.MemoryUsedBefore))
	fmt.Printf("結束後記憶體: %s\n", formatBytes(m.results.MemoryUsedAfter))
	fmt.Printf("記憶體變化: %s\n", formatBytes(int64(m.results.MemoryUsedAfter)-int64(m.results.MemoryUsedBefore)))
	fmt.Printf("分配記憶體: %s\n", formatBytes(m.results.MemoryAllocated))

	fmt.Printf("\n--- Goroutine 使用 ---\n")
	fmt.Printf("開始前 Goroutines: %d\n", m.results.GoroutinesBefore)
	fmt.Printf("結束後 Goroutines: %d\n", m.results.GoroutinesAfter)
	fmt.Printf("Goroutines 變化: %d\n", m.results.GoroutinesCreated)

	fmt.Printf("\n--- GC 資訊 ---\n")
	fmt.Printf("GC 次數變化: %d\n", m.results.GCCountAfter-m.results.GCCountBefore)
	fmt.Printf("GC 暫停時間: %v\n", m.results.GCPauseTotal)

	fmt.Printf("=====================================\n\n")
}

// formatBytes 格式化字節數為人類可讀格式
func formatBytes(bytes interface{}) string {
	var b int64
	switch v := bytes.(type) {
	case uint64:
		b = int64(v)
	case int64:
		b = v
	case int:
		b = int64(v)
	default:
		return "0 B"
	}

	if b < 0 {
		return fmt.Sprintf("-%s", formatBytes(-b))
	}

	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f %s", float64(b)/float64(div), units[exp])
}

// MeasureFunc 測量函數執行的性能
func MeasureFunc(name string, fn func()) *ResourceMetrics {
	measurement := NewMeasurement(name)
	measurement.Start()
	fn()
	return measurement.Stop()
}

// MeasureFuncWithContext 帶 context 的函數性能測量
func MeasureFuncWithContext(ctx context.Context, name string, fn func(context.Context)) *ResourceMetrics {
	measurement := NewMeasurement(name)
	measurement.Start()
	fn(ctx)
	return measurement.Stop()
}

// MeasureFuncWithResult 測量有返回值的函數
func MeasureFuncWithResult[T any](name string, fn func() T) (T, *ResourceMetrics) {
	measurement := NewMeasurement(name)
	measurement.Start()
	result := fn()
	metrics := measurement.Stop()
	return result, metrics
}

// MeasureFuncWithError 測量可能返回錯誤的函數
func MeasureFuncWithError(name string, fn func() error) (*ResourceMetrics, error) {
	measurement := NewMeasurement(name)
	measurement.Start()
	err := fn()
	metrics := measurement.Stop()
	return metrics, err
}
