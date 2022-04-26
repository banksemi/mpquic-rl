import argparse

TCP = 0x06
UDP = 0x11

def ip_to_int(ip):
    s = ip.split(".")
    sum = 0
    for i, b in enumerate(s):
        sum += int(b) << ((3-i)*8)
    return sum


parser = argparse.ArgumentParser()
parser.add_argument("--sequence", help="drop a sequence of packets (numbers separated by commas)", default="")
parser.add_argument("--gemodel", help="use a gilbert-elliott model", action="store_true")
parser.add_argument("-P", help="loss rate or p gemodel parameter (float) (0 <= p <= 100)", type=float, default=0)
parser.add_argument("-R", help="r gemodel parameter (float) (0 <= r <= 100)", type=float, default=100)
parser.add_argument("-K", help="k gemodel parameter (float) (0 <= k <= 100)", type=float, default=100)
parser.add_argument("-H", help="h gemodel parameter (float) (0 <= h <= 100)", type=float, default=0)
parser.add_argument("-f", help="filename to write the compiled eBPF bytecode into (default ebpf_dropper.o)", default="ebpf_dropper.o")
parser.add_argument("-v", help="verbose mode", action="store_true")
parser.add_argument("--ips", help="pair of IPv4 addresses to watch (separated by a comma), a packet must have both of "
                                "these addresses in either source or destination in order to be considered by the "
                                "dropper")
parser.add_argument("--port", help="port (tcp or udp), a packet must have this value either for the source or destination"
                                 "port in order to be considered by the dropper", type=int, default=443)
parser.add_argument("--udp", help="if set, monitor UDP packets instead of TCP", action="store_true")
parser.add_argument("--seed", help="prng seed (int)", type=int, default=42)
parser.add_argument("--headers", help="directory containing the uapi linux headers needed to compile the dropper",
                    default="./headers")
parser.add_argument("--attach", help="specifies the interface on which to attach the generated file",
                    default=None)
parser.add_argument("--attach-ingress", help="if set and the --attach option is used, the dropper will be attached in"
                                             "ingress instead of egress", action="store_true")
parser.add_argument("--clean", help="clean everything instead of compiling and attaching", action="store_true")


args = parser.parse_args()

sequence = args.sequence
gemodel = args.gemodel

if sequence and gemodel:
    raise Exception("Either gemodel or sequence but not both")

import os

if args.clean:
    if os.path.exists(args.f):
        os.remove(args.f)
    del_dev_cmd = "tc qdisc del dev {} clsact".format(args.attach)
    if args.v:
        print(del_dev_cmd)
    os.system(del_dev_cmd)
    exit()

clang_args = ""

ips = args.ips.split(",")

drop_sequence = 1 if sequence else 0
use_gemodel = 1 if gemodel else 0
protocol = UDP if args.udp else TCP

clang_args = "-DGEMODEL={} -DGEMODEL_P_PERCENTS={} -DGEMODEL_R_PERCENTS={} -DGEMODEL_K_PERCENTS={} " \
             "-DGEMODEL_H_PERCENTS={} -DPROBA_percents={} -DDROP_SEQUENCE={} -DSEQUENCE=\\{{{}\\}} -DSEED={} -DIP1_TO_DROP={} "\
             "-DIP2_TO_DROP={} -DPORT_TO_WATCH={} -DPROTOCOL_TO_WATCH={} -I{}"\
    .format(use_gemodel, args.P, args.R, args.K, args.H, args.P, drop_sequence, sequence, args.seed, ip_to_int(ips[0]), ip_to_int(ips[1]),
            args.port, protocol, args.headers)


compile_cmd = "clang -O2 {} -D__KERNEL__ -D__ASM_SYSREG_H -Wno-unused-value -Wno-pointer-sign " \
                  "-Wno-compare-distinct-pointer-types -I./headers -emit-llvm -c ebpf_dropper.c -o - | llc -march=bpf " \
                  "-filetype=obj -o {}".format(clang_args, args.f)
if args.v:
    print(compile_cmd)
os.system(compile_cmd)

if args.attach:
    add_dev_cmd = "tc qdisc replace dev {} clsact".format(args.attach)
    if args.v:
        print(add_dev_cmd)
    os.system(add_dev_cmd)
    direction = "ingress" if args.attach_ingress else 'egress'
    attach_cmd = "tc filter replace dev {} {} bpf obj {} section action direct-action"\
        .format(args.attach, direction, args.f)
    if args.v:
        print(attach_cmd)
    os.system(attach_cmd)

