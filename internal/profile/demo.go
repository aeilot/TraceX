package profile

// Demo is a deterministic synthetic workload. Parent links are explicitly
// authored for the demo, and make no claims about any real ARIES execution.
func Demo() Profile {
	p := empty()
	p.RunID = "synthetic-browser-research"
	p.Name = "Browser research · synthetic demo"
	p.DurationMS = 48000
	p.Configuration = map[string]any{"harness": "synthetic", "model": "demo-model", "concurrency": 3}
	p.Tasks = []Task{{ID: "research", Name: "Compare three research sources", Status: "succeeded", DurationMS: 48000, EvaluationStatus: "succeeded", CleanupStatus: "succeeded"}}
	add := func(id, parent, agent, name, kind string, start, duration float64) {
		p.Spans = append(p.Spans, Span{ID: id, TaskID: "research", AgentID: agent, ParentID: parent, Name: name, Kind: kind, Status: "succeeded", StartMS: start, DurationMS: duration, Source: "Synthetic demo", Attributes: map[string]any{"synthetic": true}})
	}
	add("orchestrator", "", "coordinator", "Coordinate research", "agent", 0, 47000)
	add("plan", "orchestrator", "coordinator", "Plan evidence collection", "model", 500, 3300)
	add("delegate-a", "orchestrator", "researcher-a", "Research source A", "agent", 4500, 23500)
	add("delegate-b", "orchestrator", "researcher-b", "Research source B", "agent", 5000, 29000)
	add("delegate-c", "orchestrator", "researcher-c", "Research source C", "agent", 6000, 24500)
	add("search-a", "delegate-a", "researcher-a", "Search source A", "browser", 6000, 8000)
	add("read-a", "delegate-a", "researcher-a", "Extract source A", "model", 15000, 10500)
	add("search-b", "delegate-b", "researcher-b", "Search source B", "browser", 6500, 12000)
	add("retry-b", "delegate-b", "researcher-b", "Retry unavailable page", "browser", 19000, 5000)
	p.Spans[len(p.Spans)-1].Status = "failed"
	add("read-b", "delegate-b", "researcher-b", "Summarize source B", "model", 24500, 8000)
	add("search-c", "delegate-c", "researcher-c", "Search source C", "browser", 7500, 9000)
	add("read-c", "delegate-c", "researcher-c", "Extract source C", "model", 17000, 10000)
	add("synthesis", "orchestrator", "coordinator", "Synthesize findings", "model", 34500, 6500)
	add("verify", "orchestrator", "reviewer", "Verify citations", "agent", 41500, 4500)
	add("citations", "verify", "reviewer", "Check evidence links", "tool", 42000, 3300)
	add("evaluate", "", "evaluator", "Independent evaluation", "evaluation", 47000, 1000)
	for second := 0; second <= 48; second++ {
		cpu := 15 + float64((second*17)%61)
		memory := float64(170+second*3) * 1024 * 1024
		p.Resources = append(p.Resources, Resource{TaskID: "research", Component: "harness", RuntimeID: "synthetic-harness", TimeMS: float64(second * 1000), CPUPercent: &cpu, MemoryBytes: &memory})
	}
	p.Edges = append(p.Edges, Edge{"plan", "search-a", "dependency"}, Edge{"plan", "search-b", "dependency"}, Edge{"plan", "search-c", "dependency"}, Edge{"search-a", "read-a", "dependency"}, Edge{"search-b", "retry-b", "dependency"}, Edge{"retry-b", "read-b", "dependency"}, Edge{"search-c", "read-c", "dependency"}, Edge{"read-a", "synthesis", "join"}, Edge{"read-b", "synthesis", "join"}, Edge{"read-c", "synthesis", "join"}, Edge{"synthesis", "citations", "dependency"})
	for i := range p.Spans {
		p.Spans[i].RuntimeID = "synthetic-harness"
		if p.Spans[i].Kind == "model" {
			input, output := float64(1000+i*100), float64(100+i*10)
			p.Spans[i].Usage = &Usage{InputTokens: &input, OutputTokens: &output}
		}
	}
	p.Spans = append(p.Spans, Span{ID: "prepare", TaskID: "research", Kind: "lifecycle", Name: "Prepare environment", DurationMS: 500, Source: "Synthetic demo", Attributes: map[string]any{"phase": "prepare", "synthetic": true}}, Span{ID: "cleanup", TaskID: "research", Kind: "lifecycle", Name: "Cleanup", StartMS: 47500, DurationMS: 500, Source: "Synthetic demo", Attributes: map[string]any{"phase": "cleanup", "synthetic": true}})
	for i := range p.Resources {
		gpu := float64((i * 11) % 95)
		p.Resources[i].GPUPercent = &gpu
	}
	p.Capabilities = []Capability{{"spans", "synthetic", "Authored synthetic events for exploring the interface."}, {"agent_dag", "synthetic", "Explicit synthetic parent links show concurrent researchers; no temporal inference."}, {"resources", "synthetic", "Generated resource samples, not measured hardware."}}
	p.Warnings = []string{"This dataset is entirely synthetic. Durations, agents, parent links, and resource samples are authored demonstration data."}
	_ = Validate(&p)
	return p
}

// DemoVariant provides a second explicitly synthetic comparison configuration.
func DemoVariant() Profile {
	p := Demo()
	p.RunID = "synthetic-slower-model"
	p.Name = "Synthetic comparison · slower model"
	p.Configuration["concurrency"] = 3
	p.Configuration["model"] = "demo-model-small"
	for i := range p.Spans {
		p.Spans[i].StartMS *= 1.25
		p.Spans[i].DurationMS *= 1.25
	}
	for i := range p.Tasks {
		p.Tasks[i].DurationMS *= 1.25
	}
	for i := range p.Resources {
		p.Resources[i].TimeMS *= 1.25
	}
	p.DurationMS *= 1.25
	return p
}
