#!/usr/bin/env python3

import math
import sys
import glob
import argparse
import os
import csv
from collections import namedtuple, defaultdict

import seaborn as sns
import matplotlib.pyplot as plt
from matplotlib.ticker import MaxNLocator
import pandas

RunConfig = namedtuple("RunConfig", "scheduler fec")
RunInfo = namedtuple("RunInfo", "request_size rtts owds")

PALETTE = "muted"

def get_mean(l):
    return sum(l) / len(l)


def get_stddev(l):
    mean = get_mean(l)
    return math.sqrt(sum([(x - mean)**2 for x in l]) / (len(l) - 1))


def get_median(l):
    return sorted(l)[len(l) // 2]


def get_z_score(x, mean, stddev):
    return abs((x - mean) / stddev)


def get_population_stats(p):
    return ", ".join([
        f"mean: {round(get_mean(p), 2)}",
        f"median: {round(get_median(p), 2)}",
        f"stddev: {round(get_stddev(p), 2)}",
        f"min: {round(min(p), 2)}",
        f"max: {round(max(p), 2)}",
        f"sum: {round(sum(p), 2)}",
    ])


def read_log(filename, scheduler, fecScheme, slow_start_duration = 15):
    conf = RunConfig(scheduler, fecScheme)

    request_size = 0
    rtts = []
    owds = []

    with open(filename, newline='') as csvfile:
        reader = csv.DictReader(csvfile)
        for row in reader:
            try:
                request_size = int(row["SIZE"])
                rtt = int(row["RCV"]) - int(row["REQ"])
                owd = int(row["RCV"]) - int(row["SND"])
            except:
                continue

            rtts.append(rtt)
            owds.append(owd)

    return conf, RunInfo(request_size, rtts, owds)


def print_stats(allInfos):
    for conf, infos in allInfos.items():
        print(f"=== {conf.scheduler}, {conf.fec} ===")

        print("> population size")
        print(f"  {len(infos)}")

        print("> rtt")
        rtts = []
        for info in infos:
            rtts += info.rtts
        print(f"  {get_population_stats(rtts)}")

        print("> owd")
        owds = []
        for info in infos:
            owds += info.owds
        print(f"  {get_population_stats(owds)}")


def visualize_boxplot_rtt(allInfos):
    plt.figure(figsize=(8,5))
    sns.set(style="ticks", palette="pastel")

    data = {}
    for conf, infos in allInfos.items():
        key = f"{conf.fec} - {conf.scheduler}".upper()

        if key not in data:
            data[key] = []

        for info in infos:
            rtts_mean = get_mean(info.rtts)
            rtts_stddev = get_stddev(info.rtts)

            filtered_rtts = []
            for rtt in info.rtts:
                if get_z_score(rtt, rtts_mean, rtts_stddev) < 1:
                    filtered_rtts.append(rtt)

            data[key] += filtered_rtts

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.boxplot(palette=PALETTE, data=df)
    sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)
    sns.despine(offset=10, trim=True)

    ax.set(xlabel='', ylabel='Round Trip Time')
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    plt.savefig("vis-boxplot-rtt.png")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('-d', '--dir', type=str,
                                help='folder containing logs')
    parser.add_argument('-t', '--slow-start', type=int, default=15,
                                help='duration of slow start phase')
    args = parser.parse_args()

    allInfos = {}

    conf, info = read_log("delay-experiment-recordings/delay-client-8kb-ll.csv", "ll", "delay-rs")
    allInfos[conf] = [info]

    conf, info = read_log("delay-experiment-recordings/delay-client-8kb-s-iod.csv", "s-iod", "delay-rs")
    allInfos[conf] = [info]

    conf, info = read_log("delay-experiment-recordings/delay-client-8kb-s-edpf.csv", "s-edpf", "delay-rs")
    allInfos[conf] = [info]

    try:
        print_stats(allInfos)
    except:
        print("print_stats failed")

    visualize_boxplot_rtt(allInfos)
    print("creating boxplots (RTT)")
    try:
        visualize_boxplot_rtt(allInfos)
    except:
        print("boxplot failed")


if __name__ == '__main__':
    sys.exit(main())
