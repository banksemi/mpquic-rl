For the experiments to work correctly, ensure that :
1) You have a eBPF-enabled Linux kernel version (The experiments have been performed on Linux 4.18.18)
2) You have a version of iproute (expecially tc) matching your kernel version (especially, you want a version of tc supporting the loading of eBPF code)

To run the experiments, ensure you have mininet correctly installed and that you have the quic-fec executable named `quic-fec` in the root of this directory.
Then run : 

	sh run_experiments.sh

This will run the DCT experiments of the paper.
