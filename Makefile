export GOCACHE := $(CURDIR)/.cache/go-build
export GOMODCACHE := $(CURDIR)/.cache/go-mod

.PHONY: build test test-web test-race vet check run clean
build:
	go build -o bin/tracex ./cmd/tracex
test:
	go test ./...
test-web:
	node --check web/resource-view.js
	node --check web/dag.js
	node --check web/timeline.js
	node --check web/insight-model.js
	node --check web/insights.js
	node --check web/motion.js
	node --check web/workspace.js
	node --check web/workspace-model.js
	node --check web/app.js
	node --check web/experiments.js
	node --check web/experiment-model.js
	node --test web/*.test.cjs
test-race:
	go test -race ./...
vet:
	go vet ./...
check: test test-web test-race vet build
run:
	go run ./cmd/tracex
clean:
	rm -rf bin
