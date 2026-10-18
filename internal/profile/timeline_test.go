package profile

import "testing"

func span(id, task, kind string, start, duration float64) Span {
	return Span{ID: id, TaskID: task, Kind: kind, Name: id, StartMS: start, DurationMS: duration}
}

// 1000 ms across 100 px: 10 ms per pixel.
func TestLayoutTimelineMergesOnlyIndistinguishableNeighbors(t *testing.T) {
	spans := []Span{
		span("wide-a", "t1", "model", 0, 300),   // 30 px: always its own bar
		span("tiny-1", "t1", "tool", 300, 5),    // 0.5 px each, adjacent:
		span("tiny-2", "t1", "browser", 306, 5), // the three tiny spans
		span("tiny-3", "t1", "tool", 312, 5),    // form one cluster
		span("wide-b", "t1", "model", 600, 200), // 28 px away: its own bar
		span("nested", "t1", "tool", 650, 50),   // overlaps wide-b: second row
		span("other", "t2", "model", 0, 1000),   // another task: its own lane
	}
	l := LayoutTimeline(spans, 0, 1000, 100)
	if l.Spans != 7 || len(l.Lanes) != 2 || l.Lanes[0].Rows != 2 || l.Lanes[1].Rows != 1 {
		t.Fatalf("lanes = %+v spans = %d", l.Lanes, l.Spans)
	}
	type at struct {
		lane  int
		start float64
	}
	items := map[at]TimelineItem{}
	for _, it := range l.Items {
		items[at{it.Lane, it.StartMS}] = it
	}
	if a := items[at{0, 0}]; a.Span == nil || a.Span.ID != "wide-a" || a.Count != 1 {
		t.Fatalf("a resolvable span must stay its own bar: %+v", a)
	}
	c := items[at{0, 300}]
	if c.Span != nil || c.Count != 3 || c.Kind != "tool" || c.DurationMS != 17 || c.Kinds["tool"] != 2 || c.Kinds["browser"] != 1 {
		t.Fatalf("cluster = %+v", c)
	}
	if b := items[at{0, 600}]; b.Span == nil || b.Span.ID != "wide-b" || b.Row != 0 {
		t.Fatalf("wide-b = %+v", b)
	}
	if n := items[at{0, 650}]; n.Span == nil || n.Row != 1 {
		t.Fatalf("nested = %+v", n)
	}
	if o := items[at{1, 0}]; o.Span == nil || o.Span.ID != "other" {
		t.Fatalf("other lane = %+v", o)
	}
	if len(l.Items) != 5 {
		t.Fatalf("items = %d", len(l.Items))
	}
}

func TestLayoutTimelineKeepsEverySpanWhenZoomedIn(t *testing.T) {
	spans := []Span{span("a", "t", "tool", 300, 5), span("b", "t", "tool", 306, 5), span("c", "t", "tool", 312, 5)}
	// 20 ms across 1000 px: each span is 250 px wide.
	l := LayoutTimeline(spans, 300, 320, 1000)
	if len(l.Items) != 3 {
		t.Fatalf("zoomed in, every span must be its own bar: %+v", l.Items)
	}
	for _, it := range l.Items {
		if it.Span == nil || it.Count != 1 {
			t.Fatalf("item = %+v", it)
		}
	}
}

func TestLayoutTimelineClipsToWindowAndBoundsItems(t *testing.T) {
	spans := make([]Span, 0, 10000)
	for i := 0; i < 10000; i++ {
		spans = append(spans, span("s", "t", "tool", float64(i), 0.5))
	}
	// 10,000 tiny spans in one row over 200 px collapse into one cluster.
	l := LayoutTimeline(spans, 0, 10000, 200)
	if len(l.Items) != 1 || l.Items[0].Count != 10000 || l.Spans != 10000 {
		t.Fatalf("items = %d first = %+v", len(l.Items), l.Items[0])
	}
	clipped := LayoutTimeline([]Span{span("long", "t", "model", -50, 200)}, 0, 100, 100)
	if it := clipped.Items[0]; it.StartMS != 0 || it.DurationMS != 100 || it.Span.StartMS != -50 {
		t.Fatalf("clipped = %+v", it)
	}
	if empty := LayoutTimeline(spans, 10, 10, 100); len(empty.Items) != 0 {
		t.Fatal("an empty window has no items")
	}
}

func TestLayoutTimelineBoundsLaneRowsWithAnOverflowRow(t *testing.T) {
	spans := make([]Span, 0, 30)
	for i := 0; i < 30; i++ {
		spans = append(spans, span("c", "t", "browser", float64(i), 500)) // 30 concurrent, each 50 px wide
	}
	l := LayoutTimeline(spans, 0, 1000, 100)
	if l.Lanes[0].Rows != maxLaneRows {
		t.Fatalf("rows = %d", l.Lanes[0].Rows)
	}
	singles, overflow := 0, TimelineItem{}
	for _, it := range l.Items {
		if it.Span != nil {
			singles++
		} else {
			overflow = it
		}
	}
	// Eleven rows hold one span each; the remaining nineteen overlap in the
	// overflow row and merge although each is wide enough to draw.
	if singles != maxLaneRows-1 || overflow.Count != 30-(maxLaneRows-1) || !overflow.Overflow || overflow.Row != maxLaneRows-1 {
		t.Fatalf("singles = %d overflow = %+v", singles, overflow)
	}
}
