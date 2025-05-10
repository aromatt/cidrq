.PHONY: build clean run

GOBUILD=go build

LIB_SOURCES := $(wildcard */*.go)
CMD_SOURCES := $(wildcard cmd/*/*.go)
CMD_DIRS := $(wildcard cmd/*)
BIN_TARGETS := $(patsubst cmd/%,bin/%,$(CMD_DIRS))

.PHONY: all

all: $(BIN_TARGETS)

$(BIN_TARGETS): $(CMD_SOURCES) $(LIB_SOURCES) go.mod go.sum
	$(GOBUILD) -o $@ ./$(patsubst bin/%,cmd/%,$@)

clean:
	rm -f bin/*
