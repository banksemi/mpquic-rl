#!/bin/bash

SERVER="nodem1.moongenmultipath.spork-join.filab.uni-hannover.de"
INTER="nodem2.moongenmultipath.spork-join.filab.uni-hannover.de"
MANIFEST="https://10.1.1.2:4242/manifest.mpd"

BITRATE=${BITRATE:=5100000}
ITERATIONS=${ITERATIONS:=5}

CROSSTRAFFIC=${CROSSTRAFFIC:=0}
DUMP=${DUMP:=0}

function start_tcpdump_server {
    FNAME=$(date +%s)_$1_$2.pcap
    ssh $SERVER "mkdir -p ~/server/tcpdump"
    ssh $SERVER "sudo tcpdump -i any -s 100 -w ~/server/tcpdump/$FNAME" &
    sleep 2
}

function kill_tcpdump_server {
    ssh $SERVER "sudo pkill tcpdump"
}

function start_tcpdump_client {
    FNAME=$(date +%s)_$1_$2.pcap
    mkdir -p ~/client/tcpdump
    sudo tcpdump -i any -s 100 -w ~/client/tcpdump/$FNAME &
    sleep 2
}

function kill_tcpdump_client {
    sudo pkill tcpdump
}

function start_server {
    if [ $DUMP -eq "1" ]
    then
        start_tcpdump_server $1 $2
    fi

    ssh $SERVER "cd mapple/server && ./caddy -quic -mp -scheduler $1 -fec -fecConfig $2 -expLog" &
    sleep 2
}

function start_server_no_fec {
    if [ $DUMP -eq "1" ]
    then
        start_tcpdump_server $1 none
    fi

    ssh $SERVER "cd mapple/server && ./caddy -quic -mp -scheduler $1 -fecConfig win-xor" &
    sleep 2
}

function kill_server {
    kill_tcpdump_server

    ssh $SERVER "pkill caddy"
}

function start_client {
    if [ $DUMP -eq "1" ]
    then
        start_tcpdump_client $1 $2
    fi

    python3 astream/dash_client.py -q -mp -m $MANIFEST -s $1 --fec --fecConfig $2 -b $BITRATE
    RETVAL=$?

    kill_tcpdump_client

    return $RETVAL
}

function start_client_no_fec {
    if [ $DUMP -eq "1" ]
    then
        start_tcpdump_client $1 none
    fi

    python3 astream/dash_client.py -q -mp -m $MANIFEST -s $1 --fecConfig win-xor -b $BITRATE
    RETVAL=$?

    kill_tcpdump_client

    return $RETVAL
}

function start_cross_traffic_server {
    ~/D-ITG/bin/ITGRecv &
    sleep 2
}

function start_cross_traffic_client_a {
    ssh $INTER "cd D-ITG/bin && ./ITGSend -T UDP -a 10.1.2.5 -e 400 -E 900 -t 100000" &
    sleep 2
}

function start_cross_traffic_client_b {
    #ssh $INTER "cd D-ITG/bin && " &
    sleep 2
}

function stop_cross_traffic_server {
    pkill ITGRecv
}

function stop_cross_traffic_client {
    ssh $INTER "pkill ITGSend" &
    sleep 2
}

function do_scheduler {
    local retval=1
    while [ $retval -ne 0 ]
    do

        if [ $CROSSTRAFFIC -eq "1" ]
        then
            start_cross_traffic_server
            start_cross_traffic_client_a
            start_cross_traffic_client_b
        fi

        start_server $1 $2
        start_client $1 $2
        retval=$?
        kill_server

        if [ $CROSSTRAFFIC -eq "1" ]
        then
            stop_cross_traffic_client
            stop_cross_traffic_server
        fi

    done

}

function iter_scheduler {
    ctr=1
    while [ $ctr -le $ITERATIONS ]
    do
        echo 'Using Scheduler' $1 'with FEC scheme' $2
        do_scheduler $1 $2
        ((ctr++))
    done
}

function do_scheduler_no_fec {
    retval=1
    while [ $retval -ne 0 ]
    do

        if [ $CROSSTRAFFIC -eq "1" ]
        then
            start_cross_traffic_server
            start_cross_traffic_client_a
            start_cross_traffic_client_b
        fi

        start_server_no_fec $1
        start_client_no_fec $1
        retval=$?
        kill_server

        if [ $CROSSTRAFFIC -eq 1 ]
        then
            stop_cross_traffic_client
            stop_cross_traffic_server
        fi

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

function iter_fec_conf {
    iter_scheduler ll $1
    iter_scheduler s-edpf $1
    iter_scheduler s-iod $1
}

#iter_fec_conf xor4-1
iter_fec_conf xor16-1
iter_fec_conf delay-xor

#iter_scheduler_no_fec s-edpf
#iter_scheduler_no_fec s-iod
#iter_scheduler_no_fec ll
