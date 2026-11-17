package telemetry

import (
	"fmt"
	"math"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// CollectCounters reads the in-process registry, never Graywolf's HTTP endpoint.
// Descriptors distinguish an available but still empty CounterVec from an absent
// capability. A forwarded value of zero must not select legacy accounting.
func CollectCounters(reg *prometheus.Registry) (Counts, string, error) {
	var counts Counts
	declared := map[string]bool{}
	descs := make(chan *prometheus.Desc)
	go func() { reg.Describe(descs); close(descs) }()
	names := []string{"graywolf_rx_frames_total", "graywolf_kiss_ingress_frames_total", "graywolf_digipeater_packets_total", "graywolf_tx_frames_total", "igate_packets_gated_total", "graywolf_tx_source_outcomes_total"}
	for d := range descs {
		s := d.String()
		for _, name := range names {
			if strings.Contains(s, `fqName: "`+name+`"`) {
				declared[name] = true
			}
		}
	}
	for _, name := range names[:4] {
		if !declared[name] {
			return counts, "", fmt.Errorf("required counter unavailable: %s", name)
		}
	}
	source := "legacy"
	if declared[names[5]] {
		source = "forwarded"
	}
	families, err := reg.Gather()
	if err != nil {
		return counts, source, err
	}
	for _, family := range families {
		name := family.GetName()
		relevant := false
		for _, n := range names {
			if name == n {
				relevant = true
				break
			}
		}
		if !relevant {
			continue
		}
		for _, m := range family.Metric {
			labels := map[string]string{}
			for _, pair := range m.Label {
				labels[pair.GetName()] = pair.GetValue()
			}
			index := -1
			switch name {
			case names[0]:
				index = 0
			case names[1]:
				if labels["mode"] == "tnc" {
					index = 0
				}
			case names[2]:
				if source == "legacy" {
					index = 1
				}
			case names[3]:
				index = 2
			case names[4]:
				if labels["direction"] == "rf_to_is" {
					index = 3
				}
				if labels["direction"] == "is_to_rf" {
					index = 4
				}
			case names[5]:
				if labels["source"] == "digipeater" && labels["outcome"] == "forwarded" {
					index = 1
				}
			}
			if index < 0 {
				continue
			}
			if m.Counter == nil {
				return Counts{}, source, fmt.Errorf("invalid counter type: %s", name)
			}
			value := m.Counter.GetValue()
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value >= float64(^uint64(0)) {
				return Counts{}, source, fmt.Errorf("invalid counter value: %s", name)
			}
			n := uint64(value)
			if ^uint64(0)-counts[index] < n {
				return Counts{}, source, fmt.Errorf("counter overflow")
			}
			counts[index] += n
		}
	}
	return counts, source, nil
}
