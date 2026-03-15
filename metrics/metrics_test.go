package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistered(t *testing.T) {
	// Verify all 8 metrics are accessible and registered by calling
	// WithLabelValues on each. If a metric were nil or unregistered,
	// this would panic.
	metrics := []struct {
		name string
		call func()
	}{
		{"ExecutionDurationSeconds", func() { ExecutionDurationSeconds.WithLabelValues("reg-test.star") }},
		{"ReconciliationDurationSeconds", func() { ReconciliationDurationSeconds.WithLabelValues("reg-test.star") }},
		{"OCIResolveDurationSeconds", func() { OCIResolveDurationSeconds.WithLabelValues("reg-test.star") }},
		{"CacheHitsTotal", func() { CacheHitsTotal.WithLabelValues("reg-test.star") }},
		{"CacheMissesTotal", func() { CacheMissesTotal.WithLabelValues("reg-test.star") }},
		{"ResourcesEmittedTotal", func() { ResourcesEmittedTotal.WithLabelValues("reg-test.star") }},
		{"ResourcesSkippedTotal", func() { ResourcesSkippedTotal.WithLabelValues("reg-test.star") }},
		{"ReconciliationsTotal", func() { ReconciliationsTotal.WithLabelValues("reg-test.star") }},
	}
	for _, m := range metrics {
		t.Run(m.name, func(t *testing.T) {
			m.call() // panics if not registered
		})
	}
}

func TestCounterNaming(t *testing.T) {
	counters := []struct {
		name     string
		wantName string
		metric   *prometheus.CounterVec
	}{
		{"CacheHitsTotal", "function_starlark_cache_hits_total", CacheHitsTotal},
		{"CacheMissesTotal", "function_starlark_cache_misses_total", CacheMissesTotal},
		{"ResourcesEmittedTotal", "function_starlark_resources_emitted_total", ResourcesEmittedTotal},
		{"ResourcesSkippedTotal", "function_starlark_resources_skipped_total", ResourcesSkippedTotal},
		{"ReconciliationsTotal", "function_starlark_reconciliations_total", ReconciliationsTotal},
	}
	for _, tc := range counters {
		t.Run(tc.name, func(t *testing.T) {
			// Verify the metric name via Desc.
			ch := make(chan *prometheus.Desc, 1)
			tc.metric.Describe(ch)
			d := <-ch
			s := d.String()
			if !strings.Contains(s, tc.wantName) {
				t.Errorf("%s desc = %q, want it to contain %q", tc.name, s, tc.wantName)
			}

			// Verify the counter is functional (Inc does not panic).
			label := tc.name + "-naming.star"
			tc.metric.WithLabelValues(label).Inc()
			got := testutil.ToFloat64(tc.metric.WithLabelValues(label))
			if got != 1 {
				t.Errorf("%s after Inc() = %v, want 1", tc.name, got)
			}
		})
	}
}

func TestHistogramNaming(t *testing.T) {
	histograms := []struct {
		name     string
		wantName string
		metric   *prometheus.HistogramVec
	}{
		{"ExecutionDurationSeconds", "function_starlark_execution_duration_seconds", ExecutionDurationSeconds},
		{"ReconciliationDurationSeconds", "function_starlark_reconciliation_duration_seconds", ReconciliationDurationSeconds},
		{"OCIResolveDurationSeconds", "function_starlark_oci_resolve_duration_seconds", OCIResolveDurationSeconds},
	}
	for _, tc := range histograms {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan *prometheus.Desc, 1)
			tc.metric.Describe(ch)
			d := <-ch
			s := d.String()
			if !strings.Contains(s, tc.wantName) {
				t.Errorf("%s desc = %q, want it to contain %q", tc.name, s, tc.wantName)
			}
		})
	}
}

func TestScriptLabel(t *testing.T) {
	// Verify the script label is accepted on all metrics.
	// Using a unique label value to avoid cross-test interference.
	label := "label-test.star"

	// Counters.
	CacheHitsTotal.WithLabelValues(label).Inc()
	CacheMissesTotal.WithLabelValues(label).Inc()
	ResourcesEmittedTotal.WithLabelValues(label).Inc()
	ResourcesSkippedTotal.WithLabelValues(label).Inc()
	ReconciliationsTotal.WithLabelValues(label).Inc()

	// Histograms.
	ExecutionDurationSeconds.WithLabelValues(label).Observe(0.001)
	ReconciliationDurationSeconds.WithLabelValues(label).Observe(0.01)
	OCIResolveDurationSeconds.WithLabelValues(label).Observe(0.01)

	// If any of the above panicked, the test fails. Additionally verify
	// counter values to ensure they actually incremented.
	if got := testutil.ToFloat64(CacheHitsTotal.WithLabelValues(label)); got != 1 {
		t.Errorf("CacheHitsTotal{script=%q} = %v, want 1", label, got)
	}
}
