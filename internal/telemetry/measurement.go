package telemetry

import (
	"encoding/json"
	"fmt"
	"time"
)

// Measurement identifies the Metrics API interval from Timestamp-Window
// through Timestamp. Sample.Timestamp remains the CorePulse capture time.
// A nil *Measurement means unknown metadata, including historical JSONL.
type Measurement struct {
	Timestamp time.Time `json:"timestamp"`
	Window    string    `json:"window"`
}

func (m Measurement) validate() error {
	if m.Timestamp.IsZero() {
		return fmt.Errorf("source timestamp must be nonzero")
	}
	d, err := time.ParseDuration(m.Window)
	if err != nil || d <= 0 {
		return fmt.Errorf("source window must be a positive duration, got %q", m.Window)
	}
	return nil
}

// UnmarshalJSON applies the same validation to stored JSONL as to live metrics.
// Missing optional metadata is handled by the containing pointer, not invented.
func (m *Measurement) UnmarshalJSON(data []byte) error {
	type plain Measurement
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("invalid source measurement: %w", err)
	}
	next := Measurement(decoded)
	if err := next.validate(); err != nil {
		return err
	}
	*m = next
	return nil
}

func decodeMeasurement(timestamp, window json.RawMessage) (*Measurement, error) {
	if len(timestamp) == 0 && len(window) == 0 {
		return nil, nil
	}
	if len(timestamp) == 0 || len(window) == 0 {
		return nil, fmt.Errorf("source timestamp and window must be supplied together")
	}
	var at time.Time
	var span string
	if err := json.Unmarshal(timestamp, &at); err != nil {
		return nil, fmt.Errorf("invalid source timestamp: %w", err)
	}
	if err := json.Unmarshal(window, &span); err != nil {
		return nil, fmt.Errorf("invalid source window: %w", err)
	}
	m := &Measurement{Timestamp: at, Window: span}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m, nil
}
