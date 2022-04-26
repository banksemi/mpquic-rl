import argparse
import os
import shutil
import sqlite3

import matplotlib
import numpy as np
import matplotlib.pyplot as plt
from matplotlib.ticker import FuncFormatter


parser = argparse.ArgumentParser()
parser.add_argument("-f", help="input filename")
parser.add_argument("-fsecondtest", help="input filename for the second test", default=None)
parser.add_argument("-t", help="output file", default="")
parser.add_argument("-m", help='plot method: can be "CDF", "uni", uni_favorable_path or "scatter"', default="CDF")
parser.add_argument("--transform", help='transform method: can be "none" "ratio" or "difference"', default="none")
parser.add_argument("--filesize", type=str, help='file size for which the data must be looked at', default=None)
parser.add_argument("--namefirsttest", help="represents the name of the first test", default=None)
parser.add_argument("--namesecondtest", help="represents the name of the second test", default=None)
parser.add_argument("--gemodel", action="store_true", default=False)
parser.add_argument("--xlabel", type=str, default=None)
parser.add_argument("--legend1", type=str, default=None)
parser.add_argument("--legend2", type=str, default=None)
parser.add_argument("--legends", type=str, default=None)
parser.add_argument("-fthirdtest", help="input filename for the third test", default=None)
parser.add_argument("--namethirdtest", help="represents the name of the third test", default=None)

args = parser.parse_args()

files = args.f.split(",")
files2 = args.fsecondtest.split(",") if args.fsecondtest is not None else [None]*len(files)
files3 = args.fthirdtest.split(",") if args.fthirdtest is not None else [None]*len(files)
name1 = args.namefirsttest.split(",") if args.namefirsttest is not None else [None]*len(files)
name2 = args.namesecondtest.split(",") if args.namesecondtest is not None else [None]*len(files)
name3 = args.namethirdtest.split(",") if args.namethirdtest is not None else [None]*len(files)
legends = args.legends.split(",") if args.legends is not None else None


print(list(enumerate(zip(files, files2, name1, name2))))
plt.figure(figsize=(4, 2), dpi=300)
for i, (filename, filename2, filename3, METHOD_1, METHOD_2, METHOD_3) in enumerate(zip(files, files2, files3, name1, name2, name3)):
    print("LENFILES=", len(files))
    sp = plt.subplot(1, len(files), i+1)
    transform = args.transform
    plot_method = args.m

    METHODS = [METHOD_1, METHOD_2]

    PARAMS = [TIME_AGGRESSIVE, TIME_DEFAULT, DELAY_0_IN, DELAY_0_OUT, DELAY_1_IN, DELAY_1_OUT, LAST_IP] = list(range(7))

    transforms = {
        "none": lambda x: x,
        "difference": lambda x: [x[0]-x[1]],
        "ratio": lambda x: [x[0]/x[1]]
    }

    transforms_output = {
        "none": 3,
        "difference": 1,
        "ratio": 1
    }

    transforms_labels = {
        "none": METHODS,
        "difference": ["time_%s - time_%s" % tuple(METHODS)],
        "ratio": ["time_%s/time_%s" % tuple(METHODS)]
    }

    transform_unit = {
        "none": "time (ms)",
        "difference": "time_%s - time_%s (ms)" % tuple(METHODS),
        "ratio": "time_%s/time_%s" % tuple(METHODS)
    }

    colors = ["darkblue", "hotpink", "green", "grey"]
    lines = ["--", "-", "-.", "dotted"]

    linewidth = 2
    marker = "."
    mew = 0.1
    markersize = 0

    idx = i

    def plot_cdf(x_axes, y_axis, transform, color=None, label=None, linestyle=None):
        print("LABEL = ", label)
        if idx == 0:
            plt.ylabel(args.m)
        else:
            plt.ylabel(" ")
        plt.xlabel("%s" % transform_unit[transform] if args.xlabel is None else args.xlabel)
        if transforms_output[transform] > 1 or color is None or label is None:
            for i in range(transforms_output[transform]):
                if type(label) is list:
                    l = label[i]
                else:
                    l = label
                print(transform, x_axes[i])
                plt.plot(x_axes[i], y_axis, color=colors[i], linewidth=linewidth, marker=marker, linestyle="-" if linestyle is None else linestyle,
                         markeredgewidth=mew, label=transforms_labels[transform][i] if l is None else l, ms=markersize)
        else:
            plt.plot(x_axes[0], y_axis, color=color, linewidth=linewidth, marker=marker, linestyle="-" if linestyle is None else linestyle,
                     markeredgewidth=mew, label=label, ms=markersize)



    def plot_json(file, color=None, label=None, linestyle=None):
        import json
        with open(file) as f:
            l = sorted(json.loads(f.read()))
            x = [elem[0]/1000000 for elem in l]
            y = [elem[1] for elem in l]
            plt.ylabel("cwin (bytes)")
            plt.xlabel("time (ms)")
            plt.plot(x, y, color="darkblue" if color is None else color, linewidth=0.25, marker=marker, linestyle="-" if linestyle is None else linestyle,
                     markeredgewidth=mew, ms=markersize, label=label)


    plt.rcParams["toolbar"] = "toolmanager"
    plt.tight_layout()
    plt.grid(True)

    if filename[-5:] == ".json":
        plot_json(filename, label=args.legend1)
        if filename2 is not None:
            plot_json(filename2, color="hotpink", linestyle="-", label=args.legend2)
    else:
        conn = sqlite3.connect(filename)

        c = conn.cursor()

        conn2 = None
        c2 = None
        c3 = None

        if args.fsecondtest is not None:
            # try:
            conn2 = sqlite3.connect(filename2)
            # except sqlite3.OperationalError:
            #     name = args.fsecondtest
            #     args.fsecondtest = ".tmp_db.db"
            #     shutil.copy(name, args.fsecondtest)
            #     conn2 = sqlite3.connect(args.fsecondtest)
            c2 = conn2.cursor()



            if args.fthirdtest is not None:
                # try:
                conn3 = sqlite3.connect(filename3)
                # except sqlite3.OperationalError:
                #     name = args.fsecondtest
                #     args.fsecondtest = ".tmp_db.db"
                #     shutil.copy(name, args.fsecondtest)
                #     conn2 = sqlite3.connect(args.fsecondtest)
                c3 = conn3.cursor()



        def plot(filesize, color=None, label=None, linestyle=None):
            if c2 is None:
                c.execute("SELECT time_{0}, time_{1} FROM results WHERE bw != 0 {2} AND time_{0} != -1 AND time_{1} != -1"
                          .format(METHOD_1, METHOD_2, ("AND file_size == %d" % filesize) if filesize is not None else ""))

                res = c.fetchall()
            else:
                if args.gemodel:
                    loss_cols = "h, k, p, r"
                else:
                    loss_cols = "loss"
                # search in two different files
                c.execute("SELECT time_{0}, bw, {1}, delay_ms, file_size FROM results WHERE bw != 0 {2} AND time_{0} != -1"
                          .format(METHOD_1, loss_cols, ("AND file_size == %d" % filesize) if filesize is not None else ""))

                res1 = c.fetchall()

                c2.execute("SELECT time_{0}, bw, {1}, delay_ms, file_size FROM results WHERE bw != 0 {2} AND time_{0} != -1"
                          .format(METHOD_2, loss_cols, ("AND file_size == %d" % filesize) if filesize is not None else ""))

                tmp_res = c2.fetchall()
                res = []
                join = False
                if join:
                    for elem in res1:
                        for elem2 in tmp_res:
                            if elem2[1:] == elem[1:]:  # if the parameters are the same
                                res.append((elem[0], elem2[0]))
                                print("join", elem, "with", elem2)
                                break
                else:
                    if c3 is not None:
                        c3.execute("SELECT time_{0}, bw, {1}, delay_ms, file_size FROM results WHERE bw != 0 {2} AND time_{0} != -1"
                                  .format(METHOD_3, loss_cols, ("AND file_size == %d" % filesize) if filesize is not None else ""))

                        res3 = c3.fetchall()
                    for i, elem in enumerate(res1):
                        if i >= len(tmp_res) or (c3 is not None and i >= len(res3)):
                            break
                        if c3 is not None:
                            res.append((elem[0], tmp_res[i][0], res3[i][0]))
                        else:
                            res.append((elem[0], tmp_res[i][0]))


            transformed_result = list(map(transforms[transform], res))


            to_plot = [list(sorted([r[i] for r in transformed_result])) for i in range(transforms_output[transform])]

            # plt.margins(x=0.5, y=0.02)

            y_axis = [(i+1)/len(res) for i in range(len(res))]

            if plot_method == "CDF":
                plot_cdf(to_plot, y_axis, transform, color, label, linestyle)

        plt.xlim(0.4, 2.05)
        plt.xticks([0.5, 1, 1.5, 2])

        try:
            filesize = None if args.filesize is None else int(args.filesize)

            plot(filesize, label=legends[idx % len(legends)] if legends is not None else None)
        except ValueError:
            for i, size in enumerate([int(i) for i in args.filesize.split(',')]):
                plot(size, color=colors[i], label=("%sB" % str(size)).replace("000000B", "MB").replace("000B", "kB"), linestyle=lines[i])


if len(name1) == 1:
    legend = plt.legend()
    # use the following for the fairness tests
    # handles, labels = sp.get_legend_handles_labels()
    # legend = plt.figlegend(handles, labels, loc='upper center', bbox_to_anchor=(0.535, 1.175))

else:
    handles, labels = sp.get_legend_handles_labels()
    legend = plt.figlegend(handles, labels, loc='upper center', ncol=5, bbox_to_anchor=(0.52, 1.1))

if args.xlabel:
    plt.xlabel("%s" % transform_unit[transform] if args.xlabel is None else args.xlabel)

if legend:
    frame = legend.get_frame()
    frame.set_facecolor('0.8')
    frame.set_edgecolor('0.8')


plt.tight_layout()

if args.t:
    plt.savefig(args.t, bbox_inches="tight")
else:
    plt.show()

