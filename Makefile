## BPF build variables
BPF_CLANG ?= clang
BPF_CFLAGS ?= -O2 -g -target bpf -D__TARGET_ARCH_x86 -I/usr/include/ -I.
BPF_LDFLAGS ?=
BPF_SRC = bpf/xdp_redirect.c
BPF_OBJ = bpf/xdp_redirect.o

## Go build variables
GO ?= go
YODA_BIN = yoda
CLI_BIN = yoda-client

.PHONY: all yoda cli bpf clean

all: yoda cli bpf

yoda:
	$(GO) build -o bin/$(YODA_BIN)

cli:
	cd cmd/cli && $(GO) build -o ../../bin/$(CLI_BIN)

bpf: $(BPF_OBJ)

$(BPF_OBJ): $(BPF_SRC)
	$(BPF_CLANG) $(BPF_CFLAGS) -c $< -o $@

clean:
	rm -f $(BPF_OBJ) bin/$(YODA_BIN) bin/$(CLI_BIN)
