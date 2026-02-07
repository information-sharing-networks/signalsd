package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBatchSize  = 5
	defaultNumBatches = 10
	defaultBaseURL    = "http://localhost"
)

var (
	// Random prefix for this test instance to avoid conflicts in parallel runs
	testInstanceID string
)

type PerformanceMetrics struct {
	TotalRequests     int
	SuccessfulBatches int
	FailedBatches     int
	TotalSignals      int
	TotalDuration     time.Duration
	AverageLatency    time.Duration
	MinLatency        time.Duration
	MaxLatency        time.Duration
	RequestsPerSecond float64
	SignalsPerSecond  float64
	TotalLatency      time.Duration // Sum of all individual batch latencies
}

func main() {
	// Initialize random test instance ID for parallel testing
	testInstanceID = generateRandomLowercase(6)

	// Get base URL from environment or use default
	signalsPort := os.Getenv("SIGNALS_PORT")

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	baseURL = fmt.Sprintf("%s:%s", baseURL, signalsPort)

	// Check for required environment variables
	authToken := os.Getenv("AUTH_TOKEN")
	isnSlug := os.Getenv("ISN_SLUG")
	signalType := os.Getenv("SIGNAL_TYPE")
	semVer := os.Getenv("SEM_VER")

	if authToken == "" || isnSlug == "" || signalType == "" || semVer == "" {
		log.Fatal("Required environment variables: AUTH_TOKEN, ISN_SLUG, SIGNAL_TYPE, SEM_VER")
	}

	// Get batch configuration from environment or use defaults
	batchSize := defaultBatchSize
	numBatches := defaultNumBatches

	if envBatchSize := os.Getenv("BATCH_SIZE"); envBatchSize != "" {
		if size, err := strconv.Atoi(envBatchSize); err == nil && size > 0 {
			batchSize = size
		}
	}

	if envNumBatches := os.Getenv("NUM_BATCHES"); envNumBatches != "" {
		if batches, err := strconv.Atoi(envNumBatches); err == nil && batches > 0 {
			numBatches = batches
		}
	}

	fmt.Printf("Starting performance test [%s] with %d batches of %d signals each\n", testInstanceID, numBatches, batchSize)
	fmt.Printf("Target: %s/api/isn/%s/signal_types/%s/v%s/signals\n", baseURL, isnSlug, signalType, semVer)

	metrics := runPerformanceTest(baseURL, authToken, isnSlug, signalType, semVer, batchSize, numBatches)
	reportMetrics(metrics)
}

func runPerformanceTest(baseURL, authToken, isnSlug, signalType, semVer string, batchSize, numBatches int) PerformanceMetrics {
	metrics := PerformanceMetrics{
		MinLatency: time.Hour,
	}

	startTime := time.Now()

	for i := 0; i < numBatches; i++ {
		batchStartTime := time.Now()

		signals := generateSignalBatch(i, batchSize)
		success := sendSignalBatch(baseURL, authToken, isnSlug, signalType, semVer, signals, i)

		batchDuration := time.Since(batchStartTime)

		metrics.TotalRequests++
		if success {
			metrics.SuccessfulBatches++
			metrics.TotalSignals += len(signals)
		} else {
			metrics.FailedBatches++
		}

		// Update latency metrics
		if batchDuration < metrics.MinLatency {
			metrics.MinLatency = batchDuration
		}
		if batchDuration > metrics.MaxLatency {
			metrics.MaxLatency = batchDuration
		}

		// Accumulate total latency for proper average calculation
		metrics.TotalLatency += batchDuration

		// Progress reporting every 50 batches for large tests
		if (i+1)%50 == 0 || i < 10 || !success {
			fmt.Printf("[%s] Batch %d/%d: %d signals, %v, success: %v\n",
				testInstanceID, i+1, numBatches, len(signals), batchDuration, success)
		}
	}

	metrics.TotalDuration = time.Since(startTime)
	if metrics.SuccessfulBatches > 0 {
		// Calculate proper average latency from sum of individual batch latencies
		metrics.AverageLatency = metrics.TotalLatency / time.Duration(metrics.SuccessfulBatches)
	}
	metrics.RequestsPerSecond = float64(metrics.TotalRequests) / metrics.TotalDuration.Seconds()
	metrics.SignalsPerSecond = float64(metrics.TotalSignals) / metrics.TotalDuration.Seconds()

	return metrics
}

func generateSignalBatch(batchIndex int, batchSize int) []map[string]interface{} {
	signals := make([]map[string]interface{}, batchSize)

	for i := 0; i < batchSize; i++ {
		signals[i] = generateComplexSignal(batchIndex*batchSize+i, batchSize)
	}

	return signals
}

func generateComplexSignal(index int, batchSize int) map[string]interface{} {
	now := time.Now()

	// Generate test data
	signal := map[string]interface{}{
		"signal_id":     fmt.Sprintf("SIG-%08d-%s", index, generateRandomString(6)),
		"timestamp":     now.Format(time.RFC3339),
		"event_type":    randomChoice([]string{"login", "logout", "data_access", "file_upload", "api_call"}),
		"severity":      rand.Intn(10) + 1,
		"source_system": fmt.Sprintf("system-%d", index%10),
		"location": map[string]interface{}{
			"country":   randomChoice([]string{"US", "GB", "DE", "FR", "JP"}),
			"city":      randomChoice([]string{"New York", "London", "Berlin", "Paris", "Tokyo"}),
			"latitude":  (rand.Float64() - 0.5) * 180,
			"longitude": (rand.Float64() - 0.5) * 360,
		},
		"user_id":    fmt.Sprintf("user_%s", generateRandomLowercase(8)),
		"session_id": fmt.Sprintf("sess_%s", generateRandomHex(32)),
		"ip_address": generateValidIPv4(),
		"user_agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",

		"request_method":    randomChoice([]string{"GET", "POST", "PUT", "DELETE"}),
		"request_url":       fmt.Sprintf("https://api.example.com/v1/resource/%d", rand.Intn(1000)),
		"response_code":     randomChoice([]int{200, 201, 400, 401, 404, 500}),
		"response_time_ms":  rand.Float64() * 1000,
		"bytes_transferred": rand.Intn(1000000),
		"error_message":     nil,
		"tags":              []string{"performance", "test", fmt.Sprintf("batch_%d", index/batchSize)},
		"metadata": map[string]interface{}{
			"test_run":     true,
			"batch_id":     index / batchSize,
			"signal_index": index,
		},
		"risk_score":    rand.Float64() * 100,
		"is_suspicious": rand.Float64() < 0.1,
		"correlation_ids": []string{
			generateUUID(),
		},
		"device_info": map[string]interface{}{
			"device_type":       randomChoice([]string{"desktop", "mobile", "tablet"}),
			"os":                randomChoice([]string{"Windows 10", "macOS", "Linux", "iOS", "Android"}),
			"browser":           randomChoice([]string{"Chrome", "Firefox", "Safari", "Edge"}),
			"screen_resolution": randomChoice([]string{"1920x1080", "1366x768", "1440x900"}),
		},
		"network_info": map[string]interface{}{
			"isp":             randomChoice([]string{"Comcast", "Verizon", "AT&T", "BT", "Deutsche Telekom"}),
			"asn":             rand.Intn(65535) + 1,
			"connection_type": randomChoice([]string{"broadband", "mobile", "satellite"}),
		},
		"performance_metrics": map[string]interface{}{
			"cpu_usage":    rand.Float64() * 100,
			"memory_usage": rand.Float64() * 8192,
			"disk_usage":   rand.Float64() * 100,
		},
		"business_context": map[string]interface{}{
			"department":  randomChoice([]string{"Engineering", "Sales", "Marketing", "Support"}),
			"project_id":  fmt.Sprintf("PROJ-%06d", rand.Intn(1000000)),
			"cost_center": fmt.Sprintf("CC-%04d", rand.Intn(10000)),
		},
		"compliance_flags": []string{
			randomChoice([]string{"gdpr", "hipaa", "sox", "pci_dss"}),
		},
		"data_classification":   randomChoice([]string{"public", "internal", "confidential"}),
		"retention_period_days": randomChoice([]int{30, 90, 365, 2555}),
		"created_by":            "perf.test@example.com",
		"version":               "1.0.0",
	}

	return signal
}

func sendSignalBatch(baseURL, authToken, isnSlug, signalType, semVer string, signals []map[string]interface{}, batchIndex int) bool {
	// Create the request payload
	payload := map[string]interface{}{
		"signals": make([]map[string]interface{}, len(signals)),
	}

	for i, signal := range signals {
		payload["signals"].([]map[string]interface{})[i] = map[string]interface{}{
			"local_ref": fmt.Sprintf("%s-batch-%d-signal-%d", testInstanceID, batchIndex, i),
			"content":   signal,
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("Failed to marshal signal batch: %v\n", err)
		return false
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/api/isn/%s/signal_types/%s/v%s/signals",
		baseURL, isnSlug, signalType, semVer)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Failed to create request: %v\n", err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)

	client := &http.Client{
		Timeout: 3 * time.Minute,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Failed to send request: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		// Read response body for error details
		body := make([]byte, 1024)
		n, _ := resp.Body.Read(body)
		fmt.Printf("Unexpected status code: %d, Response: %s\n", resp.StatusCode, string(body[:n]))
		return false
	}

	return true
}

func reportMetrics(metrics PerformanceMetrics) {
	separator := strings.Repeat("=", 60)
	fmt.Println("\n" + separator)
	fmt.Printf("PERFORMANCE TEST RESULTS [%s]\n", testInstanceID)
	fmt.Println(separator)
	fmt.Printf("Total Requests:      %d\n", metrics.TotalRequests)
	fmt.Printf("Successful Batches:  %d\n", metrics.SuccessfulBatches)
	fmt.Printf("Failed Batches:      %d\n", metrics.FailedBatches)
	fmt.Printf("Total Signals:       %d\n", metrics.TotalSignals)
	fmt.Println(separator)
	fmt.Printf("TIMING METRICS:\n")
	fmt.Printf("Total Test Duration: %v\n", metrics.TotalDuration)
	fmt.Printf("Total Request Time:  %v\n", metrics.TotalLatency)
	fmt.Printf("Average Latency:     %v\n", metrics.AverageLatency)
	fmt.Printf("Min Latency:         %v\n", metrics.MinLatency)
	fmt.Printf("Max Latency:         %v\n", metrics.MaxLatency)
	fmt.Printf("Latency Range:       %v (%.1fx slower)\n",
		metrics.MaxLatency-metrics.MinLatency,
		float64(metrics.MaxLatency)/float64(metrics.MinLatency))
	fmt.Println(separator)
	fmt.Printf("THROUGHPUT METRICS:\n")
	fmt.Printf("Requests/Second:     %.2f\n", metrics.RequestsPerSecond)
	fmt.Printf("Signals/Second:      %.2f\n", metrics.SignalsPerSecond)
	fmt.Printf("Overhead Ratio:      %.1f%% (non-request time)\n",
		100.0*(float64(metrics.TotalDuration-metrics.TotalLatency)/float64(metrics.TotalDuration)))
	fmt.Println(separator)

	// Performance summary
	if metrics.FailedBatches == 0 {
		fmt.Println("All batches processed successfully")
	} else {
		fmt.Printf("%d batches failed\n", metrics.FailedBatches)
	}

}

// Helper functions
func generateRandomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func generateRandomLowercase(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func generateRandomHex(length int) string {
	const charset = "abcdef0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func generateValidIPv4() string {
	// Generate valid IPv4 addresses that will pass format validation
	validIPs := []string{
		"192.168.1.100",
		"10.0.0.50",
		"172.16.0.25",
		"203.0.113.45",
		"198.51.100.30",
		"192.168.0.1",
		"10.1.1.1",
		"172.31.255.254",
		"8.8.8.8",
		"1.1.1.1",
	}
	return validIPs[rand.Intn(len(validIPs))]
}

func generateUUID() string {
	// Generate a simple UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	return fmt.Sprintf("%08x-%04x-4%03x-%04x-%012x",
		rand.Uint32(),
		rand.Uint32()&0xffff,
		rand.Uint32()&0xfff,
		(rand.Uint32()&0x3fff)|0x8000,
		rand.Uint64()&0xffffffffffff)
}

func randomChoice[T any](choices []T) T {
	return choices[rand.Intn(len(choices))]
}
