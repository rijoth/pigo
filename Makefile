BIN := pigo

.PHONY: build test vet fmt loc clean smoke

build:
	go build -o $(BIN) ./cmd/pigo

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f $(BIN)

loc:
	@for p in cmd/pigo internal/agent internal/llm internal/models internal/providers internal/fantasy internal/tools internal/session internal/ui internal/markdown internal/diff internal/selector internal/config internal/skills internal/event; do \
		n=$$(find $$p -name '*.go' ! -name '*_test.go' -exec cat {} + | grep -vE '^[[:space:]]*(//|$$)' | wc -l); \
		printf '%-18s %s\n' $$p $$n; \
	done

smoke:
	go run ./cmd/pigo -p "say hello"
