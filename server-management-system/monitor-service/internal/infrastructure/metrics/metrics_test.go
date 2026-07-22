package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewMetrics_RegistersAllSevenSignals(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewMetrics(reg)

	want := []string{
		"vcs_monitor_round_duration_seconds",
		"vcs_monitor_targets_expected",
		"vcs_monitor_checks_completed_total",
		"vcs_monitor_checks_missing",
		"vcs_monitor_queue_depth",
		"vcs_monitor_tcp_latency_seconds",
		"vcs_monitor_es_bulk_failure_total",
	}
	for _, name := range want {
		if n := testutil.CollectAndCount(reg, name); n == 0 {
			t.Errorf("%s is not registered", name)
		}
	}
}
