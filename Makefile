.PHONY: build clean run

GOBUILD=go build

SOURCES := $(wildcard *.go)
BIN_TARGET := cidrq

.PHONY: all

all: $(BIN_TARGET)

cidrq: $(SOURCES) go.mod go.sum
	$(GOBUILD) -o $@ ./...

clean:
	rm $(BIN_TARGET)
