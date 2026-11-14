.PHONY: build test race cover lint fmt vet vuln demo check clean

build:
	go build -trimpath -o bin/leanfox ./cmd/leanfox

test:
	go test ./... -count=1

race:
	go test -race ./... -count=1

# Coverage of the libraries. The command package is exercised by tests that run the built program as a separate
# process, which a coverage tool cannot see, so it is left out of this number.
cover:
	go test ./internal/... -count=1 -coverprofile=cover.out -coverpkg=./internal/...
	go tool cover -func=cover.out | tail -1

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	@test -z "$$(gofmt -l .)" || (echo "these files need gofmt:"; gofmt -l .; exit 1)
	go vet ./...
	staticcheck ./...
	errcheck -ignoretests -exclude .errcheck-excludes ./...
	gosec -quiet ./...

vuln:
	govulncheck ./...

# The report on a made-up session, so that it can be tried without any Claude Code history.
demo: build
	./bin/leanfox report examples/demo-session.jsonl

# Everything CI runs, in the order it runs it.
check: lint race cover vuln

clean:
	rm -rf bin cover.out
