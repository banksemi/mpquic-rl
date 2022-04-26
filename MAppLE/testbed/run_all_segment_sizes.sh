#!/bin/bash

SERVER="server.mpquic-fec.spork-join.filab.uni-hannover.de"
INTER="intermediate.mpquic-fec.spork-join.filab.uni-hannover.de"
MANIFEST="https://10.1.1.2:4242/manifest.mpd"

BITRATE=${BITRATE:=10000000}
ITERATIONS=${ITERATIONS:=5}

function write_caddyfile {
    cat <<EOF> ~/server/Caddyfile
http://0.0.0.0:4040 {
    root video/
    log stdout
}

# Secure listener, required for TLS and QUIC connections
https://10.1.1.2:4242 {
    root dash/video$1s/
    tls /users/jwolff/server/cert.pem /users/jwolff/server/privkey.pem
    log stdout
}
EOF
}

function start_server_no_fec {
    ssh $SERVER "cd server && ./caddy -quic -mp -scheduler $1 -fecConfig win-xor" &
    sleep 2
}

function kill_server {
    ssh $SERVER "pkill caddy"
}

function start_client_no_fec {
    python3 astream/dash_client.py -q -mp -m $MANIFEST -s $1 --fecConfig win-xor -b $BITRATE
    RETVAL=$?

    return $RETVAL
}

function do_scheduler_no_fec {
    retval=1
    while [ $retval -ne 0 ]
    do
        start_server_no_fec $1
        start_client_no_fec $1
        retval=$?
        kill_server
    done

}

function iter_scheduler_no_fec {
    ctr=1
    while [ $ctr -le $ITERATIONS ]
    do
        echo 'Using Scheduler' $1 'without FEC'
        do_scheduler_no_fec $1
        ((ctr++))
    done
}

function run {
    # prepare
    write_caddyfile $1

    # run
    iter_scheduler_no_fec ll

    # oraganize results
    resdir="results/video$1s"
    mkdir -p $resdir
    mv ~/client/log $resdir
    mv ~/client/proxy_log $resdir
}

run 0.1
run 0.2
run 0.3
run 0.4
run 0.5
run 0.6
run 0.7
run 0.8
run 0.9
run 1
run 2
run 3
run 4
run 5
