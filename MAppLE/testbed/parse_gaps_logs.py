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
RunInfo = namedtuple("RunInfo", "gaps fecDelays recoveries losses fecConfigs")

FECConfig = namedtuple("FECConfig", "nSourceSymbols nRepairSymbols windowStepSize timestamp")
Packet = namedtuple("Packet", "size destination fec timestamp")

PALETTE = "muted"

PALETTE_5 = sns.color_palette("muted")
PALETTE_9 = sns.color_palette("muted")
PALETTE_9[4:9] = PALETTE_9[:5]

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


def read_log(dir, conf, slow_start_duration = 15, server = False):
    filenameGlobFec = f'{dir}/{conf.scheduler}_{conf.fec}*_fec.csv'
    filenameGlobGaps = f'{dir}/{conf.scheduler}_{conf.fec}*_gaps.csv'
    filenameGlobFecConfigs = f'{dir}/{conf.scheduler}_{conf.fec}*_fecConfig.csv'

    fecDelays = []
    recoveries = []
    losses = []
    allGaps = []

    if not server:
        for filename in glob.glob(filenameGlobFec):
            runRecoveries = 0
            fecStarts = {}
            startTime = None
            with open(filename, newline='') as csvfile:
                reader = csv.DictReader(csvfile)
                for row in reader:
                    try:
                        fecBlock = int(row['fecBlock'])
                        event = row['event']
                        timestamp = int(row['timestamp'])
                    except:
                        continue

                    if startTime is None:
                        startTime = timestamp

                    if timestamp - startTime < slow_start_duration * 10**9:
                        continue

                    if event == 'started' and fecBlock not in fecStarts:
                        fecStarts[fecBlock] = timestamp
                    elif event == 'recovered' and fecBlock in fecStarts:
                        runRecoveries += 1
                        delay = (timestamp - fecStarts[fecBlock])
                        fecDelays.append(delay)
                        del fecStarts[fecBlock]

            if runRecoveries > 10:
                # anything below this number is unrealistic and points towards
                # a fault in the software (crashed, etc.)

                recoveries.append(runRecoveries)
                # all values still in the "fecStarts" dict represent fecGroups which
                # have never received the recovery packet
                losses.append(len(fecStarts))

        for filename in glob.glob(filenameGlobGaps):
            print(filename)
            runGaps = []
            with open(filename, newline='') as csvfile:
                reader = csv.DictReader(csvfile)
                for row in reader:
                    try:
                        streamId = int(row['streamID'])
                        gaps = int(row['gaps'])
                        timestamp = int(row['timestamp'])
                    except:
                        continue
                    runGaps.append({
                        "gaps": gaps,
                        "timestamp": timestamp,
                        "stream": streamId,
                    })
            allGaps.append(runGaps)

    allFecConfigs = []
    for filename in glob.glob(filenameGlobFecConfigs):
        fecConfigs = []
        with open(filename, newline='') as csvfile:
            reader = csv.DictReader(csvfile)
            for row in reader:
                try:
                    nSourceSymbols = int(row['source'])
                    nRepairSymbols = int(row['repair'])
                    windowStepSize = int(row['windowStep'])
                    timestamp = int(row['timestamp'])
                except:
                    continue
                fecConfigs.append(FECConfig(
                    nSourceSymbols,
                    nRepairSymbols,
                    windowStepSize,
                    timestamp,
                ))

        allFecConfigs.append(fecConfigs)

    return RunInfo(allGaps, fecDelays, recoveries, losses, allFecConfigs)


def visualize_reordering_accumulated(dir, confs, slow_start_duration = 15):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette=PALETTE_9)

    data = {}

    for conf in confs:
        filenameGlobGaps = f'{dir}/{conf.scheduler}_{conf.fec}*_gaps.csv'
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        for filename in glob.glob(filenameGlobGaps):
            print(filename)
            run = []
            with open(filename, newline='') as csvfile:
                reader = csv.DictReader(csvfile)
                for row in reader:
                    try:
                        streamId = int(row['streamID'])
                        gaps = int(row['gaps'])
                        timestamp = int(row['timestamp'])
                    except:
                        continue
                    run.append({
                        "gaps": gaps,
                        "timestamp": timestamp,
                        "stream": streamId,
                    })

            if len(run) < 10:
                continue

            accum = 0
            start_time = run[0]["timestamp"]

            current_stream = 0

            for i in range(1, len(run)):
                if run[i]["timestamp"] - start_time > 160 * 10**9:
                    # beyond 160s
                    break

                t = run[i]["timestamp"] - run[i-1]["timestamp"]
                if t < 0 or t > 0.1*10**9:
                    # when switching streams, timestamps may vary too strongly
                    continue

                if run[i-1]["gaps"] < run[i]["gaps"]:
                    accum+=1

            if key not in data:
                data[key] = [accum]
            else:
                data[key].append(accum)

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)
    ax = sns.barplot(data=df)

    ax.set(ylabel='# Gaps')
    #plt.yticks(range(1, max_interruptions + 1))
    plt.savefig("vis-gaps-bars." + FORMAT)


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
    args = parser.parse_args()

    globals()["FORMAT"] = args.format

    confs = []
    for fecScheme in args.fec_schemes:
        for scheduler in args.schedulers:
            confs.append(RunConfig(scheduler, fecScheme))

    visualize_reordering_accumulated(args.dir, confs, args.slow_start)


if __name__ == '__main__':
    sys.exit(main())
