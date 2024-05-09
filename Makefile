.PHONY: build clean run

GOBUILD=go build

LIB_SOURCES := $(wildcard */*.go)
CMD_SOURCES := $(wildcard cmd/*/*.go)
CMD_DIRS := $(wildcard cmd/*)
BIN_TARGETS := $(patsubst cmd/%,bin/%,$(CMD_DIRS))
DIST_TARGETS := $(patsubst cmd/%,dist/%,$(CMD_DIRS))

.PHONY: all

all: $(BIN_TARGETS) $(DIST_TARGETS)

$(BIN_TARGETS): $(CMD_SOURCES) $(LIB_SOURCES)
	$(GOBUILD) -o $@ ./$(patsubst bin/%,cmd/%,$@)

$(DIST_TARGETS): $(CMD_SOURCES) $(LIB_SOURCES)
	GOOS=linux $(GOBUILD) -o $@ ./$(patsubst dist/%,cmd/%,$@)

clean:
	rm -f bin/* dist/*
