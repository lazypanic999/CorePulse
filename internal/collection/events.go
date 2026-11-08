package collection

import "github.com/lazypanic999/CorePulse/internal/corepulse"

// protocolEvents reads the UE, gNB, SMF and UPF logs at the end of a run.
// It never changes a run's quality: a failed read is recorded as an error,
// and the dataset then leaves the event columns empty for that run.
func protocolEvents(client corepulse.Client) ([]corepulse.ComponentEvents, string) {
	events, err := corepulse.CollectProtocolEvents(client)
	if err != nil {
		return nil, err.Error()
	}
	return events, ""
}
