package main

import (
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"time"

	KCP "github.com/xtaci/kcp-go/v5"
)

type kcpProto struct {
}

func (*kcpProto) name() string {
	return KCP_NAME
}

func (*kcpProto) accept(test *iperf_test) (net.Conn, error) {
	log.Debugf("Enter KCP accept")

	conn, err := test.proto_listener.Accept()
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 4)
	n, err := conn.Read(buf)

	signal := binary.LittleEndian.Uint32(buf[:])

	if err != nil || n != 4 || signal != ACCEPT_SIGNAL {
		log.Errorf("KCP Receive Unexpected signal")
	}

	log.Debugf("KCP accept succeed. signal = %v", signal)

	return conn, nil
}

func (*kcpProto) listen(test *iperf_test) (net.Listener, error) {
	listener, err := KCP.ListenWithOptions(":"+strconv.Itoa(int(test.port)), nil, int(test.setting.data_shards), int(test.setting.parity_shards))
	err = listener.SetReadBuffer(int(test.setting.read_buf_size))
	if err != nil {
		return nil, err
	} // all income conn share the same underline packet conn, the buffer should be large

	err = listener.SetWriteBuffer(int(test.setting.write_buf_size))
	if err != nil {
		return nil, err
	}

	return listener, nil
}

func (*kcpProto) connect(test *iperf_test) (net.Conn, error) {
	conn, err := KCP.DialWithOptions(test.addr+":"+strconv.Itoa(int(test.port)), nil, int(test.setting.data_shards), int(test.setting.parity_shards))
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, ACCEPT_SIGNAL)

	n, err := conn.Write(buf)
	if err != nil || n != 4 {
		log.Errorf("KCP send accept signal failed")
	}

	log.Debugf("KCP connect succeed.")

	return conn, nil
}

func (*kcpProto) send(sp *iperf_stream) int {
	n, err := sp.conn.(*KCP.UDPSession).Write(sp.buffer)
	if err != nil {
		var serr *net.OpError
		if errors.As(err, &serr) {
			log.Debugf("kcp conn already close = %v", serr)

			return -1
		}

		log.Errorf("kcp write err = %T %v", err, err)

		return -2
	}

	if n < 0 {
		log.Errorf("kcp write err. n = %v", n)

		return n
	}

	sp.result.bytes_sent += uint64(n)
	sp.result.bytes_sent_this_interval += uint64(n)

	//log.Debugf("KCP send %v bytes of total %v", n, sp.result.bytes_sent)
	return n
}

func (*kcpProto) recv(sp *iperf_stream) int {
	// recv is blocking
	n, err := sp.conn.(*KCP.UDPSession).Read(sp.buffer)

	if err != nil {
		var serr *net.OpError

		if errors.As(err, &serr) {
			log.Debugf("kcp conn already close = %v", serr)

			return -1
		}

		log.Errorf("kcp recv err = %T %v", err, err)

		return -2
	}

	if n < 0 {
		return n
	}

	if sp.test.state == TEST_RUNNING {
		sp.result.bytes_received += uint64(n)
		sp.result.bytes_received_this_interval += uint64(n)
	}

	//log.Debugf("KCP recv %v bytes of total %v", n, sp.result.bytes_received)
	return n
}

func (*kcpProto) init(test *iperf_test) int {
	for _, sp := range test.streams {
		err := sp.conn.(*KCP.UDPSession).SetReadBuffer(int(test.setting.read_buf_size))
		if err != nil {
			log.Errorf("SetReadBuffer err = %v", err)

			return 0
		}

		err = sp.conn.(*KCP.UDPSession).SetWriteBuffer(int(test.setting.write_buf_size))
		if err != nil {
			log.Errorf("SetWriteBuffer err = %v", err)

			return 0
		}

		sp.conn.(*KCP.UDPSession).SetWindowSize(int(test.setting.snd_wnd), int(test.setting.rcv_wnd))
		sp.conn.(*KCP.UDPSession).SetStreamMode(true)

		err = sp.conn.(*KCP.UDPSession).SetDSCP(46)
		if err != nil {
			log.Errorf("SetDSCP err = %v", err)

			return 0
		}

		sp.conn.(*KCP.UDPSession).SetMtu(1400)
		sp.conn.(*KCP.UDPSession).SetACKNoDelay(false)

		err = sp.conn.(*KCP.UDPSession).SetDeadline(time.Now().Add(time.Minute))
		if err != nil {
			log.Errorf("SetDeadline err = %v", err)

			return 0
		}

		var noDelay, resend, nc int

		if test.no_delay {
			noDelay = 1
		} else {
			noDelay = 0
		}

		if test.setting.no_cong {
			nc = 1
		} else {
			nc = 0
		}

		resend = int(test.setting.fast_resend)

		sp.conn.(*KCP.UDPSession).SetNoDelay(noDelay, int(test.setting.flush_interval), resend, nc)
	}

	return 0
}

func (*kcpProto) statsCallback(_ *iperf_test, sp *iperf_stream, tempResult *iperf_interval_results) int {
	rp := sp.result

	totalRetrans := uint(KCP.DefaultSnmp.RetransSegs)
	totalLost := uint(KCP.DefaultSnmp.LostSegs)
	totalEarlyRetrans := uint(KCP.DefaultSnmp.EarlyRetransSegs)
	totalFastRetrans := uint(KCP.DefaultSnmp.FastRetransSegs)
	totalRecovers := uint(KCP.DefaultSnmp.FECRecovered)
	totalInPkts := uint(KCP.DefaultSnmp.InPkts)
	totalInSegs := uint(KCP.DefaultSnmp.InSegs)
	totalOutPkts := uint(KCP.DefaultSnmp.OutPkts)
	totalOutSegs := uint(KCP.DefaultSnmp.OutSegs)

	// retrans
	tempResult.interval_retrans = totalRetrans - rp.stream_prev_total_retrans
	rp.stream_retrans += tempResult.interval_retrans
	rp.stream_prev_total_retrans = totalRetrans

	// lost
	tempResult.interval_lost = totalLost - rp.stream_prev_total_lost
	rp.stream_lost += tempResult.interval_lost
	rp.stream_prev_total_lost = totalLost

	// early retrans
	tempResult.interval_early_retrans = totalEarlyRetrans - rp.stream_prev_total_early_retrans
	rp.stream_early_retrans += tempResult.interval_early_retrans
	rp.stream_prev_total_early_retrans = totalEarlyRetrans

	// fast retrans
	tempResult.interval_fast_retrans = totalFastRetrans - rp.stream_prev_total_fast_retrans
	rp.stream_fast_retrans += tempResult.interval_fast_retrans
	rp.stream_prev_total_fast_retrans = totalFastRetrans

	// recover
	rp.stream_recovers = totalRecovers

	// packets receive
	rp.stream_in_pkts = totalInPkts
	rp.stream_out_pkts = totalOutPkts

	// segs receive
	rp.stream_in_segs = totalInSegs
	rp.stream_out_segs = totalOutSegs

	tempResult.rtt = uint(sp.conn.(*KCP.UDPSession).GetSRTTVar() * 1000) // ms to micro sec
	if rp.stream_min_rtt == 0 || tempResult.rtt < rp.stream_min_rtt {
		rp.stream_min_rtt = tempResult.rtt
	}

	if rp.stream_max_rtt == 0 || tempResult.rtt > rp.stream_max_rtt {
		rp.stream_max_rtt = tempResult.rtt
	}

	rp.stream_sum_rtt += tempResult.rtt
	rp.stream_cnt_rtt++

	return 0
}

func (*kcpProto) teardown(_ *iperf_test) int {
	return 0
}
