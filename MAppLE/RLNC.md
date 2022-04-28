Random Linear Network Coding
============================

The implementation of this coding scheme has inherent issues. Whereas packets,
when using XOR or Reed-Solomon coding, are marked with BlockID, RLNC does not
use such a method at all. It instead relies only on packets arriving roughly
in-order.

Out-of-order arrivials can therefore be handled easily in the other two
schemes. The encoded BlockID makes it simple to order arriving packets into
their respective encoded blocks. For RLNC this works as long as only packets
belonging into the same coding window arrive out-of-order. If several disjunct
window positions get mixed up, there is no method to correctly map packets. In
such cases the scheme may still try to recover data, but fails. This produces
malformed, quasi random bytes which the packet decoding framework then tries to
decode as a regular QUIC packet. At best (!) this causes a crash. At worst,
the packet suggests a wrong stream offset or packet number. This will cause the
receiver to wait for preceding information that will never arrive, timing out
after a long while.

Especially with IOD family schedulers such events are likely to occur. But even
LowRTT may cause them.
