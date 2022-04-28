#!/usr/bin/env python3
import math
import sys

sizes = []

def get_mean(l):
    return sum(l) / len(l)


def get_median(l):
    return sorted(l)[len(l) // 2]


def get_variance(l):
    mean = get_mean(l)
    return math.sqrt(sum([(x - mean)**2 for x in l]) / (len(l) - 1))


with open(sys.argv[1], "r") as f:
    sizes = [float(line) for line in f.readlines()]

mean = round(get_mean(sizes) / 2**10, 2)
median = round(get_median(sizes) / 2**10, 2)
variance = round(get_variance(sizes) / 2**10, 2)

print("stream chunk sizes")
print(f"  mean:     {mean}k")
print(f"  median:   {median}k")
print(f"  variance: {variance}k")
