//go:build linux
// +build linux

package iperf

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func save_tcpInfo(sp *iperfStream, rp *iperf_interval_results) int {
	info := getTCPInfo(sp.conn)

	rp.rtt = uint(info.Tcpi_rtt)
	rp.rto = uint(info.Tcpi_rto)
	rp.interval_retrans = uint(info.Tcpi_total_retrans)

	return 0
}

func getTCPInfo(conn net.Conn) *unix.TCPInfo {
	file, err := conn.(*net.TCPConn).File()
	if err != nil {
		fmt.Printf("File err: %v\n", err)

		return &unix.TCPInfo{}
	}

	defer file.Close()

	fd := file.Fd()

	info, err := unix.GetsockoptTCPInfo(int(fd), unix.SOL_TCP, unix.TCP_INFO)
	if err != nil {
		fmt.Printf("GetsockoptTCPInfo err: %v\n", err)

		return &unix.TCPInfo{}
	}

	return info
}

func PrintTCPInfo(info *unix.TCPInfo) {
	fmt.Printf("TcpInfo: rcv_rtt:%v\trtt:%v\tretransmits:%v\trto:%v\tlost:%v\tretrans:%v\ttotal_retrans:%v\n",
		info.Tcpi_rcv_rtt,
		info.Tcpi_rtt,
		info.Tcpi_retransmits,
		info.Tcpi_rto,
		info.Tcpi_lost,
		info.Tcpi_retrans,
		info.Tcpi_total_retrans,
	)
}
