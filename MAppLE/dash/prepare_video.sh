#!/bin/bash

set -xe

function encode {
    mkdir -p /videos/video$1s

    ffmpeg -re -stream_loop 4 -i $SRC -c:v libx264 \
        -map 0 -b:v:0 10M -maxrate:v:0 10M -s:v:0 2560x1440 -profile:v:0 baseline \
        -map 0 -b:v:1 5M -maxrate:v:1 5M -s:v:1 1920x1080 -profile:v:1 main \
        -map 0 -b:v:2 1M -maxrate:v:2 1M -s:v:2 854x480 -profile:v:2 main \
        -bufsize 0.5M -bf 1 -keyint_min 4 -g 5 -sc_threshold 0 \
        -b_strategy 0 -use_template 0 -use_timeline 1 \
        -seg_duration $1 -streaming 1 \
        -adaptation_sets "id=0,streams=v" \
        -f dash /videos/video$1s/manifest.mpd
}

encode $1
