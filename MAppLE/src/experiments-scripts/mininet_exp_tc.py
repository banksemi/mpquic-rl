"""Custom topology example

Two directly connected switches plus a host for each switch:

   host --- switch --- switch --- host

Adding the 'topos' dict with a key/value pair to generate our newly defined
topology enables one to pass in '--topo=mytopo' from the command line.
"""
import argparse
import datetime
import os
import random
import sqlite3
import time
import imp

# mininet = imp.load_package('mininet', '/home/michelfra/mininet/mininet/')
from mininet.cli import CLI
from mininet.net import Mininet
from mininet.log import setLogLevel
from mininet.link import TCLink, TCULink
from mininet.node import Node, CPULimitedHost
from mininet.topo import Topo
from mininet.clean import cleanup as net_cleanup


class TypeWrapper(object):
    def __init__(self, type_builtin, name):
        self.builtin = type_builtin
        self.name = name

    def __call__(self, *args, **kwargs):
        return self.builtin(*args, **kwargs)

    def __str__(self):
        return self.name

    def __repr__(self):
        return self.name


int = TypeWrapper(int, "INTEGER")
float = TypeWrapper(float, "REAL")
str = TypeWrapper(str, "TEXT")


# GEModel currently unused
class GEModel(object):
    def __init__(self, p, r=None, h=None, k=None, **kwargs):
        self.p = p
        self.r = r
        self.h = h
        self.k = k

    @staticmethod
    def create_randomized_model(n_params, random):
        def generate_value_for(param_name):
            if param_name == "p":
                return random.uniform(0, 1)
            if param_name == "r":
                return random.uniform(2.5, 50)
            if param_name == "h":
                return random.uniform(0, 10)  # (0, 10) donnait un resultat interessant (non_con ultra inefficace)
            if param_name == "k":
                return random.uniform(97, 100)  # (90, 100) donnait un resultat interessant (non_con ultra inefficace)

        params = ["p", "r", "h", "k"]
        args = {}
        for i in range(n_params):
            args[params[i]] = generate_value_for(params[i])
        return GEModel(**args)

    def __str__(self):
        if self.r is None:
            return "gemodel %0.4f%%" % self.p
        elif self.h is None:
            return "gemodel %0.4f%% %0.4f%%" % (self.p, self.r)
        elif self.k is None:
            return "gemodel %0.4f%% %0.4f%% %0.4f%%" % (self.p, self.r, 100 - self.h)
        else:
            return "gemodel %0.4f%% %0.4f%% %0.4f%% %0.4f%%" % (self.p, self.r, 100 - self.h, 100 - self.k)

def ipv4_to_int(ipv4):
    split = ipv4.split('.')
    if len(split) != 4:
        return None
    retval = 0
    retval += int(split[0]) << 24
    retval += int(split[1]) << 16
    retval += int(split[2]) << 8
    retval += int(split[3])
    return retval


class MyTopo(Topo):
    "Simple topology example."

    CANDIDATES = [FEC_XOR, DEFAULT] = ["fec_xor", "default"]

    def __init__(self, bw, delay_ms, loss=(0,), p=(0,), r=(100,), h=(0,), k=(100,), gemodel=False):
        super(MyTopo, self).__init__()
        # client  -------  s1  -------  s2  -------  server
        #                     loss + bw
        #                     + delay

        MyTopo.CMD = {MyTopo.CANDIDATES[0]: {"client": lambda ip, port,
                                                size: "./%s --addr %s -p %d --size %d > out_cli.txt 2>&1" % (
                                                                                MyTopo.CANDIDATES[0], ip, port, size),
                                            "server": lambda:
                                                      "./%s -p %d -s > out_serv.txt 2>&1" % (MyTopo.CANDIDATES[0], port)}}
        if len(MyTopo.CANDIDATES) == 2:
            MyTopo.CMD[MyTopo.CANDIDATES[1]] = {"client": lambda ip, port,
                                                size: "./%s --addr %s -p %d --size %d > out_cli.txt 2>&1" % (
                                                                                  MyTopo.CANDIDATES[1], ip, port, size),
                                            "server": lambda: "./%s -p %d -s > out_serv.txt 2>&1" % (MyTopo.CANDIDATES[1], port)}

        self.bw = bw[0]
        self.gemodel = gemodel
        if self.gemodel:
            self.loss = 0
            self.p = p[0]
            self.r = r[0]
            self.h = h[0]
            self.k = k[0]
        else:
            self.loss = loss[0]
            self.p = 0
            self.r = 100
            self.h = 100
            self.k = 100
        self.delay_ms = delay_ms[0]
        self.server_ip = "10.0.0.2"
        self.client_ip = "10.0.0.1"
        self.server_name = "server"
        self.client_name = "client"
        self.switch_1_name = "s1"
        self.switch_2_name = "s2"
        self.switch_1 = self.addSwitch(self.switch_1_name, cpu=.4)
        self.switch_2 = self.addSwitch(self.switch_2_name, cpu=.4)
        self.server = self.addHost(self.server_name, cpu=.4)
        self.client = self.addHost(self.client_name, cpu=.4)
        self.addLink(self.client, self.switch_1, delay="0.1ms")
        mqs = int(1.5*(((self.bw*1000000)/8)/1200)*(2*self.delay_ms/1000.0))
        print "MAX QUEUE SIZE =", mqs
        self.addLink(self.switch_1, self.switch_2,  # loss=self.loss,
                     bw=self.bw, delay="%dms" % self.delay_ms, max_queue_size=mqs)  # let the mininet-defined qsize
        self.addLink(self.switch_2, self.server, delay="0.1ms")


    def attach_dropper(self, host, intf, seed, port):
        """attaches a deterministic loss genetator at the specified host"""
        seed_ingress = seed
        seed_egress = seed + 42
        if self.gemodel:
            loss_flags = "--gemodel -P {} -R {} -K {} -H {}".format(self.p, self.r, self.k, self.h)
        else:
            loss_flags = "-P {}".format(self.loss)
        
        flags_ingress = "-f dropper_ingress.o --attach {} --attach-ingress --seed {} --ips {},{} --port {}".format(intf, seed_ingress, self.server_ip, self.client_ip, port)
        flags_egress = "-f dropper_egress.o --attach {} --seed {} --ips {},{} --port {}".format(intf, seed_egress, self.server_ip, self.client_ip, port)
        cmd_ingress = """python3 attach_dropper.py -v --udp {} {}""".format(loss_flags, flags_ingress)
        cmd_egress = """python3 attach_dropper.py -v --udp {} {}""".format(loss_flags, flags_egress)
        
        #clean
        print("CLEAN")
        print host.cmd("pushd ebpf_dropper ; python3 attach_dropper.py --clean -f dropper_ingress.o --attach {} ; popd".format(intf))
        print host.cmd("pushd ebpf_dropper ; python3 attach_dropper.py --clean -f dropper_egress.o --attach {} ; popd".format(intf))
        #attach
        print("ATTACH")
        print(cmd_ingress)
        print host.cmd('pushd ebpf_dropper ; {} ; popd'.format(cmd_ingress))
        print host.cmd('pushd ebpf_dropper ; {} ; popd'.format(cmd_egress))

    def disable_ipv6(self, host):
        host.cmd("sysctl -w net.ipv6.conf.all.disable_ipv6=1")
        host.cmd("sysctl -w net.ipv6.conf.default.disable_ipv6=1")
        host.cmd("sysctl -w net.ipv6.conf.lo.disable_ipv6=1")

def load_wsp(filename, nrows, ncols):
    # Open the file
    f = open("%s" % filename)
    lines = f.readlines()
    f.close()

    # The interesting line is the third one
    line = lines[2]
    split_line = line.split(",")
    nums = []

    for x in split_line:
        nums.append(float(x))
    print(len(split_line))
    print(len(nums))

    if len(nums) != nrows*ncols:
        raise Exception("wrong number of elements in wsp matrix: %d instead of %d(with %d rows)" % (len(nums), nrows*ncols, nrows))

    print("load matrix")

    # The matrix is encoded as an array of nrowsxncols
    matrix = []
    for i in range(nrows):
        row = []
        for j in range(ncols):
            try:
                row.append(nums[i * ncols + j])
            except:
                print(i * ncols + j)
                raise

        matrix.append(row)

    return matrix


class ParamsGenerator(object):
    def __init__(self, params_values, matrix):
        self.index = 0
        self.params_values = params_values
        if isinstance(params_values.get("delay_ms", None), list):
            for i in range(len(params_values["delay_ms"])):
                params_values["delay_ms_%d" % i] = params_values["delay_ms"][i]
            params_values.pop("delay_ms", None)
        self.param_names = list(sorted(params_values.keys()))
        self.ranges_full_name = {self._full_name(key, val["count"]): val["range"] for key, val in params_values.items()}
        names = []
        for n in params_values.keys():
            for key in params_values[n]["range"].keys() if isinstance(params_values[n]["range"], dict) else [None]:
                names.append((n, key))
        self.param_full_names = sorted(flatten(map(lambda name_key: [self._full_name(name_key[0], i, name_key[1]) for i in range(params_values[name_key[0]]["count"])], names)))
        # decide for an arbitrary ordering of the parameters
        print self.param_full_names
        self.params_indexes = {self.param_full_names[i]: i for i in range(len(self.param_full_names))}
        self.matrix = matrix

    def _full_name(self, name, count, key=None):
        if self.params_values[name]["count"] > 1:
            return "%s_%d%s" % (name, count, ("_%s" % str(key)) if key is not None else "")
        return "%s%s" % (name, ("_%s" % str(key)) if key is not None else "")

    def generate_value(self):
        retval = self._generate_value_at(self.index)
        self.index += 1
        return retval

    def _generate_value_at(self, i):
        retval = {}
        for name in self.param_names:
            retval[name] = []
            for count in range(self.params_values[name]["count"]):
                param_range = self.params_values[name]["range"]
                if isinstance(param_range, dict):
                    to_append = {key: self.params_values[name]["type"](
                              self.matrix[self.params_indexes[self._full_name(name, count, key)]][i] * (param_range[key][1] - param_range[key][0]) + param_range[key][0])
                        for key in param_range.keys()}
                else:
                    full_name = self._full_name(name, count)
                    param_index = self.params_indexes[full_name]
                    float_value = self.matrix[param_index][i]
                    to_append = self.params_values[name]["type"](float_value * (param_range[1] - param_range[0]) + param_range[0])
                retval[name].append(to_append)
        return retval

    def __len__(self):
        return len(self.matrix[0])

    def generate_all_values(self):
        for i in range(len(self.matrix[0])):
            yield self._generate_value_at(i)

    def generate_sql_create_table(self, additional_values):
        lines = []
        for name in self.param_names:
            for count in range(self.params_values[name]["count"]):
                if isinstance(self.params_values[name]["range"], dict):
                    for k in sorted(self.params_values[name]["range"].keys()):
                        lines.append("%s %s NOT NULL" % (self._full_name(name, count, k),
                                                         str(self.params_values[name]["type"])))
                else:
                    lines.append("%s %s NOT NULL" % (self._full_name(name, count), str(self.params_values[name]["type"])))

        for name, type in additional_values:
            lines.append("%s %s NOT NULL" % (name, str(type)))

        return """
        CREATE TABLE IF NOT EXISTS results (
          %s
        );
        """ % (',\n'.join(lines))

    @staticmethod
    def generate_sql_insert(vals):
        retval = []
        for v in vals:
            if isinstance(v, dict):
                retval += [str(v[k]) for k in sorted(v.keys())]
            else:
                retval.append("'%s'" % str(v))
        print """ INSERT INTO results VALUES (%s); """ % ", ".join(retval)
        return """ INSERT INTO results VALUES (%s); """ % ", ".join(retval)


def flatten(l):
    """
        inefficiently flattens a list
        l: an arbitrary list
    """
    if not l:
        return l
    if isinstance(l[0], list):
        return flatten(l[0]) + flatten(l[1:])
    return [l[0]] + flatten(l[1:])


def median(func, times):
    """
    :param func: the function returning the metric for which we must take the median result
    :param times: the number of executions of func used to compute the median result
    :return: the median result of func
    """

    def f(i):
        print "exec %d/%d" % (i+1, times)
        return func()
    results =  sorted(filter(lambda x: x is not None, [f(i) for i in range(times)]), key=lambda x: x['time'])
    print [r["time"] for r in results]
    if len(results) == 0:
        return None
    with open("out_simple.txt", "a") as f:
        f.write(str([r["time"] for r in results]))
        f.write("\n")
        f.write(str(results[int(len(results)/2)]))
        f.write("\n")
    return results[int(len(results)/2)]

if __name__ == "__main__":

    parser = argparse.ArgumentParser()
    parser.add_argument("--namefirsttest", help="represents the name of the first test")
    parser.add_argument("--namesecondtest", help="represents the name of the second test", default=None)
    parser.add_argument("--gemodel", action="store_true", help="represents the name of the second test", default=False)
    parser.add_argument("--startindex", help="starting index", type=int, default=0)
    parser.add_argument("--out", help="filename of the resulting database", default="results.db")
    args = parser.parse_args()

    if not args.gemodel:
        ranges = {
            "bw": {"range": [0.3, 10], "type": float, "count": 1},  # Mbps
            "loss": {"range": [1, 8], "type": float, "count": 1},  # %
            "delay_ms": {"range": [100, 400], "type": int, "count": 1},  # ms
        }
        # ranges = {
        #     "bw": {"range": [1.89, 1.89], "type": float, "count": 1},  # Mbps
        #     "loss": {"range": [6, 6], "type": float, "count": 1},  # %
        #     "delay_ms": {"range": [380, 380], "type": int, "count": 1},  # ms
        # }
    else:
        ranges = {
            "bw": {"range": [0.3, 10], "type": float, "count": 1},  # Mbps
            "p": {"range": [1, 8], "type": float, "count": 1},  # %
            "r": {"range": [8, 50], "type": float, "count": 1},  # %
            "h": {"range": [0, 10], "type": float, "count": 1},  # %
            "k": {"range": [98, 100], "type": float, "count": 1},  # %
            "delay_ms": {"range": [100, 400], "type": int, "count": 1},  # ms
        }

    if args.namesecondtest is not None:
        MyTopo.CANDIDATES = [args.namefirsttest, args.namesecondtest]
    else:
        MyTopo.CANDIDATES = [args.namefirsttest]
    filename = "wsp_owd_8"
    nrows, ncols = 8, 139
    matrix = load_wsp(filename, nrows, ncols)
    gen = ParamsGenerator(ranges, matrix)
    vals = gen.generate_all_values()
    # vals = generate_variance_tests(ranges)

    conn = sqlite3.connect(args.out)
    cursor = conn.cursor()
    print gen.generate_sql_create_table(additional_values=[("time_%s" % can, float) for can in MyTopo.CANDIDATES] + [('file_size', int)])
    cursor.execute(gen.generate_sql_create_table(additional_values=[("time_%s" % can, float) for can in MyTopo.CANDIDATES] + [('file_size', int)]))
    conn.commit()

    setLogLevel('info')

    print 'len =', len(gen)

    i = args.startindex
    names = gen.param_full_names + ["file_size", "time_%s" % MyTopo.DEFAULT]
    topo, net = None, None
    port = 6121
    if args.gemodel:
        vals = list(vals)
        r = random.Random(42)
        r.shuffle(vals)
    for v in list(vals)[i:]:

        # arbitrary case
        # v["bw"] = [20, 20]
        # v["delay_ms_0"] = [{'in': 30, 'out': 30}]
        # v["delay_ms_1"] = [{'in': 30, 'out': 30}]
        print "v == ", v

        i += 1
        print "experiment %d/%d" % (i, len(gen))
        # for size in [1000, 10000, 1000000]:
        # for size in [1000, 10000, 50000, 1000000]:
        for size in [1000, 10000, 50000, 1000000]:
            elapsed_list = []
            for c in MyTopo.CANDIDATES:
                if topo is None:
                    topo = MyTopo(gemodel=args.gemodel, **v)
                    net = Mininet(topo, link=TCLink, host=CPULimitedHost)
                    net.start()
                else:
                    try:
                        net.stop()
                    except:
                        pass
                    net_cleanup()
                    topo = MyTopo(gemodel=args.gemodel, **v)
                    net = Mininet(topo, link=TCLink, host=CPULimitedHost)
                    net.start()
                    # server.cmd("kill $HTTP_PID")
                    # topo.change_parameters(**v)
                client = net.get("client")
                server = net.get("server")
                topo.disable_ipv6(client)
                topo.disable_ipv6(server)
                s1 = net.get("s1")
                s2 = net.get("s2")

                time.sleep(1)
                print client.cmd("ping %s -c 2" % topo.server_ip, verbose=True)
                MAX_FAILS = 5   # with high loss rates, fails can occur, such as impossible to perform handshake
                j = 0
                def run():
                    global j
                    j += 1
                    print "CANDIDATE = ", c
                    print "params =", v
                    # net.pingAll()
                    server.cmd("%s &" % topo.CMD[c]["server"]())
                    server.cmd("HTTP_PID=$!")
                    time.sleep(1)
                    print "ATTACH DETERMINISTIC DROPPER"
                    # drop packets from server to client
                    topo.attach_dropper(s2, 's2-eth2', i, port)
                    print "start"
                    now = datetime.datetime.now()
                    print client.cmd(topo.CMD[c]["client"](topo.server_ip, port, size), verbose=True)
                    err = int(client.cmd("echo $?"))
                    if err != 0:
                        print("client returned err %d" % err)
                        print(client.cmd("cat out_cli.txt"))
                        print(client.cmd("cat out_serv.txt"))
                        run.nfails += 1
                        print server.cmd("sudo kill -9 $HTTP_PID")
                        #CLI(net)
                        if run.nfails < MAX_FAILS:
                            return run()
                        return None
                    elapsed_ms = (datetime.datetime.now() - now).total_seconds() * 1000
                    # time.sleep(1)
                    print "done"
                    print "elapsed: %f milliseconds for %s" % (elapsed_ms, c)
                    print server.cmd("sudo kill -9 $HTTP_PID")
                    # time.sleep(5)
                    #CLI(net)
                    return {"time": elapsed_ms}

                run.nfails = 0
                elapsed_list.append(median(func=run, times=9))
                print "median =", elapsed_list[-1]
            # ugly way to handle failed results...
            elapsed_time = map(lambda x: x if x is not None else -1, [(x["time"] if x is not None and "time" in x else None) for x in elapsed_list])
            values_list = flatten([v[k] for k in sorted(v.keys())]) + elapsed_time + [size]
            print gen.generate_sql_insert(values_list)
            cursor.execute(gen.generate_sql_insert(values_list))
            conn.commit()
            print "committed"
            net.stop()
            net_cleanup()


# topos = {'mytopo': (lambda: MyTopo())}


