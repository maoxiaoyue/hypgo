package benchmark

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Executor 測試執行器
type Executor struct {
	config      GeneralConfig
	workerPool  *WorkerPool
	rateLimiter *RateLimiter
	metrics     *MetricsCollector
}

// NewExecutor 創建執行器
func NewExecutor() *Executor {
	return &Executor{
		metrics: NewMetricsCollector(),
	}
}

// Init 初始化執行器
func (e *Executor) Init(config GeneralConfig) {
	e.config = config
	e.workerPool = NewWorkerPool(config.MaxConcurrency)
	e.rateLimiter = NewRateLimiter(0) // 0 表示無限制
}

// Execute 執行測試
func (e *Executor) Execute(ctx context.Context, runner Runner, operation Operation, options OperationOptions) *OperationResult {
	result := &OperationResult{
		OperationType: operation.Type,
		StartTime:     time.Now(),
	}

	// 開始收集指標
	e.metrics.Start()

	// 根據操作類型執行
	switch operation.Type {
	case OpTypeWrite:
		e.executeWrite(ctx, runner, operation, options, result)
	case OpTypeRead:
		e.executeRead(ctx, runner, operation, options, result)
	case OpTypeMixed:
		e.executeMixed(ctx, runner, operation, options, result)
	default:
		result.Errors = append(result.Errors, fmt.Errorf("unsupported operation type: %v", operation.Type))
	}

	// 停止收集指標
	e.metrics.Stop()

	// 填充結果
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.OpsPerSecond = float64(result.SuccessOperations) / result.Duration.Seconds()
	result.CPUUsage = e.metrics.GetCPUUsage()
	result.MemoryUsage = int64(e.metrics.GetMemoryUsage())
	result.GoroutineCount = runtime.NumGoroutine()

	// 計算延遲統計
	latencies := e.metrics.GetLatencies()
	if len(latencies) > 0 {
		result.AvgLatency = e.calculateAverage(latencies)
		result.MinLatency = e.findMin(latencies)
		result.MaxLatency = e.findMax(latencies)
		result.P50Latency = e.calculatePercentile(latencies, 50)
		result.P95Latency = e.calculatePercentile(latencies, 95)
		result.P99Latency = e.calculatePercentile(latencies, 99)
	}

	// 計算吞吐量
	totalBytes := atomic.LoadInt64(&e.metrics.bytesProcessed)
	result.Throughput = float64(totalBytes) / result.Duration.Seconds() / (1024 * 1024) // MB/s

	return result
}

// executeWrite 執行寫入測試
func (e *Executor) executeWrite(ctx context.Context, runner Runner, operation Operation, options OperationOptions, result *OperationResult) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, operation.Concurrency)

	successCount := int64(0)
	failureCount := int64(0)

	for i := 0; i < options.RecordCount; i++ {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			defer func() { <-sem }()

			startTime := time.Now()

			op := Operation{
				Type: OpTypeWrite,
				Data: e.generateTestData(index),
			}

			opResult, err := runner.RunOperation(ctx, op)

			latency := time.Since(startTime)
			e.metrics.RecordLatency(latency)

			if err != nil || opResult == nil {
				atomic.AddInt64(&failureCount, 1)
				result.Errors = append(result.Errors, err)
			} else {
				atomic.AddInt64(&successCount, 1)
				atomic.AddInt64(&e.metrics.bytesProcessed, 1024) // 假設每條記錄 1KB
			}
		}(i)
	}

	wg.Wait()

	result.TotalOperations = successCount + failureCount
	result.SuccessOperations = successCount
	result.FailedOperations = failureCount
}

// executeRead 執行讀取測試
func (e *Executor) executeRead(ctx context.Context, runner Runner, operation Operation, options OperationOptions, result *OperationResult) {
	// 類似 executeWrite，但執行讀取操作
	var wg sync.WaitGroup
	sem := make(chan struct{}, operation.Concurrency)

	successCount := int64(0)
	failureCount := int64(0)

	for i := 0; i < options.RecordCount; i++ {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			defer func() { <-sem }()

			startTime := time.Now()

			op := Operation{
				Type: OpTypeRead,
				Data: map[string]interface{}{"id": index},
			}

			opResult, err := runner.RunOperation(ctx, op)

			latency := time.Since(startTime)
			e.metrics.RecordLatency(latency)

			if err != nil || opResult == nil {
				atomic.AddInt64(&failureCount, 1)
			} else {
				atomic.AddInt64(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	result.TotalOperations = successCount + failureCount
	result.SuccessOperations = successCount
	result.FailedOperations = failureCount
}

// executeMixed 執行混合測試
func (e *Executor) executeMixed(ctx context.Context, runner Runner, operation Operation, options OperationOptions, result *OperationResult) {
	// 實現混合讀寫操作
	// 根據 MixedRatio 決定讀寫比例
	var wg sync.WaitGroup
	sem := make(chan struct{}, operation.Concurrency)

	successCount := int64(0)
	failureCount := int64(0)

	for i := 0; i < options.RecordCount; i++ {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			defer func() { <-sem }()

			// 根據比例決定是讀還是寫
			isRead := float64(index%100)/100.0 < options.MixedRatio

			var op Operation
			if isRead {
				op = Operation{
					Type: OpTypeRead,
					Data: map[string]interface{}{"id": index},
				}
			} else {
				op = Operation{
					Type: OpTypeWrite,
					Data: e.generateTestData(index),
				}
			}

			startTime := time.Now()
			opResult, err := runner.RunOperation(ctx, op)
			latency := time.Since(startTime)
			e.metrics.RecordLatency(latency)

			if err != nil || opResult == nil {
				atomic.AddInt64(&failureCount, 1)
			} else {
				atomic.AddInt64(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	result.TotalOperations = successCount + failureCount
	result.SuccessOperations = successCount
	result.FailedOperations = failureCount
}

// generateTestData 生成測試數據
func (e *Executor) generateTestData(index int) interface{} {
	return map[string]interface{}{
		"id":        index,
		"timestamp": time.Now().UnixNano(),
		"data":      fmt.Sprintf("test_data_%d", index),
		"value":     index * 100,
	}
}

// 計算輔助方法
func (e *Executor) calculateAverage(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	return total / time.Duration(len(latencies))
}

func (e *Executor) findMin(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	min := latencies[0]
	for _, l := range latencies[1:] {
		if l < min {
			min = l
		}
	}
	return min
}

func (e *Executor) findMax(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	max := latencies[0]
	for _, l := range latencies[1:] {
		if l > max {
			max = l
		}
	}
	return max
}

func (e *Executor) calculatePercentile(latencies []time.Duration, percentile int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	// 簡單實現，實際應該排序後計算
	return latencies[len(latencies)*percentile/100]
}

// WorkerPool 工作池
type WorkerPool struct {
	workers int
	tasks   chan func()
	wg      sync.WaitGroup
}

// NewWorkerPool 創建工作池
func NewWorkerPool(workers int) *WorkerPool {
	pool := &WorkerPool{
		workers: workers,
		tasks:   make(chan func(), workers*2),
	}

	// 啟動工作協程
	for i := 0; i < workers; i++ {
		go pool.worker()
	}

	return pool
}

func (pool *WorkerPool) worker() {
	for task := range pool.tasks {
		task()
	}
}

func (pool *WorkerPool) Submit(task func()) {
	pool.tasks <- task
}

func (pool *WorkerPool) Close() {
	close(pool.tasks)
}

// RateLimiter 速率限制器
type RateLimiter struct {
	rate   int
	ticker *time.Ticker
	tokens chan struct{}
}

// NewRateLimiter 創建速率限制器
func NewRateLimiter(ratePerSecond int) *RateLimiter {
	if ratePerSecond <= 0 {
		return &RateLimiter{rate: 0}
	}

	limiter := &RateLimiter{
		rate:   ratePerSecond,
		tokens: make(chan struct{}, ratePerSecond),
		ticker: time.NewTicker(time.Second / time.Duration(ratePerSecond)),
	}

	go limiter.refill()

	return limiter
}

func (r *RateLimiter) refill() {
	for range r.ticker.C {
		select {
		case r.tokens <- struct{}{}:
		default:
		}
	}
}

func (r *RateLimiter) Wait() {
	if r.rate == 0 {
		return
	}
	<-r.tokens
}

// MetricsCollector 指標收集器
type MetricsCollector struct {
	startTime      time.Time
	endTime        time.Time
	latencies      []time.Duration
	bytesProcessed int64
	cpuStart       float64
	cpuEnd         float64
	memStart       runtime.MemStats
	memEnd         runtime.MemStats
	mu             sync.Mutex
}

// NewMetricsCollector 創建指標收集器
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		latencies: make([]time.Duration, 0, 10000),
	}
}

func (m *MetricsCollector) Start() {
	m.startTime = time.Now()
	m.cpuStart = getCurrentCPU()
	runtime.ReadMemStats(&m.memStart)
}

func (m *MetricsCollector) Stop() {
	m.endTime = time.Now()
	m.cpuEnd = getCurrentCPU()
	runtime.ReadMemStats(&m.memEnd)
}

func (m *MetricsCollector) RecordLatency(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencies = append(m.latencies, latency)
}

func (m *MetricsCollector) GetLatencies() []time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Duration{}, m.latencies...)
}

func (m *MetricsCollector) GetCPUUsage() float64 {
	return m.cpuEnd - m.cpuStart
}

func (m *MetricsCollector) GetMemoryUsage() uint64 {
	return m.memEnd.Alloc - m.memStart.Alloc
}

func getCurrentCPU() float64 {
	// 簡化實現，實際應該使用系統調用獲取真實CPU使用率
	return float64(runtime.NumCPU())
}
