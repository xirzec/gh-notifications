BINARY = gh-notifications

.PHONY: build test lint fmt clean

build:
	go build -o $(BINARY) .

test:
	go test ./...

fmt:
	gofmt -w .

lint:
	go vet ./...
	go test -count=1 -run TestSourceFormatted ./...

clean:
	rm -f $(BINARY) $(BINARY).exe $(BINARY).exe~
