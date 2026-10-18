package profile

import "sort"

// TimelineLane is one task × agent lane of the linear timeline; Name is
// empty for spans without a recorded agent.
type TimelineLane struct {
	Name string `json:"name"`
	Task string `json:"task"`
	Rows int    `json:"rows"`
}

// TimelineItem is one drawable bar: a span, or a cluster of neighboring spans
// in the same row that are too small to tell apart at the requested width.
type TimelineItem struct {
	Lane       int     `json:"lane"`
	Row        int     `json:"row"`
	StartMS    float64 `json:"start_ms"`
	DurationMS float64 `json:"duration_ms"`
	// Kind is the span kind, or for a cluster the kind with the most spans.
	Kind string `json:"kind"`
	// Span is set for a single span; Count and Kinds describe a cluster.
	Span  *Span          `json:"span,omitempty"`
	Count int            `json:"count"`
	Kinds map[string]int `json:"kinds,omitempty"`
	// Overflow marks a cluster of concurrent spans beyond the lane's row
	// limit; they overlap in time, so zooming alone does not separate them.
	Overflow bool `json:"overflow,omitempty"`
}

// TimelineLayout is every span in a time window laid out for one plot width,
// so that the timeline needs no pagination however many spans a run has.
type TimelineLayout struct {
	Lanes []TimelineLane `json:"lanes"`
	Items []TimelineItem `json:"items"`
	Spans int            `json:"spans"`
}

// Bars narrower than minBarPX cannot be told apart; neighbors closer than
// mergeGapPX are visually adjacent.
const (
	minBarPX   = 2.0
	mergeGapPX = 1.0
	// maxLaneRows bounds a lane's height. Spans more concurrent than its
	// first maxLaneRows-1 rows share the last, overflow row, where overlapping
	// and adjacent spans merge regardless of width.
	maxLaneRows = 12
)

// LayoutTimeline packs spans (sorted by start) into task × agent lanes and
// rows exactly as the browser timeline does, then merges, row by row, adjacent
// narrow bars that would be indistinguishable at width pixels across
// [start, end]. A span wide enough to draw is always its own bar, so zooming
// in recovers every span.
func LayoutTimeline(spans []Span, start, end, width float64) TimelineLayout {
	out := TimelineLayout{Lanes: []TimelineLane{}, Items: []TimelineItem{}, Spans: len(spans)}
	if end <= start || width <= 0 {
		return out
	}
	px := width / (end - start)
	laneIndex := map[string]int{}
	rowEnds := [][]float64{}
	rowItems := map[[2]int][]int{} // lane,row -> indexes into out.Items
	for i := range spans {
		s := spans[i]
		task, agent := s.TaskID, s.AgentID
		key := task + "\x00" + agent
		lane, ok := laneIndex[key]
		if !ok {
			lane = len(out.Lanes)
			laneIndex[key] = lane
			out.Lanes = append(out.Lanes, TimelineLane{Name: agent, Task: task})
			rowEnds = append(rowEnds, nil)
		}
		ends := rowEnds[lane]
		row := -1
		for r, e := range ends {
			if r < maxLaneRows-1 && e <= s.StartMS {
				row = r
				break
			}
		}
		if row < 0 {
			row = min(len(ends), maxLaneRows-1)
			if row == len(ends) {
				ends = append(ends, 0)
			}
		}
		overflow := row == maxLaneRows-1
		ends[row] = max(ends[row], s.StartMS+s.DurationMS)
		rowEnds[lane] = ends
		if len(ends) > out.Lanes[lane].Rows {
			out.Lanes[lane].Rows = len(ends)
		}

		clipStart, clipEnd := max(s.StartMS, start), min(s.StartMS+s.DurationMS, end)
		if clipEnd < clipStart {
			clipEnd = clipStart
		}
		cell := [2]int{lane, row}
		if list := rowItems[cell]; len(list) > 0 {
			last := &out.Items[list[len(list)-1]]
			lastEnd := last.StartMS + last.DurationMS
			gap := (clipStart - lastEnd) * px
			// A resolvable span is never absorbed: merge only a narrow span
			// into a narrow bar or an existing cluster.
			narrow := (clipEnd-clipStart)*px < minBarPX && (last.Span == nil || last.DurationMS*px < minBarPX)
			if gap < mergeGapPX && (narrow || overflow) {
				if last.Span != nil {
					last.Kinds = map[string]int{last.Span.Kind: 1}
					last.Span = nil
				}
				last.Count++
				last.Kinds[s.Kind]++
				last.Overflow = last.Overflow || overflow
				last.DurationMS = max(lastEnd, clipEnd) - last.StartMS
				last.Kind = dominant(last.Kinds)
				continue
			}
		}
		span := spans[i]
		out.Items = append(out.Items, TimelineItem{Lane: lane, Row: row, StartMS: clipStart, DurationMS: clipEnd - clipStart, Kind: s.Kind, Span: &span, Count: 1})
		rowItems[cell] = append(rowItems[cell], len(out.Items)-1)
	}
	return out
}

func dominant(kinds map[string]int) string {
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	best := ""
	for _, k := range names {
		if best == "" || kinds[k] > kinds[best] {
			best = k
		}
	}
	return best
}
