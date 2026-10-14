package telemetry

import (
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// MinuteBucket aggregates HTTP metrics for a single 1-minute interval.
type MinuteBucket struct {
	Timestamp     string  `json:"timestamp"`
	MinuteLabel   string  `json:"minute_label"`
	TotalRequests int64   `json:"total_requests"`
	Status2xx     int64   `json:"status_2xx"`
	Status3xx     int64   `json:"status_3xx"`
	Status4xx     int64   `json:"status_4xx"`
	Status5xx     int64   `json:"status_5xx"`
	BytesWritten  int64   `json:"bytes_written"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	MaxLatencyMs  float64 `json:"max_latency_ms"`

	totalLatencyMs float64
	latencies      []float64
}

// EndpointStat aggregates metrics per HTTP method + normalized route pattern.
type EndpointStat struct {
	Method         string  `json:"method"`
	Path           string  `json:"path"`
	Requests       int64   `json:"requests"`
	Errors         int64   `json:"errors"`
	BytesWritten   int64   `json:"bytes_written"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	MaxLatencyMs   float64 `json:"max_latency_ms"`
	totalLatencyMs float64
}

// TelemetrySnapshot represents a point-in-time view of API telemetry.
type TelemetrySnapshot struct {
	ServiceName     string         `json:"service_name"`
	ServiceVersion  string         `json:"service_version"`
	UptimeSeconds   int64          `json:"uptime_seconds"`
	TotalRequests   int64          `json:"total_requests"`
	TotalErrors     int64          `json:"total_errors"`
	TotalBytes      int64          `json:"total_bytes"`
	CurrentRPM      int64          `json:"current_rpm"`
	AvgLatencyMs    float64        `json:"avg_latency_ms"`
	P95LatencyMs    float64        `json:"p95_latency_ms"`
	AllocMB         float64        `json:"alloc_mb"`
	SysMB           float64        `json:"sys_mb"`
	NumGoroutines   int            `json:"num_goroutines"`
	NumGC           uint32         `json:"num_gc"`
	MinuteSeries    []MinuteBucket `json:"minute_series"`
	TopEndpoints    []EndpointStat `json:"top_endpoints"`
	RecentRequests  []LogEntry     `json:"recent_requests"`
	RecentErrors    []LogEntry     `json:"recent_errors"`
	CollectedAt     string         `json:"collected_at"`
}

// Collector records HTTP request telemetry in-memory with bounded buffers.
type Collector struct {
	mu             sync.RWMutex
	startTime      time.Time
	serviceName    string
	serviceVersion string
	totalRequests  int64
	totalErrors    int64
	totalBytes     int64
	totalLatencyMs float64
	buckets        map[int64]*MinuteBucket
	endpoints      map[string]*EndpointStat
	recentRequests []LogEntry
	recentErrors   []LogEntry
}

var defaultCollector = &Collector{
	startTime:      time.Now(),
	serviceName:    "file4base-api",
	serviceVersion: "0.12.1",
	buckets:        make(map[int64]*MinuteBucket),
	endpoints:      make(map[string]*EndpointStat),
	recentRequests: make([]LogEntry, 0, 120),
	recentErrors:   make([]LogEntry, 0, 60),
}

// DefaultCollector returns the process-wide telemetry collector.
func DefaultCollector() *Collector {
	return defaultCollector
}

// normalizePath collapses UUIDs and numeric IDs in URL paths so endpoint stats group cleanly.
func normalizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if len(p) >= 24 && strings.Count(p, "-") >= 3 {
			parts[i] = "{id}"
			continue
		}
		if len(p) > 0 {
			allDigits := true
			for _, ch := range p {
				if ch < '0' || ch > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				parts[i] = "{id}"
			}
		}
	}
	return strings.Join(parts, "/")
}

// Record logs a completed HTTP request into the telemetry collector.
func (c *Collector) Record(entry LogEntry) {
	now := time.Now().UTC()
	minuteEpoch := now.Truncate(time.Minute).Unix()

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry.ServiceName != "" {
		c.serviceName = entry.ServiceName
	}
	if entry.ServiceVersion != "" {
		c.serviceVersion = entry.ServiceVersion
	}

	c.totalRequests++
	c.totalBytes += entry.BytesWritten
	c.totalLatencyMs += entry.DurationMs
	if entry.HTTPStatusCode >= 400 {
		c.totalErrors++
	}

	b, ok := c.buckets[minuteEpoch]
	if !ok {
		tMin := time.Unix(minuteEpoch, 0).UTC()
		b = &MinuteBucket{
			Timestamp:   tMin.Format(time.RFC3339),
			MinuteLabel: tMin.Format("15:04"),
			latencies:   make([]float64, 0, 32),
		}
		c.buckets[minuteEpoch] = b
		// Prune buckets older than 65 minutes
		cutoff := minuteEpoch - 65*60
		for k := range c.buckets {
			if k < cutoff {
				delete(c.buckets, k)
			}
		}
	}

	b.TotalRequests++
	b.BytesWritten += entry.BytesWritten
	b.totalLatencyMs += entry.DurationMs
	b.AvgLatencyMs = b.totalLatencyMs / float64(b.TotalRequests)
	if entry.DurationMs > b.MaxLatencyMs {
		b.MaxLatencyMs = entry.DurationMs
	}
	if len(b.latencies) < 500 {
		b.latencies = append(b.latencies, entry.DurationMs)
	}
	switch {
	case entry.HTTPStatusCode >= 500:
		b.Status5xx++
	case entry.HTTPStatusCode >= 400:
		b.Status4xx++
	case entry.HTTPStatusCode >= 300:
		b.Status3xx++
	default:
		b.Status2xx++
	}

	normPath := normalizePath(entry.HTTPPath)
	epKey := entry.HTTPMethod + " " + normPath
	ep, ok := c.endpoints[epKey]
	if !ok {
		if len(c.endpoints) >= 200 {
			normPath = "other"
			epKey = entry.HTTPMethod + " " + normPath
		}
		ep = &EndpointStat{
			Method: entry.HTTPMethod,
			Path:   normPath,
		}
		c.endpoints[epKey] = ep
	}
	ep.Requests++
	ep.BytesWritten += entry.BytesWritten
	ep.totalLatencyMs += entry.DurationMs
	ep.AvgLatencyMs = ep.totalLatencyMs / float64(ep.Requests)
	if entry.DurationMs > ep.MaxLatencyMs {
		ep.MaxLatencyMs = entry.DurationMs
	}
	if entry.HTTPStatusCode >= 400 {
		ep.Errors++
	}

	// Keep most recent 100 requests (newest first)
	c.recentRequests = append([]LogEntry{entry}, c.recentRequests...)
	if len(c.recentRequests) > 100 {
		c.recentRequests = c.recentRequests[:100]
	}

	if entry.HTTPStatusCode >= 400 {
		c.recentErrors = append([]LogEntry{entry}, c.recentErrors...)
		if len(c.recentErrors) > 50 {
			c.recentErrors = c.recentErrors[:50]
		}
	}
}

func computeP95(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	idx := int(float64(len(cp)-1) * 0.95)
	return cp[idx]
}

// Snapshot returns a complete telemetry report for the last N minutes (default 30, max 60).
func (c *Collector) Snapshot(minutes int) TelemetrySnapshot {
	if minutes <= 0 || minutes > 60 {
		minutes = 30
	}
	now := time.Now().UTC()
	currentMinute := now.Truncate(time.Minute)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.mu.RLock()
	defer c.mu.RUnlock()

	series := make([]MinuteBucket, 0, minutes)
	var allLatencies []float64
	for i := minutes - 1; i >= 0; i-- {
		tMin := currentMinute.Add(-time.Duration(i) * time.Minute)
		epoch := tMin.Unix()
		if b, ok := c.buckets[epoch]; ok {
			copyBucket := *b
			copyBucket.P95LatencyMs = computeP95(b.latencies)
			series = append(series, copyBucket)
			allLatencies = append(allLatencies, b.latencies...)
		} else {
			series = append(series, MinuteBucket{
				Timestamp:   tMin.Format(time.RFC3339),
				MinuteLabel: tMin.Format("15:04"),
			})
		}
	}

	currentRPM := int64(0)
	if len(series) > 0 {
		currentRPM = series[len(series)-1].TotalRequests
		if len(series) > 1 && currentRPM < series[len(series)-2].TotalRequests {
			currentRPM = series[len(series)-2].TotalRequests
		}
	}

	avgLat := 0.0
	if c.totalRequests > 0 {
		avgLat = c.totalLatencyMs / float64(c.totalRequests)
	}

	eps := make([]EndpointStat, 0, len(c.endpoints))
	for _, ep := range c.endpoints {
		eps = append(eps, *ep)
	}
	sort.Slice(eps, func(i, j int) bool {
		return eps[i].Requests > eps[j].Requests
	})
	if len(eps) > 25 {
		eps = eps[:25]
	}

	recentReqs := make([]LogEntry, len(c.recentRequests))
	copy(recentReqs, c.recentRequests)

	recentErrs := make([]LogEntry, len(c.recentErrors))
	copy(recentErrs, c.recentErrors)

	return TelemetrySnapshot{
		ServiceName:    c.serviceName,
		ServiceVersion: c.serviceVersion,
		UptimeSeconds:  int64(time.Since(c.startTime).Seconds()),
		TotalRequests:  c.totalRequests,
		TotalErrors:    c.totalErrors,
		TotalBytes:     c.totalBytes,
		CurrentRPM:     currentRPM,
		AvgLatencyMs:   avgLat,
		P95LatencyMs:   computeP95(allLatencies),
		AllocMB:        float64(m.Alloc) / (1024 * 1024),
		SysMB:          float64(m.Sys) / (1024 * 1024),
		NumGoroutines:  runtime.NumGoroutine(),
		NumGC:          m.NumGC,
		MinuteSeries:   series,
		TopEndpoints:   eps,
		RecentRequests: recentReqs,
		RecentErrors:   recentErrs,
		CollectedAt:    now.Format(time.RFC3339),
	}
}
