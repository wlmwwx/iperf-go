package iperf

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

func (*kcpProto) accept(test *IperfTest) (net.Conn, error) {
	Log.Debugf("Enter KCP accept")

	conn, err := test.protoListener.Accept()
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 4)
	n, err := conn.Read(buf)

	signal := binary.LittleEndian.Uint32(buf[:])

	if err != nil || n != 4 || signal != ACCEPT_SIGNAL {
		Log.Errorf("KCP Receive Unexpected signal")
	}

	Log.Debugf("KCP accept succeed. signal = %v", signal)

	return conn, nil
}

func (*kcpProto) listen(test *IperfTest) (net.Listener, error) {
	listener, err := KCP.ListenWithOptions(":"+strconv.Itoa(int(test.port)), nil, int(test.setting.dataShards), int(test.setting.parityShards))
	err = listener.SetReadBuffer(int(test.setting.readBufSize))
	if err != nil {
		return nil, err
	} // all income conn share the same underline packet conn, the buffer should be large

	err = listener.SetWriteBuffer(int(test.setting.writeBufSize))
	if err != nil {
		return nil, err
	}

	return listener, nil
}

func (*kcpProto) connect(test *IperfTest) (net.Conn, error) {
	conn, err := KCP.DialWithOptions(test.addr+":"+strconv.Itoa(int(test.port)), nil, int(test.setting.dataShards), int(test.setting.parityShards))
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, ACCEPT_SIGNAL)

	n, err := conn.Write(buf)
	if err != nil || n != 4 {
		Log.Errorf("KCP send accept signal failed")
	}

	Log.Debugf("KCP connect succeed.")

	return conn, nil
}

func (*kcpProto) send(sp *iperfStream) int {
	n, err := sp.conn.(*KCP.UDPSession).Write(sp.buffer)
	if err != nil {
		var serr *net.OpError
		if errors.As(err, &serr) {
			Log.Debugf("kcp conn already close = %v", serr)

			return -1
		}

		Log.Errorf("kcp write err = %T %v", err, err)

		return -2
	}

	if n < 0 {
		Log.Errorf("kcp write err. n = %v", n)

		return n
	}

	sp.result.bytes_sent += uint64(n)
	sp.result.bytes_sent_this_interval += uint64(n)

	//log.Debugf("KCP send %v bytes of total %v", n, sp.result.bytes_sent)
	return n
}

func (*kcpProto) recv(sp *iperfStream) int {
	// recv is blocking
	n, err := sp.conn.(*KCP.UDPSession).Read(sp.buffer)

	if err != nil {
		var serr *net.OpError

		if errors.As(err, &serr) {
			Log.Debugf("kcp conn already close = %v", serr)

			return -1
		}

		Log.Errorf("kcp recv err = %T %v", err, err)

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

func (*kcpProto) init(test *IperfTest) int {
	for _, sp := range test.streams {
		err := sp.conn.(*KCP.UDPSession).SetReadBuffer(int(test.setting.readBufSize))
		if err != nil {
			Log.Errorf("SetReadBuffer err = %v", err)

			return 0
		}

		err = sp.conn.(*KCP.UDPSession).SetWriteBuffer(int(test.setting.writeBufSize))
		if err != nil {
			Log.Errorf("SetWriteBuffer err = %v", err)

			return 0
		}

		sp.conn.(*KCP.UDPSession).SetWindowSize(int(test.setting.sndWnd), int(test.setting.rcvWnd))
		sp.conn.(*KCP.UDPSession).SetStreamMode(true)

		err = sp.conn.(*KCP.UDPSession).SetDSCP(46)
		if err != nil {
			Log.Errorf("SetDSCP err = %v", err)

			return 0
		}

		sp.conn.(*KCP.UDPSession).SetMtu(1400)
		sp.conn.(*KCP.UDPSession).SetACKNoDelay(false)

		err = sp.conn.(*KCP.UDPSession).SetDeadline(time.Now().Add(time.Minute))
		if err != nil {
			Log.Errorf("SetDeadline err = %v", err)

			return 0
		}

		var noDelay, resend, nc int

		if test.noDelay {
			noDelay = 1
		} else {
			noDelay = 0
		}

		if test.setting.noCong {
			nc = 1
		} else {
			nc = 0
		}

		resend = int(test.setting.fastResend)

		sp.conn.(*KCP.UDPSession).SetNoDelay(noDelay, int(test.setting.flushInterval), resend, nc)
	}

	return 0
}

func (*kcpProto) statsCallback(_ *IperfTest, sp *iperfStream, tempResult *iperf_interval_results) int {
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

func (*kcpProto) teardown(_ *IperfTest) int {
	return 0
}
