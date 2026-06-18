BINARY = gh-notifications

.PHONY: build test lint fmt clean

build:
	go build -o $(BINARY) .

test:
	go test ./...

fmt:
	gofmt -w .

lint:
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "The following files need gofmt (run 'make fmt'):"; \
		gofmt -l .; \
		exit 1; \
	fi
	go vet ./...

clean:
	rm -f $(BINARY) $(BINARY).exe $(BINARY).exe~
