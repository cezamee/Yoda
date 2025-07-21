BPF_CLANG ?= clang
BPF_CFLAGS ?= -O2 -g -target bpf -D__TARGET_ARCH_x86 -I/usr/include/ -I.
BPF_LDFLAGS ?=

BPF_SRC = xdp_redirect.c
BPF_OBJ = xdp_redirect.o

all: $(BPF_OBJ)

$(BPF_OBJ): $(BPF_SRC)
	$(BPF_CLANG) $(BPF_CFLAGS) -c $< -o $@

clean:
	rm -f $(BPF_OBJ)

.PHONY: all clean
