package evaluation

import "sort"

type ClassMetrics struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	Support   int     `json:"support"`
	Predicted int     `json:"predicted"`
}

type Metrics struct {
	Labels    []string                `json:"labels"`
	Confusion [][]int                 `json:"confusion"`
	Classes   map[string]ClassMetrics `json:"classes"`
	Total     int                     `json:"total"`
	Predicted int                     `json:"predicted"`
	Abstained int                     `json:"abstained"`
	Coverage  float64                 `json:"coverage"`
	Accuracy  float64                 `json:"accuracy_on_predicted"`
	MacroF1   float64                 `json:"macro_f1"`
}

type observation struct{ runID, truth, prediction string }

// Confusion rows are truth, columns prediction. Abstentions retain support and
// count as missed true-class recall, but do not invent a predicted class.
func measure(labels []string, observations []observation) Metrics {
	m := Metrics{Labels: labels, Classes: map[string]ClassMetrics{}, Total: len(observations), Confusion: make([][]int, len(labels))}
	indices := map[string]int{}
	for i, label := range labels {
		indices[label] = i
		m.Confusion[i] = make([]int, len(labels))
	}
	for _, o := range observations {
		c := m.Classes[o.truth]
		c.Support++
		m.Classes[o.truth] = c
		if o.prediction == "" {
			m.Abstained++
			continue
		}
		m.Predicted++
		m.Confusion[indices[o.truth]][indices[o.prediction]]++
		p := m.Classes[o.prediction]
		p.Predicted++
		m.Classes[o.prediction] = p
	}
	correct := 0
	for i, label := range labels {
		c := m.Classes[label]
		tp := m.Confusion[i][i]
		correct += tp
		if c.Predicted > 0 {
			c.Precision = float64(tp) / float64(c.Predicted)
		}
		if c.Support > 0 {
			c.Recall = float64(tp) / float64(c.Support)
		}
		if c.Precision+c.Recall > 0 {
			c.F1 = 2 * c.Precision * c.Recall / (c.Precision + c.Recall)
		}
		m.MacroF1 += c.F1
		m.Classes[label] = c
	}
	if m.Total > 0 {
		m.Coverage = float64(m.Predicted) / float64(m.Total)
	}
	if m.Predicted > 0 {
		m.Accuracy = float64(correct) / float64(m.Predicted)
	}
	if len(labels) > 0 {
		m.MacroF1 /= float64(len(labels))
	}
	return m
}

func byRun(labels []string, observations []observation) []observation {
	groups := map[string][]observation{}
	for _, o := range observations {
		groups[o.runID] = append(groups[o.runID], o)
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]observation, 0, len(ids))
	for _, id := range ids {
		rows := groups[id]
		votes := map[string]int{}
		for _, row := range rows {
			if row.prediction != "" {
				votes[row.prediction]++
			}
		}
		best, prediction := 0, ""
		for _, label := range labels {
			if votes[label] > best {
				best, prediction = votes[label], label
			}
		}
		out = append(out, observation{runID: id, truth: rows[0].truth, prediction: prediction})
	}
	return out
}
