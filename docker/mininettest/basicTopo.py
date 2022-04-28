from mininet.topo import Topo
from mininet.cli import CLI
from mininet.net import Mininet
from mininet.node import OVSBridge, Host


def setup_environment():
    net = Mininet(topo=DoubleConnTopo(), switch=OVSBridge, controller=None)
    server = net.get("server")
    client = net.get("client")
    s1 = net.get("s1")

    server.setIP("10.0.0.20", intf="server-eth0")
    client.setIP("10.0.0.1", intf="client-eth0")
    client.setIP("10.0.0.2", intf="client-eth1")

    client.cmd("./scripts/routing.bash")
    client.cmd("./scripts/tc_client.bash")
    s1.cmd("./scripts/tc_s1.bash")

    return net


class DoubleConnTopo(Topo):

    def build(self):
        client = self.addHost("client")
        server = self.addHost("server")
        s1 = self.addSwitch('s1')
        self.addLink(s1, client)
        self.addLink(s1, client)
        self.addLink(s1, server)


if __name__ == '__main__':
    NET = setup_environment()
    NET.start()
    CLI(NET)
    NET.stop()
