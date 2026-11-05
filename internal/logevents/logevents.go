// Package logevents turns UERANSIM and Open5GS log lines into timestamped
// protocol event observations.
//
// The parser is pure: it reads text and never contacts Kubernetes. Events are
// observations that a marker appeared in a log. The absence of an event is not
// evidence of a healthy state or of a zero failure rate; Result.Window only
// says which time span the parsed lines covered.
package logevents

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Kind identifies a protocol event.
type Kind string

const (
	RegistrationSuccess  Kind = "registration_success"
	RegistrationFailure  Kind = "registration_failure"
	PDUSessionSuccess    Kind = "pdu_session_success"
	PDUSessionFailure    Kind = "pdu_session_failure"
	PFCPAssociated       Kind = "pfcp_associated"
	PFCPDeassociated     Kind = "pfcp_deassociated"
	PFCPHeartbeatTimeout Kind = "pfcp_heartbeat_timeout"
)

// Format identifies a pinned log line layout.
type Format string

const (
	// FormatUERANSIM is "[2026-11-02 10:15:30.123] [nas] [info] message".
	FormatUERANSIM Format = "ueransim"
	// FormatOpen5GS is "11/02 10:15:30.123: [smf] INFO: message (file:line)".
	// It carries no year and no zone.
	FormatOpen5GS Format = "open5gs"
)

// Source describes where the lines came from. It is copied into every event.
type Source struct {
	// Component is required for UERANSIM logs ("ue" or "gnb"), whose bracket
	// tag is a protocol layer rather than a network function. Open5GS lines
	// name their component, which then takes precedence.
	Component string `json:"component,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`
	Format    Format `json:"format"`
}

// Options controls time interpretation. Both log formats omit the zone, so
// the caller must state it rather than relying on the host's local zone.
type Options struct {
	// Location the log timestamps were written in. Required.
	Location *time.Location
	// Reference is required for Open5GS logs, whose timestamps have no year.
	// It should be the time the logs were collected. The year of Reference is
	// used, or the previous year when that would place an event more than one
	// day after Reference (a December log read in January).
	Reference time.Time
}

// Event is one observed protocol marker.
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Component string    `json:"component"`
	Kind      Kind      `json:"kind"`
	// Line is the 1-based line number in the parsed input.
	Line   int    `json:"line"`
	Source Source `json:"source"`
	// MarkerObservedLive is true only for markers seen in the CorePulse lab
	// (docs/validated-signals.md). Other markers follow the upstream log text
	// and have not yet been confirmed against the pinned lab images.
	MarkerObservedLive bool `json:"marker_observed_live"`
}

// Skipped records a line that looked like a log line but could not be used.
type Skipped struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Window is the time span covered by timestamped lines in the input.
type Window struct {
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
}

// Result is the outcome of parsing one log stream. It intentionally has no
// health or rate field.
type Result struct {
	Events []Event `json:"events"`
	// Skipped lists malformed lines. Message text is not kept so that
	// subscriber identifiers in logs are never copied into results.
	Skipped []Skipped `json:"skipped"`
	// TimestampedLines counts lines with a valid timestamp, matched or not.
	TimestampedLines int `json:"timestamped_lines"`
	// Window is nil when no line had a valid timestamp.
	Window *Window `json:"window"`
}

type marker struct {
	kind       Kind
	format     Format
	components []string
	pattern    *regexp.Regexp
	live       bool
}

// Failure markers are checked before success markers, and de-association
// before association.
var markers = []marker{
	{RegistrationFailure, FormatUERANSIM, []string{"ue"}, regexp.MustCompile(`(?i)registration (failed|reject)`), false},
	{RegistrationSuccess, FormatUERANSIM, []string{"ue"}, regexp.MustCompile(`(?i)registration is successful`), true},
	{PDUSessionFailure, FormatUERANSIM, []string{"ue"}, regexp.MustCompile(`(?i)pdu session establishment.*(reject|fail)`), false},
	{PDUSessionSuccess, FormatUERANSIM, []string{"ue"}, regexp.MustCompile(`(?i)pdu session establishment is successful`), true},
	{PFCPDeassociated, FormatOpen5GS, []string{"smf", "upf"}, regexp.MustCompile(`PFCP de-associated`), false},
	{PFCPAssociated, FormatOpen5GS, []string{"smf", "upf"}, regexp.MustCompile(`PFCP associated`), true},
	{PFCPHeartbeatTimeout, FormatOpen5GS, []string{"smf", "upf"}, regexp.MustCompile(`No Heartbeat from (UPF|SMF)`), false},
}

var (
	ansiEscape   = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	ueransimLine = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})\] \[([A-Za-z0-9_-]+)\] \[([a-z]+)\] (.*)$`)
	open5gsLine  = regexp.MustCompile(`^(\d{2}/\d{2} \d{2}:\d{2}:\d{2}\.\d{3}): \[([a-z0-9_-]+)\] ([A-Z]+): (.*)$`)
	// Lines that start like a timestamp but do not parse are reported as
	// malformed; anything else (stack traces, banners) is ignored silently.
	ueransimPrefix = regexp.MustCompile(`^\[\d`)
	open5gsPrefix  = regexp.MustCompile(`^\d{2}/\d{2} `)
)

const maxLineBytes = 1 << 20

// Parse reads log lines from r and returns the protocol events they contain.
func Parse(r io.Reader, src Source, opts Options) (Result, error) {
	if opts.Location == nil {
		return Result{}, errors.New("logevents: Options.Location is required; log timestamps carry no zone")
	}
	switch src.Format {
	case FormatUERANSIM:
		if src.Component == "" {
			return Result{}, errors.New("logevents: Source.Component is required for UERANSIM logs")
		}
	case FormatOpen5GS:
		if opts.Reference.IsZero() {
			return Result{}, errors.New("logevents: Options.Reference is required for yearless Open5GS timestamps")
		}
	default:
		return Result{}, fmt.Errorf("logevents: unsupported format %q", src.Format)
	}

	result := Result{Events: []Event{}, Skipped: []Skipped{}}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimRight(ansiEscape.ReplaceAllString(scanner.Text(), ""), "\r")
		ts, component, message, reason := splitLine(text, src, opts)
		if reason != "" {
			result.Skipped = append(result.Skipped, Skipped{Line: line, Reason: reason})
			continue
		}
		if ts.IsZero() {
			continue
		}
		result.TimestampedLines++
		if result.Window == nil {
			result.Window = &Window{First: ts, Last: ts}
		} else {
			if ts.Before(result.Window.First) {
				result.Window.First = ts
			}
			if ts.After(result.Window.Last) {
				result.Window.Last = ts
			}
		}
		if m, ok := match(src.Format, component, message); ok {
			result.Events = append(result.Events, Event{
				Timestamp: ts, Component: component, Kind: m.kind, Line: line,
				Source: src, MarkerObservedLive: m.live,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("logevents: read line %d: %w", line+1, err)
	}
	return result, nil
}

// splitLine returns a zero timestamp and empty reason for lines that are not
// log records at all.
func splitLine(text string, src Source, opts Options) (time.Time, string, string, string) {
	switch src.Format {
	case FormatUERANSIM:
		parts := ueransimLine.FindStringSubmatch(text)
		if parts == nil {
			if ueransimPrefix.MatchString(text) {
				return time.Time{}, "", "", "malformed ueransim line"
			}
			return time.Time{}, "", "", ""
		}
		ts, err := time.ParseInLocation("2006-01-02 15:04:05.000", parts[1], opts.Location)
		if err != nil {
			return time.Time{}, "", "", "invalid timestamp"
		}
		return ts.UTC(), src.Component, parts[4], ""
	default:
		parts := open5gsLine.FindStringSubmatch(text)
		if parts == nil {
			if open5gsPrefix.MatchString(text) {
				return time.Time{}, "", "", "malformed open5gs line"
			}
			return time.Time{}, "", "", ""
		}
		ts, err := open5gsTime(parts[1], opts)
		if err != nil {
			return time.Time{}, "", "", "invalid timestamp"
		}
		return ts, parts[2], parts[4], ""
	}
}

func open5gsTime(value string, opts Options) (time.Time, error) {
	ref := opts.Reference.In(opts.Location)
	for _, year := range []int{ref.Year(), ref.Year() - 1} {
		ts, err := time.ParseInLocation("2006/01/02 15:04:05.000", fmt.Sprintf("%04d/%s", year, value), opts.Location)
		if err != nil {
			return time.Time{}, err
		}
		if !ts.After(ref.Add(24 * time.Hour)) {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, errors.New("timestamp after reference")
}

func match(format Format, component, message string) (marker, bool) {
	for _, m := range markers {
		if m.format != format || !contains(m.components, component) {
			continue
		}
		if m.pattern.MatchString(message) {
			return m, true
		}
	}
	return marker{}, false
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// Dedupe removes events repeated across overlapping reads of the same log,
// such as two "kubectl logs --tail" calls. Events are equal when timestamp,
// component, kind, pod, and container match. Distinct occurrences with
// different timestamps are kept, so repeated real events remain countable.
// Input order is preserved.
func Dedupe(events []Event) []Event {
	type key struct {
		ts                        int64
		component, pod, container string
		kind                      Kind
	}
	seen := map[key]bool{}
	out := make([]Event, 0, len(events))
	for _, e := range events {
		k := key{e.Timestamp.UnixNano(), e.Component, e.Source.Pod, e.Source.Container, e.Kind}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}
