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
    else:
        allFecConfigs = []
        filenames = glob.glob(filenameGlobFecConfigs)
        for i, filename in enumerate(filenames):
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
            print(f"{round(i/len(filenames)*100)}%", end="\r")
        print("")

    return RunInfo(allGaps, fecDelays, recoveries, losses, allFecConfigs)


def visualize_fec_delays(allInfos):
    plt.figure(figsize=(15,8))
    sns.set(style="ticks", palette="pastel")

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        data[key] = []
        for x in infos:
            data[key] += x.fecDelays

    # remove outliers
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())
        if len(data[key]) == 0:
            continue

        mean = get_mean(data[key])
        stddev = get_stddev(data[key])
        ndata = []

        for x in data[key]:
            z = get_z_score(x, mean, stddev)
            if z < 0.75:
                ndata.append(x * 10**-6)

        data[key] = ndata

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.boxplot(palette=PALETTE, data=df)
    #sns.despine(offset=10, trim=True)

    ax.set(xlabel='', ylabel='FEC Recovery Delay (ms)')
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    plt.savefig("vis-fec-delay." + FORMAT)


def visualize_gaps(allInfos):
    plt.figure(figsize=(15,8))
    sns.set(style="ticks", palette="pastel")

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        data[key] = []
        for x in infos:
            data[key] += x.gaps

    # remove outliers
#    for conf, infos in allInfos.items():
#        key = f"{conf.scheduler} - {conf.fec}".upper()
#        if len(data[key]) == 0:
#            continue
#
#        mean = get_mean(data[key])
#        stddev = get_stddev(data[key])
#        ndata = []
#
#        for x in data[key]:
#            z = get_z_score(x, mean, stddev)
#            if z < 2.5:
#                ndata.append(x)
#
#        data[key] = ndata

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.swarmplot(palette=PALETTE, data=df)
    sns.despine(offset=10, trim=True)

    ax.set(xlabel='', ylabel='Receive Buffer Size')
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    plt.savefig("vis-gaps." + FORMAT)


def visualize_gaps_timeline(allInfos):
    plt.figure(figsize=(15,8))
    sns.set(style="ticks", palette="pastel")

    data = {
        'arrival': [],
        'gaps': [],
        'config': [],
    }

    for conf, infos in allInfos.items():
        config = fixname(f"{conf.scheduler} - {conf.fec}".upper())
        for info in infos:
            for i in range(1, len(info.gaps)):
                gaps = info.gaps[i-1]["gaps"]
                t = info.gaps[i]["timestamp"]-info.gaps[i-1]["timestamp"]

                data['arrival'].append(i)
                data['gaps'].append(gaps*t)
                data['config'].append(config)

                print("working " + ['|', '/', '-', '\\'][(i%4)], end="\r")
    print("")

    df = pandas.DataFrame.from_dict(data)
    print("drawing...")
    ax = sns.lineplot(data=df, x='arrival', y='gaps', hue='config')

    ax.set(xlabel='Arrivals', ylabel='Receive Buffer Size')
    #plt.yticks(list(bitrates))
    ax.ticklabel_format(style='plain')
    plt.savefig("vis-gaps." + FORMAT)


def visualize_gaps_boxplot(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette="pastel")

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        data[key] = []
        for info in infos:
            for run in info.gaps:
                data[key] += [x["gaps"] for x in run]
        if len(data[key]) == 0:
            del data[key]

    # remove outliers
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())
        if key not in data or len(data[key]) == 0:
            continue

        mean = get_mean(data[key])
        stddev = get_stddev(data[key])
        ndata = []

        for x in data[key]:
            z = get_z_score(x, mean, stddev)
            if z < 2.5:
                ndata.append(x)

        data[key] = ndata

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.boxplot(palette=PALETTE_9, data=df)
    #sns.despine(offset=10, trim=True)

    ax.set(xlabel='', ylabel='# Reordered Packets')
    ax.set(ylim=(0, None))
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    plt.savefig("vis-gaps-boxplot." + FORMAT)


def visualize_reordering_timeseries(allInfos):
    plt.figure(figsize=(15,8))
    sns.set(style="ticks", palette="pastel")

    data = {
        "packet": [],
        "accumulated": [],
        "config": [],
        "time": [],
    }

    max_segment_number = 0

    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        for info in infos:
            for run in info.gaps:
                if len(run) < 10:
                    continue

                accum = 0
                bucket = 0
                bucket_accum = 0
                bucket_start_time = 0
                start_time = run[0]["timestamp"]

                gap_start_times = []
                current_stream = 0

                for i in range(1, len(run)):
                    if run[i]["timestamp"] - start_time > 160 * 10**9:
                        # beyond 160s
                        break

                    t = run[i]["timestamp"] - run[i-1]["timestamp"]
                    if t < 0 or t > 0.1*10**9:
                        # when switching streams, timestamps may vary too strongly
                        continue

#                    if run[i]["stream"] > run[i-1]["stream"]:
#                        gap_start_times = []
#                        bucket_accum = 0
#                        bucket_start_time = run[i]["timestamp"]

                    if run[i-1]["gaps"] < run[i]["gaps"]:
                        gap_start = run[i]["timestamp"]
                        gap_start_times.append(gap_start)
                        bucket_accum+=1
                    elif run[i-1]["gaps"] > run[i]["gaps"]:
                        if len(gap_start_times) < 1:
                            continue
                        gap_start = gap_start_times[0]
                        del gap_start_times[0]
                        gap_end = run[i]["timestamp"]

                    #bucket_accum += run[i]["gaps"]

                    if run[i]["timestamp"] > bucket_start_time + 10**9:
                        accum += bucket_accum
                        #accum += bucket
                        data["packet"].append(i)
                        data["time"].append((run[i]["timestamp"] - start_time) // 10**9)
                        #data["accumulated"].append(accum)
                        data["accumulated"].append(accum)
                        data["config"].append(key)

                        bucket = 0
                        bucket_accum = 0
                        bucket_start_time = run[i]["timestamp"]

    df = pandas.DataFrame.from_dict(data)
    ax = sns.lineplot(data=df, x="time", y="accumulated", hue="config")

    ax.set(xlabel='Time (s)', ylabel='# Gaps')
    ax.set(xlim=(0, 160))
    #plt.yticks(range(1, max_interruptions + 1))
    plt.savefig("vis-gaps-time." + FORMAT)


def visualize_reordering_accumulated(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette=PALETTE_9)

    data = {}

    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        for info in infos:
            for run in info.gaps:
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


def visualize_coding_rate(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette="pastel")

    data = {
        "timestamp": [],
        "rate": [],
        "config": [],
    }

    for conf, infos in allInfos.items():
        config = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        for info in infos:
            for i, fecConfig in enumerate(info.fecConfigs):
                if len(fecConfig) == 0:
                    continue

                fecConfig = fecConfig[:-1] # XXX last row is often malformatted

                startTime = fecConfig[0].timestamp
                endTime = fecConfig[-1].timestamp

                if endTime - startTime < 120 * 10**9:
                    # stream must have stopped too early
                    continue

                if endTime - startTime > 200 * 10**9:
                    # stream went on for too long
                    # we have enough data to ignore those few outliers
                    continue

                rates_begin_timestamp = -1
                rates = []

                for pkt in fecConfig:
                    timestamp = pkt.timestamp - startTime

                    if timestamp // 10**9 > 150:
                        continue

                    rate = 0.0
                    if "xor" in conf.fec:
                        rate = 1 / pkt.nSourceSymbols
                    elif "rs" in conf.fec:
                        rate = pkt.nRepairSymbols / pkt.nSourceSymbols
                    elif "rlc" in conf.fec:
                        rate = pkt.nSourceSymbols / (pkt.windowStepSize*pkt.nSourceSymbols)

                    rates.append(rate)
                    if rates_begin_timestamp < timestamp // 10**9:
                        data["timestamp"].append(timestamp // 10**9)
                        data["rate"].append(get_mean(rates))
                        data["config"].append(config)

                        rates = []
                        rates_begin_timestamp = timestamp // 10**9

                print("working " + ['|', '/', '-', '\\'][(i%4)], end="\r")

    print("")

    df = pandas.DataFrame.from_dict(data)
    print("drawing...")
    ax = sns.lineplot(data=df, x='timestamp', y='rate', hue='config')

    ax.set(xlabel='Time (s)', ylabel='Redundancy Rate')
    ax.ticklabel_format(style='plain')
    ax.set(xlim=(0,150), ylim=(0, 0.5))
    plt.savefig("vis-fec-config." + FORMAT)


def visualize_losses_recoveries_kde(allInfos):
    plt.figure(figsize=(10,6))
    sns.set(style="ticks", palette="pastel")

    losses = {}
    recoveries = {}

    max_y = 0

    for conf, infos in allInfos.items():
        config = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        nlosses = []
        nrecoveries = []

        for info in infos:
            nlosses.extend(info.losses)
            nrecoveries.extend(info.recoveries)

            if len(info.recoveries) > 0:
                max_y = max(max_y, max(info.recoveries))

        if len(nlosses) == 0 or len(nrecoveries) == 0:
            continue

        nlosses, nrecoveries = z_filter_2d(
            nlosses,
            nrecoveries,
        )

        if config not in losses:
            losses[config] = []

        if config not in recoveries:
            recoveries[config] = []

        losses[config].extend(nlosses)
        recoveries[config].extend(nrecoveries)

    cmaps = ["Blues", "Oranges", "Greens", "Reds"]
    patches = []

    for i, conf in enumerate(losses.keys()):
#        sns.kdeplot(
#            x=losses[conf],
#            y=recoveries[conf],
#            label=conf,
#            cmap=cmaps[i],
#            shade=False,
#            cut=0,
#        )

        sns.kdeplot(
            x=losses[conf],
            y=recoveries[conf],
            label=conf,
            cmap=cmaps[i],
            shade=True,
            cut=0,
            thresh=0.5,
            levels=4,
            alpha=0.5,
        )

        patches.append(mpatches.Patch(
            color=sns.color_palette(cmaps[i])[-2],
            label=conf
        ))

    ax = sns.lineplot(
        data=pandas.DataFrame.from_dict({
            "x": [0, max_y],
            "y": [0, max_y],
        }),
        x="x",
        y="y",
        color="black",
        linestyle=":",
        alpha=0.25,
    )

    ax.set(xlabel='# Lost Packets', ylabel='# Recovered Packets')
    ax.set_xlim(left=0)
    ax.set_ylim(bottom=0)

    plt.legend(handles=patches)

    plt.savefig("vis-lrr-kde." + FORMAT)


def visualize_losses_recoveries_strip(allInfos):
    plt.figure(figsize=(15,6))
    sns.set(style="ticks", palette="pastel")

    data = {
        "value": [],
        "type": [],
        "config": [],
    }

    configs = set()

    for conf, infos in allInfos.items():
        config = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        configs.add(config)

        losses = []
        recoveries = []

        for info in infos:
            losses.extend(info.losses)
            recoveries.extend(info.recoveries)

        #losses, recoveries = z_filter_2d(losses, recoveries)

        data["value"].extend(losses)
        data["config"].extend([config] * len(losses))
        data["type"].extend(["losses"] * len(losses))

        data["value"].extend(recoveries)
        data["config"].extend([config] * len(recoveries))
        data["type"].extend(["recoveries"] * len(recoveries))


    df = pandas.DataFrame.from_dict(data)

    ax = sns.stripplot(x="value", y="config", hue="type", data=df,
                       dodge=True, alpha=.75, zorder=1)
    sns.pointplot(x="value", y="config", hue="type", data=df,
                  dodge=0.5, join=False, palette="dark", markers="d",
                  scale=1, ci=None)

    ax.set(xlabel='# Packets')

    #plt.legend(handles=patches)

    plt.savefig("vis-lrr-strip." + FORMAT)


def print_lost_recovered_ratio(allInfos):
    data = {}

    for conf, infos in allInfos.items():
        config = fixname(f"{conf.scheduler} - {conf.fec}".upper())
        print("=== " + config + " ===")

        data[config] = []

        for info in infos:
            for losses, recoveries in zip(info.losses, info.recoveries):
                # we only want IRRECOVERABLE losses here
                # "losses" includes also those we successfully recovered from
                # so we can simply make this subtraction here
                data[config].append((losses - recoveries)/recoveries)

        print("> lrr")
        print(f"  {get_population_stats(data[config])}")


def print_stats(allInfos):
    for conf, infos in allInfos.items():
        print(f"=== {conf.scheduler}, {conf.fec} ===")

        fecDelays = []
        for x in infos:
            fecDelays += x.fecDelays
        if len(fecDelays) > 0:
            print("> fecDelays")
            print(f"  {get_population_stats(fecDelays)}")

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

    allInfos = {}

    print("loading...")
    for fecScheme in args.fec_schemes:
        for scheduler in args.schedulers:
            conf = RunConfig(scheduler, fecScheme)
            info = read_log(args.dir, conf, args.slow_start, args.server)

            if conf in allInfos:
                allInfos[conf].append(info)
            else:
                allInfos[conf] = [info]

    print("printing statistics")
    try:
        print_stats(allInfos)
    except:
        print("\tFAILED!")

    if args.server:
        print("visualizing coding rate")
        visualize_coding_rate(allInfos)
    else:
        #print("visualizing LRR STRIP")
        #visualize_losses_recoveries_strip(allInfos)

        #print("visualizing LRR KDE")
        #visualize_losses_recoveries_kde(allInfos)

        print("visualizing receive buffer accumulated")
        visualize_reordering_accumulated(allInfos)

        print_lost_recovered_ratio(allInfos)

        #print("visualizing receive buffer evolution")
        #visualize_reordering_timeseries(allInfos)

        #print("visualizing receive buffer evolution (boxplot)")
        #visualize_gaps_boxplot(allInfos)

        #print("visualizing fec delays")
        #visualize_fec_delays(allInfos)

        #print("visualizing receive buffer evolution")
        #visualize_gaps(allInfos)


if __name__ == '__main__':
    sys.exit(main())
