import time
import argparse
from basicTopo import setup_environment

SERVER_CMD = "/App/quic/server_mt"
CERTPATH = "--certpath /App/quic/quic_go_certs"
SCH = "-scheduler %s"
ARGS = "-bind :6121 -www /var/www/"
END = "> /App/logs/server.logs 2>&1"

BASIC_DELAY = 10

CLIENT_CMD = "/App/quic/client_mt -m https://10.0.0.20:6121/demo  > /App/logs/client.logs 2>&1"

TCP_SERVER_CMD = "cd /var/www && python -m SimpleHTTPServer 80 &"
TCP_CLIENT_CMD = "curl -s -o /dev/null 10.0.0.20/demo &"


def setup():
    net = setup_environment()
    net.start()
    return net


def exec_test(server_cmd, rtt, tcp_traffic):
    network = setup()

    s1 = network.get("s1")
    server = network.get("server")
    client = network.get("client")

    if tcp_traffic:
        server.cmd(TCP_SERVER_CMD)

    server.sendCmd(server_cmd)
    client.cmd("sleep 1")

    s1.cmd("./scripts/set_delay.bash %d" % int((BASIC_DELAY + rtt) / 2))
    client.cmd("./scripts/client_set_delay.bash %d" % int((BASIC_DELAY + rtt) / 2))

    if tcp_traffic:
        client.cmd(TCP_CLIENT_CMD)

    start = time.time()
    client.sendCmd(CLIENT_CMD)
    # Timeout of 20 seconds for detecting crashing tests
    output = client.monitor(timeoutms=20000)

    # Check for timeout
    if client.waiting:
        delta = 20
        client.sendInt()
        client.waiting = False
        network.stop()
        time.sleep(1)
        network.cleanup()
    else:
        # TODO: Check for errors here?? How??
        delta = time.time() - start

    server.sendInt()

    server.monitor()
    server.waiting = False


def do_training(sch, rtt, tcp_b):
    server_cmd = " ".join([SERVER_CMD, CERTPATH, SCH % sch, ARGS, END])

    exec_test(server_cmd, rtt, tcp_b)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Executes a test with defined scheduler')
    parser.add_argument('--scheduler', dest="sch", help="Scheduler (rtt, random)", required=True)
    parser.add_argument('--rtt', type=int, dest="rtt", help="rtt primary leg")
    parser.add_argument('--background-tcp', dest="tcp_background", action="store_true",
                        help='generates TCP background traffic during tests')

    args = parser.parse_args()
    do_training(args.sch, args.rtt, args.tcp_background)
