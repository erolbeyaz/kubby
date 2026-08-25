package audit

import (
	"context"
	"log/slog"
	"sync"
)

// ShipperManager owns the current shipper and swaps it when the configuration changes.
//
// Swapping rather than reconfiguring: a sink holds a connection pool and a worker, and
// mutating one while it is mid-batch is how a batch ends up half-delivered to the old
// destination and half to the new.
type ShipperManager struct {
	logger *slog.Logger

	mu      sync.RWMutex
	current *Shipper
	config  SinkConfig
	enabled bool
}

func NewShipperManager(logger *slog.Logger) *ShipperManager {
	return &ShipperManager{logger: logger}
}

// Apply installs a configuration, replacing whatever was running.
//
// A configuration that cannot be built is reported and the previous shipper is left
// alone: an admin mistyping a URL should not silently stop the shipping that was working
// a moment ago.
func (m *ShipperManager) Apply(ctx context.Context, enabled bool, cfg SinkConfig) error {
	m.mu.Lock()
	unchanged := m.enabled == enabled && sameConfig(m.config, cfg)
	m.mu.Unlock()

	if unchanged {
		return nil
	}

	var next *Shipper
	if enabled {
		sink, err := NewSink(cfg)
		if err != nil {
			return err
		}
		next = NewShipper(sink, m.logger, ShipperOptions{})
	}

	m.mu.Lock()
	previous := m.current
	m.current = next
	m.config = cfg
	m.enabled = enabled
	m.mu.Unlock()

	if previous != nil {
		// Drained rather than dropped: what is already queued was produced under the old
		// configuration and still belongs at the old destination.
		if err := previous.Close(ctx); err != nil {
			m.logger.Warn("closing the previous audit sink failed", slog.String("error", err.Error()))
		}
	}

	if enabled {
		m.logger.Info("audit shipping started", slog.String("sink", cfg.Kind), slog.String("url", cfg.URL))
	} else {
		m.logger.Info("audit shipping stopped")
	}
	return nil
}

// Ship offers an event to whatever is currently running. With nothing configured it is a
// no-op: the database and the log stream have already recorded it.
func (m *ShipperManager) Ship(ev Shipped) {
	m.mu.RLock()
	shipper := m.current
	m.mu.RUnlock()

	if shipper != nil {
		shipper.Enqueue(ev)
	}
}

// Stats reports the running shipper, for /metrics and the settings screen.
func (m *ShipperManager) Stats() (ShipperStats, bool) {
	m.mu.RLock()
	shipper := m.current
	m.mu.RUnlock()

	if shipper == nil {
		return ShipperStats{}, false
	}
	return shipper.Stats(), true
}

// sameConfig avoids tearing down a working shipper for a save that changed nothing else
// on the settings screen. Written out rather than compared with == because the labels are
// a map, and a shallow comparison would miss a changed one.
func sameConfig(a, b SinkConfig) bool {
	if a.Kind != b.Kind || a.URL != b.URL || a.Index != b.Index ||
		a.Username != b.Username || a.Token != b.Token || a.Scheme != b.Scheme ||
		a.InsecureSkipVerify != b.InsecureSkipVerify ||
		len(a.ExtraLabels) != len(b.ExtraLabels) {
		return false
	}
	for key, value := range a.ExtraLabels {
		if b.ExtraLabels[key] != value {
			return false
		}
	}
	return true
}

func (m *ShipperManager) Close(ctx context.Context) error {
	m.mu.Lock()
	shipper := m.current
	m.current = nil
	m.mu.Unlock()

	if shipper == nil {
		return nil
	}
	return shipper.Close(ctx)
}
