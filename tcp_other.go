//go:build !linux
// +build !linux

package main

import (
	"fmt"
	"net"
)

func saveTCPInfo(sp *iperfStream, rp *iperf_interval_results) int {
	fmt.Println("TCP stats not supported on this platform")

	rp.rtt = 0
	rp.rto = 0
	rp.interval_retrans = 0

	return 0
}

func getTCPInfo(conn net.Conn) interface{} {
	return nil
}

func PrintTCPInfo(info interface{}) {
	fmt.Println("TCP info not available on this platform")
}
