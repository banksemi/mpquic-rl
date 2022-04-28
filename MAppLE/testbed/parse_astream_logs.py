#!/usr/bin/env python3

import json
import math
import sys
import glob
import argparse
import os
from collections import namedtuple, defaultdict

import seaborn as sns
import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
from matplotlib.lines import Line2D
from matplotlib.ticker import MaxNLocator
import pandas

RunConfig = namedtuple("RunConfig", "scheduler fec")
RunInfo = namedtuple("RunInfo", "count total durations interrupted_segments interrupt_times bitrates segment_bitrates segment_download_times segment_filenames initial_buffering")

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


def z_filter(l, cutoff = 2.5):
    mean = get_mean(l)
    stddev = get_stddev(l)
    return list(filter(lambda x: get_z_score(x, mean, stddev) < cutoff, l))


def fixname(name):
    name = name[:3].replace("IOD", "R-IOD") + name[3:]
    name = name.replace("XOR4-1", "XOR 4")
    name = name.replace("XOR16-1", "XOR 16")
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


def read_log(filename, slow_start_duration = 15):
    with open(filename, 'rb') as fo:
        log = json.load(fo)

    conf = RunConfig(log['scheduler'], log['fecConfig'])

    total = 0.0
    start_time = log['playback_info']['start_time']
    initial_buffering = float(log['playback_info']['initial_buffering_duration'])
    count = 0
    durations = []
    interrupted_segments = []
    interrupt_times = []
    for event in log['playback_info']['interruptions']['events']:
        seg_no = event['segment_number']
        start = event['timeframe'][0]
        end = event['timeframe'][1]
        duration = end - start

        if start < start_time + slow_start_duration:
            # ignore first few seconds of stream
            continue

        # some interruptions are really short, ignore?
        if duration < 1e-4:
            continue

        # some, on the other hand, are unrealistically long. this points
        # towards a crash in the server and can be ignored
        if duration > 10:
            continue

        count += 1
        durations.append(duration)
        total += duration

        interrupted_segments.append(seg_no)
        interrupt_times.append({
            "start": start - start_time,
            "end": end - start_time,
            "duration": duration,
        })

    segment_filenames = [x[0] for x in log['segment_info']]
    segment_bitrates = [int(x[1]) for x in log['segment_info']]
    segment_download_times = [float(x[3]) for x in log['segment_info']]
    bitrates = set(segment_bitrates)

    return conf, RunInfo(count, total, durations, interrupted_segments,
            interrupt_times, bitrates, segment_bitrates,
            segment_download_times, segment_filenames, initial_buffering)


def print_stats(allInfos):
    for conf, infos in allInfos.items():
        print(f"=== {conf.scheduler}, {conf.fec} ===")

        print("> population size")
        print(f"  {len(infos)}")

        print("> count")
        counts = [x.count for x in infos]
        print(f"  {get_population_stats(counts)}")

        print("> total")
        totals = [x.total for x in infos]
        print(f"  {get_population_stats(totals)}")

        print("> bitrates")
        bitrates = []
        for info in infos:
            bitrates += info.segment_bitrates
        print(f"  {get_population_stats(bitrates)}")

        print("> bitrate switching (up)")
        bitrate_up = []
        for info in infos:
            count = 0
            for prev, current in zip(info.segment_bitrates[:-1], info.segment_bitrates[1:]):
                if prev < current:
                    count += 1
            bitrate_up.append(count)
        print(f"  {get_population_stats(bitrate_up)}")

        print("> bitrate switching (down)")
        bitrate_down = []
        for info in infos:
            count = 0
            for prev, current in zip(info.segment_bitrates[:-1], info.segment_bitrates[1:]):
                if prev > current:
                    count += 1
            bitrate_down.append(count)
        print(f"  {get_population_stats(bitrate_down)}")


def visualize_boxplot(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette=PALETTE_9)

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        data[key] = z_filter([x.count for x in infos])

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.boxplot(palette=PALETTE_9, data=df)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='# Interruptions')
    ax.set(ylim=(0, None))
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    plt.savefig("vis-boxplot." + FORMAT)


def visualize_boxplot_split(allInfos):
    data_a = {}
    data_b = {}

    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        data_a[key] = []
        data_b[key] = []

        for info in infos:
            count_a = 0
            count_b = 0

            for interrupt_time in info.interrupt_times:
                if interrupt_time["start"] < 100:
                    count_a+=1
                else:
                    count_b+=1

            data_a[key].append(count_a)
            data_b[key].append(count_b)

    # fill missing recordings with NaNs
    maxlen_a = max([len(data_a[k]) for k in data_a.keys()])
    maxlen_b = max([len(data_b[k]) for k in data_b.keys()])
    for k, v in data_a.items():
        data_a[k] = v + [float('nan')] * (maxlen_a - (len(v)))
    for k, v in data_b.items():
        data_b[k] = v + [float('nan')] * (maxlen_b - (len(v)))

    # draw A

    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette=PALETTE_9)

    df = pandas.DataFrame.from_dict(data_a)

    ax = sns.boxplot(palette=PALETTE_9, data=df)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='# Interruptions')
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    ax.set(ylim=(0, None))
    plt.savefig("vis-boxplot-split-a." + FORMAT)

    # draw B

    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette=PALETTE_9)

    df = pandas.DataFrame.from_dict(data_b)

    ax = sns.boxplot(palette=PALETTE_9, data=df)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='# Interruptions')
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    ax.set(ylim=(0, None))
    plt.savefig("vis-boxplot-split-b." + FORMAT)


def visualize_distplot_interrupts(allInfos):
    plt.figure(figsize=(6,5))
    sns.set(style="whitegrid", palette=PALETTE_9)

    data = {
        "config": [],
        "interrupted_segments": [],
    }
    configs = set()

    segments_count = 0

    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())
        configs.add(key)

        for info in infos:
            data["config"].extend([key]*len(info.interrupted_segments))
            data["interrupted_segments"].extend(info.interrupted_segments)
            segments_count = max(segments_count, len(info.segment_bitrates))

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = plt.gca()

    pal = sns.cubehelix_palette(10, rot=-.25, light=.7)
    g = sns.FacetGrid(df, row="config", hue="config", aspect=10, height=1, palette=pal)

    g.map(sns.kdeplot, "interrupted_segments", clip_on=False, shade=True, alpha=1, lw=1.5, bw=.2, clip=(0, segments_count))
    ##g.map(plt.axhline, y=0, lw=2, clip_on=False)

    # Set the subplots to overlap
    #g.fig.subplots_adjust(hspace=-.25)

    #g.set_titles("")
    g.set(yticks=[], xlabel='Segments')
    g.despine(bottom=True, left=True, right=True)

    plt.savefig("vis-dist-interrupts." + FORMAT)


def visualize_distplot_interrupts_cumulative(allInfos):
    plt.figure(figsize=(10,6))
    sns.set(style="ticks", palette=PALETTE_5)

    data = {}
    configs = set()
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())
        configs.add(key)

        data[key] = [x.count for x in infos]

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    kwargs = {"cumulative": True}
    patches = []
    for i, config in enumerate(configs):
        ax = sns.distplot(data[config], hist=False, kde_kws=kwargs)

        patches.append(mpatches.Patch(
            color=sns.color_palette()[i],
            label=config
        ))

        ax.set(xlabel='# Interruptions', ylabel='')

    plt.legend(handles=patches)
    plt.savefig("vis-dist-amount-cumulative." + FORMAT)


def visualize_boxplot_accumulated(allInfos, split=False):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette=PALETTE_9)

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        data[key] = [x.total for x in infos]

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.boxplot(palette=PALETTE_9, data=df)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='Accumulated Interruption Duration (s)')
    ax.set(ylim=(0, None))
    plt.savefig("vis-boxplot2." + FORMAT)


def visualize_boxplot_mean(allInfos, split=False):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette="pastel")

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        data[key] = []
        for x in infos:
            data[key] += x.durations
        data[key] = z_filter(data[key])


    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.boxplot(palette=PALETTE_9, data=df, showfliers=False)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='Interruption Duration (s)')
    ax.set(ylim=(0, None))
    plt.savefig("vis-boxplot3." + FORMAT)


def visualize_boxplot_mean_split(allInfos, split=False):
    data_a = {}
    data_b = {}

    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        data_a[key] = []
        data_b[key] = []

        for info in infos:
            durations_a = []
            durations_b = []

            for interrupt_time in info.interrupt_times:
                if interrupt_time["start"] < 100:
                    durations_a.append(interrupt_time["duration"])
                else:
                    durations_b.append(interrupt_time["duration"])

            data_a[key].extend(durations_a)
            data_b[key].extend(durations_b)

    # fill missing recordings with NaNs
    maxlen_a = max([len(data_a[k]) for k in data_a.keys()])
    maxlen_b = max([len(data_b[k]) for k in data_b.keys()])
    for k, v in data_a.items():
        data_a[k] = v + [float('nan')] * (maxlen_a - (len(v)))
    for k, v in data_b.items():
        data_b[k] = v + [float('nan')] * (maxlen_b - (len(v)))

    # draw A

    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette=PALETTE_9)

    df = pandas.DataFrame.from_dict(data_a)

    ax = sns.boxplot(palette=PALETTE_9, data=df)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='# Interruptions')
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    plt.savefig("vis-boxplot3-split-a." + FORMAT)

    # draw B

    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette=PALETTE_9)

    df = pandas.DataFrame.from_dict(data_b)

    ax = sns.boxplot(palette=PALETTE_9, data=df)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='# Interruptions')
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    plt.savefig("vis-boxplot3-split-b." + FORMAT)


def visualize_distplot_duration(allInfos):
    plt.figure(figsize=(10,10))
    sns.set(style="ticks", palette="pastel")

    data = {
        "config": [],
        "duration": [],
    }
    configs = set()

    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())
        configs.add(key)

        for info in infos:
            for duration in info.durations:
                    data["config"].append(key)
                    data["duration"].append(duration)

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = plt.gca()

    pal = sns.cubehelix_palette(10, rot=-.25, light=.7)
    g = sns.FacetGrid(df, row="config", hue="config", aspect=10, height=2, palette=pal)

    g.map(sns.kdeplot, "duration", clip_on=False, shade=True, alpha=1, lw=1.5, bw=.2, clip=(0, 0.2))
    g.map(sns.kdeplot, "duration", clip_on=False, color="w", lw=2, bw=.2, clip=(0, 0.2))
    g.map(plt.axhline, y=0, lw=2, clip_on=False)

    # Set the subplots to overlap
    #g.fig.subplots_adjust(hspace=-.25)

    g.despine(bottom=True, left=True)

    ax.set(xlabel='', ylabel='Interruption Duration (s)')
    ax.set(ylim=(0, None))
    plt.savefig("vis-dist-duration." + FORMAT)


def visualize_boxplot_initial_buffering(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette=PALETTE_9)

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        data[key] = []
        for x in infos:
            data[key].append(x.initial_buffering)

        data[key] = z_filter(data[key])

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.boxplot(palette=PALETTE_9, data=df)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='Initial Buffering Delay (s)')
    plt.savefig("vis-boxplot4." + FORMAT)


def visualize_boxplot_segments_until_highest_bitrate(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette="pastel")

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        data[key] = []
        for x in infos:
            segments = 0
            for bitrate in x.segment_bitrates:
                if bitrate < 10000000:
                    segments += 1
                else:
                    break

            data[key].append(segments)

    df = pandas.DataFrame.from_dict(data)

    ax = sns.violinplot(palette=PALETTE_9, data=df, cut=0)
    #sns.swarmplot(size=2, color="0.3", linewidth=0, data=df)

    ax.set(xlabel='', ylabel='# Segments until highest bitrate')
    plt.savefig("vis-boxplot5." + FORMAT)


def visualize_timeseries(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette="pastel")

    data = {
        "segment": [],
        "accumulated": [],
        "config": [],
    }

    max_segment_number = 0

    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        run_max_seg_no = 0

        for info in infos:
            if len(info.interrupted_segments) > 0 and max(info.interrupted_segments) > run_max_seg_no:
                run_max_seg_no = max(info.interrupted_segments)

        if run_max_seg_no > max_segment_number:
            max_segment_number = run_max_seg_no

        for run in infos:
            accum = 0
            for segment in range(run_max_seg_no + 1):
                if segment in run.interrupted_segments:
                    accum += 1
                data["segment"].append(segment)
                data["accumulated"].append(accum)
                data["config"].append(key)

    df = pandas.DataFrame.from_dict(data)
    ax = sns.lineplot(data=df, x="segment", y="accumulated", hue="config")

    ax.set(xlabel='Segment Number', ylabel='Accumulated Interruptions')
    #plt.yticks(range(1, max_interruptions + 1))
    plt.savefig("vis-interrupt-distribution." + FORMAT)


def visualize_timeseries_duration(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette="pastel")

    data = {
        "segment": [],
        "accumulated": [],
        "config": [],
    }

    max_segment_number = 0

    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        run_max_seg_no = 0

        for info in infos:
            if len(info.interrupted_segments) > 0 and max(info.interrupted_segments) > run_max_seg_no:
                run_max_seg_no = max(info.interrupted_segments)

        if run_max_seg_no > max_segment_number:
            max_segment_number = run_max_seg_no

        for run in infos:
            accum = 0
            durations = run.durations
            for segment in range(run_max_seg_no + 1):

                if segment in run.interrupted_segments:
                    accum += durations[0]
                    durations.pop(0)
                data["segment"].append(segment)
                data["accumulated"].append(accum)
                data["config"].append(key)

    df = pandas.DataFrame.from_dict(data)
    ax = sns.lineplot(data=df, x="segment", y="accumulated", hue="config")

    ax.set(xlabel='Segment Number', ylabel='Accumulated Duration (s)')
    #plt.yticks(range(1, max_interruptions + 1))
    plt.savefig("vis-interrupt-duration." + FORMAT)


def visualize_bitrate(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette=PALETTE_5)

    data = {
        'segment': [],
        'bitrate': [],
        'config': [],
    }

    bitrates = set()

    for conf, infos in allInfos.items():
        config = fixname(f"{conf.scheduler} - {conf.fec}".upper())

        for info in infos:
            for i, bitrate in enumerate(info.segment_bitrates):
                data['segment'].append(i)
                data['bitrate'].append(bitrate)
                data['config'].append(config)
            bitrates.update(info.segment_bitrates)

    df = pandas.DataFrame.from_dict(data)
    ax = sns.lineplot(data=df, x='segment', y='bitrate', hue='config',
                      palette='muted')

    ax.set(xlabel='Segment Number', ylabel='Bit Rate')
    plt.yticks(list(bitrates))
    ax.ticklabel_format(style='plain')
    plt.savefig("vis-abr." + FORMAT)


def visualize_bitrate_smoothed(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="ticks", palette=PALETTE_5)

    data = {
        'segment': [],
        'bitrate': [],
        'config': [],
    }

    bitrates = set()

    for conf, infos in allInfos.items():
        config = fixname(f"{conf.scheduler} - {conf.fec}".upper())
        for info in infos:
            for i in range(len(info.segment_bitrates)):
                if i < 2 or i >= len(info.segment_bitrates) - 2:
                    continue

                neighbour_bitrates = [
                    info.segment_bitrates[i-2]//10**6,
                    info.segment_bitrates[i-1]//10**6,
                    info.segment_bitrates[i]//10**6,
                    info.segment_bitrates[i+1]//10**6,
                    info.segment_bitrates[i+2]//10**6,
                ]
                data['segment'].append(i)
                data['bitrate'].append(get_mean(neighbour_bitrates))
                data['config'].append(config)
                bitrates.add(info.segment_bitrates[i]//10**6)

    df = pandas.DataFrame.from_dict(data)
    ax = sns.lineplot(data=df, x='segment', y='bitrate', hue='config',
                      palette='muted')

    ax.set(xlabel='Segment Number', ylabel='Bit Rate (Mbit/s)')
    plt.yticks(list(bitrates))
    ax.ticklabel_format(style='plain')
    plt.savefig("vis-abr-smooth." + FORMAT)


def visualize_download_time(allInfos):
    data = {
        'segment': [],
        'download_time': [],
        'config': [],
        'config_linebreak': [],
    }

    for conf, infos in allInfos.items():
        config = fixname(f"{conf.scheduler} - {conf.fec}".upper())
        config_linebreak = fixname(f"{conf.scheduler}\n{conf.fec}".upper())
        for info in infos:
            mean = get_mean(info.segment_download_times)
            stddev = get_mean(info.segment_download_times)

            for i, download_time in enumerate(info.segment_download_times):
                if get_z_score(download_time, mean, stddev) < 1:
                    data['segment'].append(i)
                    data['download_time'].append(download_time)
                    data['config'].append(config)
                    data['config_linebreak'].append(config_linebreak)

    def lineplot():
        plt.figure(figsize=(15,8))
        sns.set(style="ticks", palette="pastel")

        df = pandas.DataFrame.from_dict(data)
        ax = sns.lineplot(data=df, x='segment', y='download_time', hue='config')

        ax.set(xlabel='Segment Number', ylabel='Download Time (s)')
        ax.ticklabel_format(style='plain')
        plt.savefig("vis-dload-time." + FORMAT)

    def boxplot():
        plt.figure(figsize=FIGSIZE.WIDE_M)
        sns.set(style="whitegrid", palette=PALETTE_9)

        df = pandas.DataFrame.from_dict(data)
        ax = sns.boxplot(data=df, x='config_linebreak', y='download_time', showfliers=False)
        sns.despine(offset=10, trim=True)

        ax.set(xlabel='', ylabel='Segment Download Time')
        plt.savefig("vis-dload-dist." + FORMAT)

    def cdf(complementary=False):
        plt.figure(figsize=FIGSIZE.BOX_M)
        sns.set(style="ticks", palette=PALETTE_5)

        data = {}
        configs = list()
        for conf, infos in allInfos.items():
            key = f"{conf.scheduler} - {conf.fec}".upper()
            configs.append(key)

            data[key] = []
            for info in infos:
                data[key] += filter(lambda x: x < 0.4, info.segment_download_times)

            data[key] = z_filter(data[key], 1)

        # fill missing recordings with NaNs
        maxlen = max([len(data[k]) for k in data.keys()])
        for k, v in data.items():
            data[k] = v + [float('nan')] * (maxlen - (len(v)))

        patches = []
        for i, config in enumerate(configs):
            color = sns.color_palette()[i]
            linestyle = "-"

            if "S-IOD" in config:
                color = "orange"
                linestyle = "-"
            elif "IOD" in config:
                color = "goldenrod"
                linestyle = "--"
            elif "S-EDPF" in config:
                color = "royalblue"
                linestyle = "-"
            elif "NONE" in config:
                color = "black"
                linestyle = ":"
            elif "LL" in config:
                color = "black"
                linestyle = "-"

            kwargs = {
                "cumulative": True,
                "color": color,
                "linestyle": linestyle,
            }

            #ax = sns.distplot(data[config], hist=False, kde_kws=kwargs)
            ax = sns.ecdfplot(data[config], complementary=complementary,
                    color=color, linestyle=linestyle)

            patches.append(Line2D([0], [0],
                color=color,
                label=fixname(config),
                linestyle=linestyle,
                lw=2,
            ))

            ax.set(xlabel='Download Time (s)', ylabel='')

        plt.legend(handles=patches)
        if complementary:
            plt.savefig("vis-dload-ccdf." + FORMAT)
        else:
            plt.savefig("vis-dload-cdf." + FORMAT)


    #lineplot()
    boxplot()
    cdf(False)
    cdf(True)


def visualize_download_relation(allInfos, video_dir):
    data = {
        'download_time': [],
        'segment_size': [],
        'config': [],
    }

    for conf, infos in allInfos.items():
        config = f"{conf.scheduler} - {conf.fec}".upper()
        for info in infos:
            mean = get_mean(info.segment_download_times)
            stddev = get_mean(info.segment_download_times)

            for download_time, fname in zip(info.segment_download_times, info.segment_filenames):
                if get_z_score(download_time, mean, stddev) < 2.5:
                    segment_size = os.stat(os.path.join(video_dir, fname)).st_size

                    if segment_size < 10000:
                        continue

                    data['download_time'].append(download_time)
                    data['segment_size'].append(segment_size)
                    data['config'].append(config)

    df = pandas.DataFrame.from_dict(data)

    palette = sns.color_palette("rocket_r")

    sns.relplot(data=df, x="segment_size", y="download_time", hue="config",
                kind="line")

    plt.savefig("vis-dload-relplot." + FORMAT)


def print_goodput(allInfos, video_dir):
    data = {}

    for conf, infos in allInfos.items():
        config = f"{conf.scheduler} - {conf.fec}".upper()

        if config not in data:
            data[config] = []

        for info in infos:
            for download_time, fname in zip(info.segment_download_times, info.segment_filenames):
                segment_size = os.stat(os.path.join(video_dir, fname)).st_size

                if segment_size < 10000:
                    continue

                data[config].append(segment_size/download_time)

    for conf in data.keys():
        print(f"=== {conf} ===")
        print(get_population_stats(data[conf]))


def visualize_inter_interrupt_time(allInfos):
    plt.figure(figsize=FIGSIZE.WIDE_M)
    sns.set(style="whitegrid", palette=PALETTE_9)

    data = {}
    for conf, infos in allInfos.items():
        key = fixname(f"{conf.scheduler}\n{conf.fec}".upper())

        times = []

        for info in infos:
            for i in range(1, len(info.interrupt_times)):
                t = info.interrupt_times[i]["start"]-info.interrupt_times[i-1]["end"]
                times.append(t)

        data[key] = times

    for key in data.keys():
        data[key] = z_filter(data[key])

    # fill missing recordings with NaNs
    maxlen = max([len(data[k]) for k in data.keys()])
    for k, v in data.items():
        data[k] = v + [float('nan')] * (maxlen - (len(v)))

    df = pandas.DataFrame.from_dict(data)

    ax = sns.boxenplot(data=df)

    ax.set(xlabel='', ylabel='Inter-Interruption Time (s)')
    ax.set(ylim=(0, None))
    ax.yaxis.set_major_locator(MaxNLocator(integer=True))
    plt.savefig("vis-time-inter." + FORMAT)


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
    parser.add_argument('--video-dir', type=str,
                                help='directory containing the DASH stream')
    parser.add_argument('--split', default=False, action="store_true",
                                help='split boxplot (for ABR test)')
    args = parser.parse_args()

    globals()["FORMAT"] = args.format

    allInfos = {}

    filenameGlob = os.path.join(args.dir, 'ASTREAM*')
    for filename in sorted(glob.glob(filenameGlob)):
        conf, info = read_log(filename, args.slow_start)

        if not conf.fec in args.fec_schemes:
            continue

        if not conf.scheduler in args.schedulers:
            continue

        if conf in allInfos:
            allInfos[conf].append(info)
        else:
            allInfos[conf] = [info]

    try:
        print_stats(allInfos)
    except:
        print("print_stats failed")

    if FORMAT == "none":
        print_goodput(allInfos)
        return 0

    print("creating boxplots (rebuffering frequency)")
    try:
        if args.split:
            visualize_boxplot_split(allInfos)
        else:
            visualize_boxplot(allInfos)
    except:
        print("boxplot failed")

    print("creating boxplots (accumulated rebuffering duration)")
    try:
        visualize_boxplot_accumulated(allInfos)
    except:
        print("boxplot failed")

    print("creating boxplots (individual rebuffering duration)")
    try:
        if args.split:
            visualize_boxplot_mean_split(allInfos)
        else:
            visualize_boxplot_mean(allInfos)
    except:
        print("boxplot failed")

    print("creating boxplots (initial buffering duration)")
    try:
        visualize_boxplot_initial_buffering(allInfos)
    except:
        print("boxplot failed")

    print("creating boxplots (time until highest bitrate)")
    try:
        visualize_boxplot_segments_until_highest_bitrate(allInfos)
    except:
        print("boxplot failed")

    print("visualizing timeseries")
    try:
        visualize_timeseries(allInfos)
    except:
        print("visualizing interrupt timeseries failed")

    print("visualizing timeseries duration")
    try:
        visualize_timeseries_duration(allInfos)
    except:
        print("visualizing interrupt timeseries duration failed")

    print("visualizing interrupts ccdf")
    try:
        visualize_distplot_interrupts_cumulative(allInfos)
    except:
        print("visualizing interrupts ccdf failed")

    print("visualizing interrupts distribution")
    try:
        visualize_distplot_interrupts(allInfos)
    except:
        print("visualizing interrupts distribution failed")

#    print("visualizing bitrate")
#    try:
#        visualize_bitrate(allInfos)
#    except:
#        print("visualizing adaptive bitrate timeseries failed")

    print("visualizing bitrate (smoothed)")
    try:
        visualize_bitrate_smoothed(allInfos)
    except:
        print("visualizing smoothed adaptive bitrate timeseries failed")

    print("visualizing download times")
    try:
        visualize_download_time(allInfos)
    except:
        print("visualizing download times failed")

    print("visualizing download time/file size relation")
    try:
        visualize_download_relation(allInfos, args.video_dir)
    except:
        print("visualizing download time/file size relation failed")



if __name__ == '__main__':
    sys.exit(main())
