package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/aeilot/TraceX/internal/profile"
)

func TestFindRetainsContextAndLocatesAcrossPages(t *testing.T) {
	p := profile.Demo()
	base := p.Spans[0]
	p.Spans = nil
	p.Edges = nil
	for i := 0; i < 630; i++ {
		span := base
		span.ID, span.ParentID, span.Kind = fmt.Sprintf("span-%04d", i), "", "model"
		span.Name, span.StartMS, span.DurationMS = "Work", float64(i), 1
		if i == 629 {
			span.Name = "Plan the next step"
		}
		p.Spans = append(p.Spans, span)
	}
	h, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/api/find?q=PLAN&kind=model", nil))
	var found struct {
		Total  int    `json:"total"`
		Offset int    `json:"offset"`
		ID     string `json:"span_id"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &found); err != nil {
		t.Fatal(err)
	}
	if r.Code != 200 || found.Total != 1 || found.Offset != 629 || found.ID != "span-0629" {
		t.Fatalf("unexpected match: %s", r.Body.String())
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/api/view?offset=500&limit=250&kind=model", nil))
	var view struct {
		Total   int             `json:"total_spans"`
		Profile profile.Profile `json:"profile"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Total != 630 || len(view.Profile.Spans) != 130 {
		t.Fatalf("context lost: total=%d, page=%d", view.Total, len(view.Profile.Spans))
	}
	for _, query := range []string{"q=plan&start_ms=0&end_ms=10", "q=missing", "q=plan&task=absent", "q=plan&kind=tool"} {
		r = httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/api/find?"+query, nil))
		if err := json.Unmarshal(r.Body.Bytes(), &found); err != nil {
			t.Fatal(err)
		}
		if r.Code != 200 || found.Total != 0 {
			t.Fatalf("unexpected filtered match for %s: %s", query, r.Body.String())
		}
	}
	for _, query := range []string{"index=-1", "index=1.5", "start_ms=30&end_ms=10", "index=NaN"} {
		r = httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/api/find?"+query, nil))
		if r.Code != 400 {
			t.Fatalf("invalid query accepted: %s", query)
		}
	}
}
