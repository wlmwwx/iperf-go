# iperf-go

**A lightweight, extensible network performance testing tool written in Go**

**2025 Updates**

I have forked this project to add new features and fix bugs. It is currently usable, and I intend to maintain it actively.

- Original repository: [https://github.com/zezhongwang/iperf-go](https://github.com/zezhongwang/iperf-go)
- Improvements borrowed from: [https://github.com/damao33/iperf-go/](https://github.com/damao33/iperf-go/), especially for the RUDP protocol implementation.

-- mfreeman451

Existing network testing tools like NS2 (Network Simulator) have a high learning curve and poor support for protocols not written in C/C++. Lightweight tools like iperf3 lack robust support for extending protocol testing beyond TCP, UDP, and SCTP. With hundreds of application-layer protocols and a need to test custom ones, iperf-go fills this gap by offering a simple interface to measure the network speed of user-defined protocols.

This implementation draws inspiration from iperf3's C source code and is built in Go.

## Usage

### TCP Testing

Server side:

```bash
./iperf-go -s
```

Client side (default parameters):

```bash
./iperf-go -c <server_ip_addr>
./iperf-go -c <server_ip_addr> -R  # -R for server-to-client (downlink) testing
```

### KCP Testing

[KCP Project](https://github.com/xtaci/kcp-go)

Server side:

```bash
./iperf-go -s
```

Client side:

```bash
./iperf-go -c <server_ip_addr> -proto kcp
./iperf-go -c <server_ip_addr> -proto kcp -sw 512 -rw 512  # Set send/receive window sizes
```

### Additional Parameters

For detailed options, run:

```bash
./iperf-go -h
```

Output:

```shell
Usage of ./iperf-go:
-D    No delay option
  -P uint
        The number of simultaneous connections (default 1)
  -R    Reverse mode: client receives, server sends
  -b string
        Bandwidth limit (M/K, default MB/s) (default "0")
  -c string
        Client side (default "127.0.0.1")
  -d uint
        Duration (s) (default 10)
  -debug
        Debug mode
  -f uint
        Flush interval for RUDP (ms) (default 10)
  -fr uint
        RUDP fast resend strategy; 0 disables fast resend
  -h    This help
  -i uint
        Test interval (ms) (default 1000)
  -info
        Info mode
  -l uint
Send/read block size (default 4096)
  -nc
        No congestion control or BBR (default true)
  -p uint
        Connect/listen port (default 5201)
  -proto string
        Protocol under test (default "tcp")
  -rb uint
        Read buffer size (KB) (default 4096)
  -rw uint
        RUDP receive window size (default 512)
  -s    Server side
  -sw uint
        RUDP send window size (default 10)
  -wb uint
        Write buffer size (KB) (default 4096)
```

### Custom Protocol Testing

To test custom application-layer protocols, implement this simple interface:

```shell
type protocol interface {
    name() string
    accept(test *iperf_test) (net.Conn, error)
    listen(test *iperf_test) (net.Listener, error)
    connect(test *iperf_test) (net.Conn, error)
    send(test *iperf_stream) int
    recv(test *iperf_stream) int
    init(test *iperf_test) int
    teardown(test *iperf_test) int
    stats_callback(test *iperf_test, sp *iperf_stream, temp_result *iperf_interval_results) int
}
```

This provides basic bandwidth metrics. For RTT, packet loss, or custom stats, extract them in stats_callback. See iperf_rudp.go for an example implementation.

### Results Example

```shell
Server listening on 5201
Accept connection from client: 221.4.34.225:22567
[ ID]    Interval        Transfer        Bandwidth        RTT      Retrans   Retrans(%)
[  0] 0.00-1.00 sec    1.50 MB    12.00 Mb/s    172.6ms     0    0.00%
[  0] 1.00-2.00 sec    3.12 MB    25.00 Mb/s    169.1ms     0    0.00%
[  0] 2.00-3.00 sec    2.75 MB    22.00 Mb/s    205.3ms   378   19.14%
[  0] 3.00-4.00 sec    2.00 MB    16.00 Mb/s    164.8ms  1490  103.73%
[  0] 4.00-5.00 sec    4.75 MB    38.00 Mb/s    162.9ms   873   25.59%
[  0] 5.00-6.00 sec    1.25 MB    10.00 Mb/s    163.3ms     0    0.00%
[  0] 6.00-7.00 sec    2.50 MB    20.00 Mb/s    163.3ms     0    0.00%
[  0] 7.00-8.00 sec    2.62 MB    21.00 Mb/s    163.5ms     6    0.32%
[  0] 8.00-9.00 sec    2.38 MB    19.00 Mb/s    162.7ms     0    0.00%
[  0] 9.00-10.19 sec   2.50 MB    20.00 Mb/s    163.1ms     0    0.00%
- - - - - - - - - - - - - - - - SUMMARY - - - - - - - - - - - - - - - -
[ ID]    Interval        Transfer        Bandwidth        RTT      Retrans   Retrans(%)
[  0] 0.00-10.19 sec  25.50 MB    20.40 Mb/s    169.1ms  2747    0.15%    [SENDER]
```

### Internal State Machine

State Machine

