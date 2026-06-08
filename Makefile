BINARY = gh-notifications

.PHONY: build test lint clean

build:
	go build -o $(BINARY) .

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -f $(BINARY) $(BINARY).exe $(BINARY).exe~
