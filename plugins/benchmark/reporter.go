package benchmark

import (
	"fmt"
	"sync"
	"time"
)

type Reporter struct {
	config  ReportingConfig
	results []BenchmarkResult
	mu      sync.RWMutex
}

// NewReporter 創建報告生成器
func NewReporter() *Reporter {
	return &Reporter{
		results: make([]BenchmarkResult, 0),
	}
}

// Init 初始化報告器
func (r *Reporter) Init(config ReportingConfig) {
	r.config = config
}

// SaveResult 保存測試結果
func (r *Reporter) SaveResult(result *BenchmarkResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.results = append(r.results, *result)

	// 如果配置了自動導出，立即導出
	if r.config.AutoExport {
		for _, format := range r.config.Formats {
			if err := r.exportSingle(format, result); err != nil {
				return err
			}
		}
	}

	return nil
}

// Export 導出報告
func (r *Reporter) Export(format string, outputPath string, results []*BenchmarkResult) error {
	switch format {
	case "json":
		return r.exportJSON(outputPath, results)
	case "html":
		return r.exportHTML(outputPath, results)
	case "markdown":
		return r.exportMarkdown(outputPath, results)
	case "csv":
		return r.exportCSV(outputPath, results)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// exportSingle 導出單個結果
func (r *Reporter) exportSingle(format string, result *BenchmarkResult) error {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("benchmark_%s_%s.%s", result.Database, timestamp, format)
	return r.Export(format, filename, []*BenchmarkResult{result})
}

// exportJSON 導出為JSON
func (r *Reporter) exportJSON(outputPath string, results []*BenchmarkResult) error {
	// 實現JSON導出
	return nil
}

// exportHTML 導出為HTML
func (r *Reporter) exportHTML(outputPath string, results []*BenchmarkResult) error {
	// 實現HTML導出
	return nil
}

// exportMarkdown 導出為Markdown
func (r *Reporter) exportMarkdown(outputPath string, results []*BenchmarkResult) error {
	// 實現Markdown導出
	return nil
}

// exportCSV 導出為CSV
func (r *Reporter) exportCSV(outputPath string, results []*BenchmarkResult) error {
	// 實現CSV導出
	return nil
}

// Compare 比較多個結果
func (r *Reporter) Compare(results ...*BenchmarkResult) *ComparisonReport {
	report := &ComparisonReport{
		Databases:   make([]string, 0),
		Comparisons: make(map[string]*Comparison),
	}

	// 收集所有資料庫名稱
	for _, result := range results {
		report.Databases = append(report.Databases, result.Database)
	}

	// 比較各項指標
	metrics := []string{"avg_latency", "p95_latency", "throughput", "success_rate"}
	for _, metric := range metrics {
		comparison := &Comparison{
			Metric: metric,
			Values: make(map[string]float64),
		}

		for _, result := range results {
			value := r.getMetricValue(result, metric)
			comparison.Values[result.Database] = value

			if value > comparison.BestValue {
				comparison.BestValue = value
				comparison.BestDB = result.Database
			}
		}

		report.Comparisons[metric] = comparison
	}

	// 決定獲勝者
	report.Winner = r.determineWinner(report)
	report.Summary = r.generateSummary(report)

	return report
}

// getMetricValue 獲取指標值
func (r *Reporter) getMetricValue(result *BenchmarkResult, metric string) float64 {
	if result.OperationResult == nil {
		return 0
	}

	switch metric {
	case "avg_latency":
		return float64(result.OperationResult.AvgLatency)
	case "p95_latency":
		return float64(result.OperationResult.P95Latency)
	case "throughput":
		return result.OperationResult.Throughput
	case "success_rate":
		if result.OperationResult.TotalOperations == 0 {
			return 0
		}
		return float64(result.OperationResult.SuccessOperations) / float64(result.OperationResult.TotalOperations) * 100
	default:
		return 0
	}
}

// determineWinner 決定獲勝者
func (r *Reporter) determineWinner(report *ComparisonReport) string {
	scores := make(map[string]int)

	for _, comparison := range report.Comparisons {
		scores[comparison.BestDB]++
	}

	var winner string
	var maxScore int
	for db, score := range scores {
		if score > maxScore {
			winner = db
			maxScore = score
		}
	}

	return winner
}

// generateSummary 生成摘要
func (r *Reporter) generateSummary(report *ComparisonReport) string {
	return fmt.Sprintf("%s performed best in %d out of %d metrics",
		report.Winner,
		len(report.Comparisons),
		len(report.Comparisons))
}
