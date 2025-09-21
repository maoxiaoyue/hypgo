package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

// Plugin Benchmark 插件主結構
type Plugin struct {
	config   *Config
	logger   *logger.Logger
	enabled  bool
	runners  map[string]Runner
	executor *Executor
	reporter *Reporter
	mu       sync.RWMutex
}

// Config 插件配置
type Config struct {
	Enabled   bool                `json:"enabled" yaml:"enabled"`
	General   GeneralConfig       `json:"general" yaml:"general"`
	Databases map[string]DBConfig `json:"databases" yaml:"databases"`
	Scenarios []ScenarioConfig    `json:"scenarios" yaml:"scenarios"`
	Reporting ReportingConfig     `json:"reporting" yaml:"reporting"`
}

// GeneralConfig 通用配置
type GeneralConfig struct {
	OutputDir         string        `json:"output_dir" yaml:"output_dir"`
	DefaultIterations int           `json:"default_iterations" yaml:"default_iterations"`
	WarmupRuns        int           `json:"warmup_runs" yaml:"warmup_runs"`
	Timeout           time.Duration `json:"timeout" yaml:"timeout"`
	CollectMemStats   bool          `json:"collect_mem_stats" yaml:"collect_mem_stats"`
	CollectIOStats    bool          `json:"collect_io_stats" yaml:"collect_io_stats"`
	MaxConcurrency    int           `json:"max_concurrency" yaml:"max_concurrency"`
	RetryOnError      bool          `json:"retry_on_error" yaml:"retry_on_error"`
	MaxRetries        int           `json:"max_retries" yaml:"max_retries"`
}

// DBConfig 資料庫配置
type DBConfig struct {
	Type    string                 `json:"type" yaml:"type"`
	Enabled bool                   `json:"enabled" yaml:"enabled"`
	Config  map[string]interface{} `json:"config" yaml:"config"`
}

// ScenarioConfig 場景配置
type ScenarioConfig struct {
	Name        string        `json:"name" yaml:"name"`
	Description string        `json:"description" yaml:"description"`
	Database    string        `json:"database" yaml:"database"`
	Operation   string        `json:"operation" yaml:"operation"`
	RecordCount int           `json:"record_count" yaml:"record_count"`
	BatchSize   int           `json:"batch_size" yaml:"batch_size"`
	Concurrency int           `json:"concurrency" yaml:"concurrency"`
	Duration    time.Duration `json:"duration" yaml:"duration"`
	MixedRatio  float64       `json:"mixed_ratio" yaml:"mixed_ratio"`
}

// ReportingConfig 報告配置
type ReportingConfig struct {
	Formats        []string `json:"formats" yaml:"formats"`
	AutoExport     bool     `json:"auto_export" yaml:"auto_export"`
	CompareResults bool     `json:"compare_results" yaml:"compare_results"`
	IncludeCharts  bool     `json:"include_charts" yaml:"include_charts"`
	EmailReport    bool     `json:"email_report" yaml:"email_report"`
	EmailTo        []string `json:"email_to" yaml:"email_to"`
}

// Runner 測試執行器介面
type Runner interface {
	Name() string
	Init(config map[string]interface{}) error
	Setup(ctx context.Context) error
	Teardown(ctx context.Context) error
	RunQuery(ctx context.Context, query Query) (*QueryResult, error)
	RunOperation(ctx context.Context, op Operation) (*OperationResult, error)
	HealthCheck(ctx context.Context) error
	GetMetrics() *Metrics
}

// Query 查詢定義
type Query struct {
	ID           string
	Statement    string
	Parameters   []interface{}
	Timeout      time.Duration
	ExpectedRows int
}

// Operation 操作定義
type Operation struct {
	Type        OperationType
	Data        interface{}
	BatchSize   int
	Concurrency int
}

// OperationType 操作類型
type OperationType int

const (
	OpTypeWrite OperationType = iota
	OpTypeRead
	OpTypeUpdate
	OpTypeDelete
	OpTypeMixed
)

// QueryResult 查詢結果
type QueryResult struct {
	QueryID      string        `json:"query_id"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`
	CPUTime      time.Duration `json:"cpu_time"`
	WaitTime     time.Duration `json:"wait_time"`
	RowsScanned  int64         `json:"rows_scanned"`
	BytesScanned int64         `json:"bytes_scanned"`
	RowsReturned int64         `json:"rows_returned"`
	MemoryUsed   int64         `json:"memory_used"`
	IOReadOps    int64         `json:"io_read_ops"`
	IOWriteOps   int64         `json:"io_write_ops"`
	IOReadBytes  int64         `json:"io_read_bytes"`
	IOWriteBytes int64         `json:"io_write_bytes"`
	CacheHits    int64         `json:"cache_hits"`
	CacheMisses  int64         `json:"cache_misses"`
	Error        error         `json:"error,omitempty"`
	Success      bool          `json:"success"`
}

// OperationResult 操作結果
type OperationResult struct {
	OperationType     OperationType `json:"operation_type"`
	StartTime         time.Time     `json:"start_time"`
	EndTime           time.Time     `json:"end_time"`
	Duration          time.Duration `json:"duration"`
	TotalOperations   int64         `json:"total_operations"`
	SuccessOperations int64         `json:"success_operations"`
	FailedOperations  int64         `json:"failed_operations"`
	OpsPerSecond      float64       `json:"ops_per_second"`
	AvgLatency        time.Duration `json:"avg_latency"`
	MinLatency        time.Duration `json:"min_latency"`
	MaxLatency        time.Duration `json:"max_latency"`
	P50Latency        time.Duration `json:"p50_latency"`
	P95Latency        time.Duration `json:"p95_latency"`
	P99Latency        time.Duration `json:"p99_latency"`
	Throughput        float64       `json:"throughput_mbps"`
	CPUUsage          float64       `json:"cpu_usage_percent"`
	MemoryUsage       int64         `json:"memory_usage_bytes"`
	GoroutineCount    int           `json:"goroutine_count"`
	Errors            []error       `json:"errors,omitempty"`
}

// Metrics 性能指標
type Metrics struct {
	TotalQueries      int64
	TotalOperations   int64
	TotalErrors       int64
	TotalDuration     time.Duration
	AverageDuration   time.Duration
	TotalBytesRead    int64
	TotalBytesWritten int64
	PeakMemory        uint64
	CurrentMemory     uint64
	GCCount           uint32
	LastUpdate        time.Time
}

// NewPlugin 創建新的 Benchmark 插件
func NewPlugin() *Plugin {
	return &Plugin{
		enabled:  true,
		runners:  make(map[string]Runner),
		executor: NewExecutor(),
		reporter: NewReporter(),
	}
}

// Name 返回插件名稱
func (p *Plugin) Name() string {
	return "benchmark"
}

// Init 初始化插件
func (p *Plugin) Init(config map[string]interface{}, log *logger.Logger) error {
	p.logger = log

	// 解析配置
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	p.config = &Config{}
	if err := json.Unmarshal(configBytes, p.config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 檢查是否啟用
	if !p.config.Enabled {
		p.enabled = false
		p.logger.Info("Benchmark plugin is disabled")
		return nil
	}

	// 設置默認值
	p.setDefaults()

	// 初始化執行器
	p.executor.Init(p.config.General)

	// 初始化報告器
	p.reporter.Init(p.config.Reporting)

	// 初始化資料庫運行器
	if err := p.initRunners(); err != nil {
		return fmt.Errorf("failed to init runners: %w", err)
	}

	p.logger.Info("Benchmark plugin initialized successfully")
	return nil
}

// setDefaults 設置默認配置
func (p *Plugin) setDefaults() {
	if p.config.General.OutputDir == "" {
		p.config.General.OutputDir = "benchmark_results"
	}
	if p.config.General.DefaultIterations == 0 {
		p.config.General.DefaultIterations = 1000
	}
	if p.config.General.WarmupRuns == 0 {
		p.config.General.WarmupRuns = 10
	}
	if p.config.General.Timeout == 0 {
		p.config.General.Timeout = 30 * time.Second
	}
	if p.config.General.MaxConcurrency == 0 {
		p.config.General.MaxConcurrency = runtime.NumCPU() * 2
	}
	if p.config.General.MaxRetries == 0 {
		p.config.General.MaxRetries = 3
	}
}

// initRunners 初始化測試運行器
func (p *Plugin) initRunners() error {
	for name, dbConfig := range p.config.Databases {
		if !dbConfig.Enabled {
			continue
		}

		runner, err := CreateRunner(dbConfig.Type, dbConfig.Config)
		if err != nil {
			p.logger.Error("Failed to create runner", "database", name, "error", err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), p.config.General.Timeout)
		defer cancel()

		if err := runner.Init(dbConfig.Config); err != nil {
			p.logger.Error("Failed to init runner", "database", name, "error", err)
			continue
		}

		if err := runner.Setup(ctx); err != nil {
			p.logger.Error("Failed to setup runner", "database", name, "error", err)
			continue
		}

		p.RegisterRunner(name, runner)
		p.logger.Info("Runner registered", "database", name, "type", dbConfig.Type)
	}

	return nil
}

// Start 啟動插件
func (p *Plugin) Start() error {
	if !p.enabled {
		return nil
	}

	// 啟動監控
	go p.startMonitoring()

	p.logger.Info("Benchmark plugin started")
	return nil
}

// Stop 停止插件
func (p *Plugin) Stop() error {
	if !p.enabled {
		return nil
	}

	// 停止所有運行器
	for name, runner := range p.runners {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := runner.Teardown(ctx); err != nil {
			p.logger.Error("Failed to teardown runner", "name", name, "error", err)
		}
		cancel()
	}

	// 生成最終報告
	if p.config.Reporting.AutoExport {
		if err := p.generateFinalReport(); err != nil {
			p.logger.Error("Failed to generate final report", "error", err)
		}
	}

	p.logger.Info("Benchmark plugin stopped")
	return nil
}

// Health 健康檢查
func (p *Plugin) Health() error {
	if !p.enabled {
		return nil
	}

	for name, runner := range p.runners {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := runner.HealthCheck(ctx)
		cancel()

		if err != nil {
			return fmt.Errorf("runner %s health check failed: %w", name, err)
		}
	}

	return nil
}

// RegisterRunner 註冊運行器
func (p *Plugin) RegisterRunner(name string, runner Runner) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runners[name] = runner
}

// GetRunner 獲取運行器
func (p *Plugin) GetRunner(name string) (Runner, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	runner, exists := p.runners[name]
	return runner, exists
}

// RunQueryBenchmark 執行查詢基準測試
func (p *Plugin) RunQueryBenchmark(ctx context.Context, database string, queries []Query, options QueryOptions) (*BenchmarkResult, error) {
	runner, exists := p.GetRunner(database)
	if !exists {
		return nil, fmt.Errorf("runner not found for database: %s", database)
	}

	result := &BenchmarkResult{
		Database:  database,
		Type:      "query",
		StartTime: time.Now(),
	}

	// 預熱
	for i := 0; i < options.WarmupRuns; i++ {
		for _, query := range queries {
			runner.RunQuery(ctx, query)
		}
	}

	// 執行測試
	var queryResults []*QueryResult
	for i := 0; i < options.Iterations; i++ {
		for _, query := range queries {
			qResult, err := runner.RunQuery(ctx, query)
			if err != nil {
				p.logger.Error("Query failed", "database", database, "query", query.ID, "error", err)
			}
			queryResults = append(queryResults, qResult)
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.QueryResults = queryResults

	// 計算統計
	result.Statistics = p.calculateStatistics(queryResults)

	// 保存結果
	if options.SaveResults {
		if err := p.reporter.SaveResult(result); err != nil {
			p.logger.Error("Failed to save result", "error", err)
		}
	}

	return result, nil
}

// RunOperationBenchmark 執行操作基準測試
func (p *Plugin) RunOperationBenchmark(ctx context.Context, database string, operation Operation, options OperationOptions) (*BenchmarkResult, error) {
	runner, exists := p.GetRunner(database)
	if !exists {
		return nil, fmt.Errorf("runner not found for database: %s", database)
	}

	result := &BenchmarkResult{
		Database:  database,
		Type:      "operation",
		StartTime: time.Now(),
	}

	// 使用執行器運行測試
	opResult := p.executor.Execute(ctx, runner, operation, options)

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.OperationResult = opResult

	// 保存結果
	if options.SaveResults {
		if err := p.reporter.SaveResult(result); err != nil {
			p.logger.Error("Failed to save result", "error", err)
		}
	}

	return result, nil
}

// RunScenario 執行預定義場景
func (p *Plugin) RunScenario(ctx context.Context, scenarioName string) (*BenchmarkResult, error) {
	var scenario *ScenarioConfig
	for _, s := range p.config.Scenarios {
		if s.Name == scenarioName {
			scenario = &s
			break
		}
	}

	if scenario == nil {
		return nil, fmt.Errorf("scenario not found: %s", scenarioName)
	}

	operation := Operation{
		Type:        p.parseOperationType(scenario.Operation),
		BatchSize:   scenario.BatchSize,
		Concurrency: scenario.Concurrency,
	}

	options := OperationOptions{
		RecordCount: scenario.RecordCount,
		Duration:    scenario.Duration,
		MixedRatio:  scenario.MixedRatio,
		SaveResults: true,
	}

	return p.RunOperationBenchmark(ctx, scenario.Database, operation, options)
}

// CompareResults 比較測試結果
func (p *Plugin) CompareResults(results ...*BenchmarkResult) *ComparisonReport {
	return p.reporter.Compare(results...)
}

// ExportResults 導出測試結果
func (p *Plugin) ExportResults(format string, outputPath string) error {
	return p.reporter.Export(format, outputPath, p.getAllResults())
}

// GetMetrics 獲取當前指標
func (p *Plugin) GetMetrics() map[string]*Metrics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	metrics := make(map[string]*Metrics)
	for name, runner := range p.runners {
		metrics[name] = runner.GetMetrics()
	}

	return metrics
}

// parseOperationType 解析操作類型
func (p *Plugin) parseOperationType(op string) OperationType {
	switch op {
	case "write":
		return OpTypeWrite
	case "read":
		return OpTypeRead
	case "update":
		return OpTypeUpdate
	case "delete":
		return OpTypeDelete
	case "mixed":
		return OpTypeMixed
	default:
		return OpTypeWrite
	}
}

// calculateStatistics 計算統計信息
func (p *Plugin) calculateStatistics(results []*QueryResult) *Statistics {
	stats := &Statistics{}

	if len(results) == 0 {
		return stats
	}

	var totalDuration time.Duration
	var successCount int64
	var totalBytes int64

	for _, r := range results {
		totalDuration += r.Duration
		if r.Success {
			successCount++
		}
		totalBytes += r.BytesScanned
	}

	stats.TotalQueries = int64(len(results))
	stats.SuccessQueries = successCount
	stats.FailedQueries = stats.TotalQueries - successCount
	stats.SuccessRate = float64(successCount) / float64(stats.TotalQueries) * 100
	stats.AverageDuration = totalDuration / time.Duration(len(results))
	stats.TotalBytesScanned = totalBytes

	// 計算百分位數
	stats.Percentiles = p.calculatePercentiles(results)

	return stats
}

// calculatePercentiles 計算百分位數
func (p *Plugin) calculatePercentiles(results []*QueryResult) map[string]time.Duration {
	// 實現百分位數計算邏輯
	return map[string]time.Duration{
		"p50": 0,
		"p95": 0,
		"p99": 0,
	}
}

// startMonitoring 啟動監控
func (p *Plugin) startMonitoring() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		metrics := p.GetMetrics()
		p.logger.Debug("Metrics update", "metrics", metrics)

		// 可以將指標發送到監控系統
	}
}

// generateFinalReport 生成最終報告
func (p *Plugin) generateFinalReport() error {
	results := p.getAllResults()

	for _, format := range p.config.Reporting.Formats {
		outputPath := fmt.Sprintf("%s/final_report.%s", p.config.General.OutputDir, format)
		if err := p.reporter.Export(format, outputPath, results); err != nil {
			return err
		}
	}

	return nil
}

// getAllResults 獲取所有測試結果
func (p *Plugin) getAllResults() []*BenchmarkResult {
	// 實現獲取所有結果的邏輯
	return []*BenchmarkResult{}
}

// 輔助類型定義

// BenchmarkResult 基準測試結果
type BenchmarkResult struct {
	Database        string           `json:"database"`
	Type            string           `json:"type"`
	StartTime       time.Time        `json:"start_time"`
	EndTime         time.Time        `json:"end_time"`
	Duration        time.Duration    `json:"duration"`
	QueryResults    []*QueryResult   `json:"query_results,omitempty"`
	OperationResult *OperationResult `json:"operation_result,omitempty"`
	Statistics      *Statistics      `json:"statistics,omitempty"`
}

// Statistics 統計信息
type Statistics struct {
	TotalQueries      int64                    `json:"total_queries"`
	SuccessQueries    int64                    `json:"success_queries"`
	FailedQueries     int64                    `json:"failed_queries"`
	SuccessRate       float64                  `json:"success_rate"`
	AverageDuration   time.Duration            `json:"average_duration"`
	TotalBytesScanned int64                    `json:"total_bytes_scanned"`
	Percentiles       map[string]time.Duration `json:"percentiles"`
}

// QueryOptions 查詢選項
type QueryOptions struct {
	Iterations   int           `json:"iterations"`
	WarmupRuns   int           `json:"warmup_runs"`
	Timeout      time.Duration `json:"timeout"`
	SaveResults  bool          `json:"save_results"`
	CollectStats bool          `json:"collect_stats"`
}

// OperationOptions 操作選項
type OperationOptions struct {
	RecordCount int           `json:"record_count"`
	Duration    time.Duration `json:"duration"`
	MixedRatio  float64       `json:"mixed_ratio"`
	SaveResults bool          `json:"save_results"`
}

// ComparisonReport 比較報告
type ComparisonReport struct {
	Databases   []string               `json:"databases"`
	Comparisons map[string]*Comparison `json:"comparisons"`
	Winner      string                 `json:"winner"`
	Summary     string                 `json:"summary"`
}

// Comparison 比較結果
type Comparison struct {
	Metric    string             `json:"metric"`
	Values    map[string]float64 `json:"values"`
	BestValue float64            `json:"best_value"`
	BestDB    string             `json:"best_db"`
}
