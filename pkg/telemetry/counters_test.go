package telemetry_test

import (
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"testing"
)

func TestCounterOrderStationScopeAndFallback(t *testing.T) {
	reg := prometheus.NewRegistry()
	rx := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "graywolf_rx_frames_total", Help: "test"}, []string{"channel"})
	ingress := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "graywolf_kiss_ingress_frames_total", Help: "test"}, []string{"interface_id", "mode"})
	digi := prometheus.NewCounter(prometheus.CounterOpts{Name: "graywolf_digipeater_packets_total", Help: "test"})
	tx := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "graywolf_tx_frames_total", Help: "test"}, []string{"channel"})
	reg.MustRegister(rx, ingress, digi, tx)
	rx.WithLabelValues("1").Inc()
	rx.WithLabelValues("2").Inc()
	ingress.WithLabelValues("3", "tnc").Inc()
	ingress.WithLabelValues("4", "modem").Inc()
	digi.Add(5)
	tx.WithLabelValues("1").Inc()
	tx.WithLabelValues("2").Inc()
	ig := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "igate_packets_gated_total", Help: "test"}, []string{"direction"})
	reg.MustRegister(ig)
	ig.WithLabelValues("rf_to_is").Add(6)
	ig.WithLabelValues("is_to_rf").Add(7)
	counts, source, err := telemetry.CollectCounters(reg)
	if err != nil || source != "legacy" || counts != (telemetry.Counts{3, 5, 2, 6, 7}) {
		t.Fatalf("%v %s %v", counts, source, err)
	}
	newer := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "graywolf_tx_source_outcomes_total", Help: "test"}, []string{"channel", "source", "outcome"})
	reg.MustRegister(newer)
	counts, source, err = telemetry.CollectCounters(reg)
	if err != nil || source != "forwarded" || counts[1] != 0 {
		t.Fatalf("empty capability must be zero: %v %s %v", counts, source, err)
	}
	newer.WithLabelValues("1", "digipeater", "forwarded").Add(3)
	newer.WithLabelValues("2", "digipeater", "forwarded").Add(4)
	newer.WithLabelValues("1", "digipeater", "deduped").Add(100)
	newer.WithLabelValues("1", "beacon", "forwarded").Add(200)
	counts, _, err = telemetry.CollectCounters(reg)
	if err != nil || counts[1] != 7 {
		t.Fatalf("%v %v", counts, err)
	}
}
func TestMissingCountersAreNotZero(t *testing.T) {
	if _, _, err := telemetry.CollectCounters(prometheus.NewRegistry()); err == nil {
		t.Fatal("missing counters accepted")
	}
}

func TestProductionRegistryStartsWithValidZeroCounters(t *testing.T) {
	m := metrics.New()
	counts, source, err := telemetry.CollectCounters(m.Registry)
	if err != nil || counts != (telemetry.Counts{}) || (source != "legacy" && source != "forwarded") {
		t.Fatalf("%v %s %v", counts, source, err)
	}
}
