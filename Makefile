.PHONY: build clean run

GOBUILD=go build

LIB_SOURCES := $(wildcard pkg/*.go)
BIN_TARGET := cidrq

.PHONY: all

all: $(BIN_TARGET)

cidrq: $(LIB_SOURCES) go.mod go.sum
	$(GOBUILD) -o $@ main.go

clean:
	rm $(BIN_TARGET)
