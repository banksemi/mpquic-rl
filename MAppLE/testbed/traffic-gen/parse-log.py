#!/usr/bin/env python3
import sys
from collections import namedtuple

Record = namedtuple("Record", "seqNo timestamp size")

records = []

with open(sys.argv[1]) as f:
    for line in f.readlines():
        data = [int(x) for x in line.split()]
        records.append(Record(
            seqNo=data[0],
            timestamp=data[1],
            size=data[2],
        ))

records.sort(key=lambda x: x.seqNo)

accumSize = sum([r.size for r in records])
minTime = min([r.timestamp for r in records])
maxTime = max([r.timestamp for r in records])

rate = accumSize*2**-20 / ((maxTime - minTime) * 10**-9)
print(f"Sent: {accumSize * 2**-20}MB in {(maxTime - minTime) * 10**-9} seconds")
print(f"DL Rate: {rate}MB/s")
print(f"DL Rate: {rate*8}Mb/s")
