#!/bin/sh

SRC="jellyfish.mkv"
SRCURL="http://www.jell.yfish.us/media/jellyfish-100-mbps-hd-h264.mkv"

if [ ! -f "$SRC" ]; then
    wget $SRCURL -O $SRC
fi

function encode {
    mkdir -p video$1s

    ffmpeg -re -stream_loop 4 -i $SRC -c:v libx264 \
        -map 0 -b:v:0 10M -maxrate:v:0 10M -s:v:0 2560x1440 -profile:v:0 baseline \
        -map 0 -b:v:1 5M -maxrate:v:1 5M -s:v:1 1920x1080 -profile:v:1 main \
        -map 0 -b:v:2 1M -maxrate:v:2 1M -s:v:2 854x480 -profile:v:2 main \
        -bufsize 0.5M -bf 1 -keyint_min 4 -g 5 -sc_threshold 0 \
        -b_strategy 0 -use_template 0 -use_timeline 1 \
        -seg_duration $1 -streaming 1 \
        -adaptation_sets "id=0,streams=v" \
        -f dash video$1s/manifest.mpd

    ls -l video/*stream0* | awk '{print $5}' > chunksizes-stream0.txt
    ls -l video/*stream1* | awk '{print $5}' > chunksizes-stream1.txt
    ls -l video/*stream2* | awk '{print $5}' > chunksizes-stream2.txt
}

encode 0.1
encode 0.2
encode 0.3
encode 0.4
encode 0.5
encode 0.6
encode 0.7
encode 0.8
encode 0.9
encode 1
encode 2
encode 3
encode 4
encode 5
