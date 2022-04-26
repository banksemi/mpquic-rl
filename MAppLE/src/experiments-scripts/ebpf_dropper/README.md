# ebpf_dropper

`ebpf_dropper` is a small eBPF program intended to be attached to `tc` and provides tools to drop TCP segments
based on TCP flags or payload. `ebpf_dropper` does not depend on any external library (e.g. bcc) 
except the libraries provides by the Linux kernel itself.

## example
The makefile compiles `ebpf_dropper` to drop the packets with a loss rate of 1%, with the seed 42, for the transfer between 10.0.0.1 and 10.0.0.2, considering only packets for which the UDP port 6121 is present (as src or dst port)
