#!/usr/bin/env python3

import json
import math
import sys
import glob
import argparse
import os
import csv
from collections import namedtuple, defaultdict

import seaborn as sns
import numpy as np
import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
from matplotlib.ticker import MaxNLocator
import pandas

RunConfig = namedtuple("RunConfig", "scheduler fec")
RunInfo = namedtuple("RunInfo", "packets")

Packet = namedtuple("Packet", "size destination fec timestamp")

PALETTE = "muted"

class FIGSIZE():
    BOX_M = (5, 5)
    WIDE_M = (12, 5)
    WIDE_L = (15, 8)

def get_mean(l):
    return sum(l) / len(l)


def get_stddev(l):
    mean = get_mean(l)
    return math.sqrt(sum([(x - mean)**2 for x in l]) / (len(l) - 1))


def get_median(l):
    return sorted(l)[len(l) // 2]


def get_z_score(x, mean, stddev):
    return abs((x - mean) / stddev)


def fixname(name):
    name = name[:3].replace("IOD", "R-IOD") + name[3:]
    return name.replace("LL", "LowRTT")


def get_population_stats(p):
    return ", ".join([
        f"mean: {round(get_mean(p), 2)}",
        f"median: {round(get_median(p), 2)}",
        f"stddev: {round(get_stddev(p), 2)}",
        f"min: {round(min(p), 2)}",
        f"max: {round(max(p), 2)}",
        f"sum: {round(sum(p), 2)}",
    ])


def z_filter_2d(data1, data2, z_cutoff = 2.5):
    ndata1 = []
    ndata2 = []

    mean1 = get_mean(data1)
    stddev1 = get_stddev(data1)

    mean2 = get_mean(data2)
    stddev2 = get_stddev(data2)

    for x, y in zip(data1, data2):
        if get_z_score(x, mean1, stddev1) < z_cutoff and get_z_score(y, mean2, stddev2) < z_cutoff:
            ndata1.append(x)
            ndata2.append(y)

    return ndata1, ndata2


def print_packet_stats(directory, conf, min_time, max_time):
    print(f"=== {conf.scheduler}, {conf.fec} ===")

    filenameGlobPackets = f'{directory}/{conf.scheduler}_{conf.fec}*_packet.csv'

    ratios_fec_on_wifi = []
    ratios_fec_on_lte = []
    ratios_app_on_wifi = []
    ratios_app_on_lte = []

    bps_throughput = []
    bps_goodput = []

    for filename in glob.glob(filenameGlobPackets):
        with open(filename, newline='') as csvfile:
            reader = csv.DictReader(csvfile)
            packets = []

            for row in reader:
                try:
                    size = int(row['size'])
                    destination = row['destination']
                    fec = row['fec'] == "true"
                    timestamp = int(row['timestamp'])
                except:
                    continue
                packets.append(Packet(
                    size,
                    destination,
                    fec,
                    timestamp,
                ))

            if len(packets) == 0:
                continue

            start_time = packets[0].timestamp

            app_on_wifi = 0
            app_on_lte = 0
            fec_on_wifi = 0
            fec_on_lte = 0

            last_sec = 0
            last_sec_throughput = 0
            last_sec_goodput = 0

            for i, packet in enumerate(packets):
                if packet.timestamp - start_time < min_time:
                    continue
                elif packet.timestamp - start_time > max_time:
                    break

                if packet.timestamp > last_sec + 10**9:
                    if last_sec_throughput > 0:
                        bps_throughput.append(last_sec_throughput)
                        bps_goodput.append(last_sec_goodput)

                    last_sec = packet.timestamp
                    last_sec_throughput = 0
                    last_sec_goodput = 0
                else:
                    last_sec_throughput += packet.size
                    if not packet.fec:
                        last_sec_goodput += packet.size

                if "10.1.2.5" in packet.destination:
                    if packet.fec:
                        fec_on_wifi += 1
                    else:
                        app_on_wifi += 1
                elif "10.1.3.5" in packet.destination:
                    if packet.fec:
                        fec_on_lte += 1
                    else:
                        app_on_lte += 1

            if fec_on_wifi > 0 and fec_on_lte > 0:
                ratios_fec_on_wifi.append(fec_on_wifi / (fec_on_lte + fec_on_wifi))
                ratios_fec_on_lte.append(fec_on_lte / (fec_on_lte + fec_on_wifi))

            if app_on_wifi > 0 and app_on_lte > 0:
                ratios_app_on_wifi.append(app_on_wifi / (app_on_lte + app_on_wifi))
                ratios_app_on_lte.append(app_on_lte / (app_on_lte + app_on_wifi))

    a = lambda x: x*8/2**20
    b = lambda x: round(a(x), 2)

    print("> fec on wifi")
    print(f"  {get_population_stats(ratios_fec_on_wifi)}")

    #print("> fec on lte")
    #print(f"  {get_population_stats(ratios_fec_on_lte)}")

    print("> app on wifi")
    print(f"  {get_population_stats(ratios_app_on_wifi)}")

    #print("> app on lte")
    #print(f"  {get_population_stats(ratios_app_on_lte)}")

    print("> throughput")
    print(f"  {b(get_mean(bps_throughput))}mb/s")

    print("> goodput")
    print(f"  {b(get_mean(bps_goodput))}mb/s")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('-d', '--dir', type=str,
                                help='folder containing logs')
    parser.add_argument('-f', '--fec-schemes', type=str, nargs='+',
                                help='fec schemes')
    parser.add_argument('-s', '--schedulers', type=str, nargs='+',
                                help='schedulers')
    parser.add_argument('-t', '--slow-start', type=int, default=15,
                                help='duration of slow start phase')
    parser.add_argument('--format', type=str, default='png',
                                help='export file format for plots')
    parser.add_argument('--server', default=False, action="store_true",
                                help='duration of slow start phase')
    parser.add_argument('--split', default=False, action="store_true",
                                help='split boxplot (for ABR test)')
    args = parser.parse_args()

    globals()["FORMAT"] = args.format

    confs = []
    for fecScheme in args.fec_schemes:
        for scheduler in args.schedulers:
            confs.append(RunConfig(scheduler, fecScheme))

    if args.server:
        if args.split:
            for conf in confs:
                print("Crosstraffic:")
                print_packet_stats(args.dir, conf, 0, 90*10**9)
        else:
            for conf in confs:
                print_packet_stats(args.dir, conf, 0, 160*10**9)
    else:
        pass


if __name__ == '__main__':
    sys.exit(main())
