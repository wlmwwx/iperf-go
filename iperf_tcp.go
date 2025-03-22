package main

import (
	"errors"
	"net"
	"strconv"
	"time"
)

type TCPProto struct {
}

func (t *TCPProto) name() string {
	return TCP_NAME
}

func (t *TCPProto) accept(test *iperfTest) (net.Conn, error) {
	log.Debugf("Enter TCP accept")

	conn, err := test.protoListener.Accept()
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func (t *TCPProto) listen(test *iperfTest) (net.Listener, error) {
	log.Debugf("Enter TCP listen")

	return test.listener, nil
}

func (t *TCPProto) connect(test *iperfTest) (net.Conn, error) {
	log.Debugf("Enter TCP connect")

	tcpAddr, err := net.ResolveTCPAddr("tcp4", test.addr+":"+strconv.Itoa(int(test.port)))
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, err
	}

	err = conn.SetDeadline(time.Now().Add(time.Duration(test.duration+5) * time.Second))
	if err != nil {
		log.Errorf("SetDeadline err: %v", err)

		return nil, err
	}

	return conn, nil
}

func (t *TCPProto) send(sp *iperfStream) int {
	n, err := sp.conn.(*net.TCPConn).Write(sp.buffer)
	if err != nil {
		var serr *net.OpError

		if errors.As(err, &serr) {
			log.Debugf("tcp conn already closed = %v", serr)

			return -1
		}

		log.Errorf("tcp write err = %T %v", err, err)

		return -2
	}

	if n < 0 {
		return n
	}

	sp.result.bytes_sent += uint64(n)
	sp.result.bytes_sent_this_interval += uint64(n)

	log.Debugf("TCP sent %d bytes, total sent: %d", n, sp.result.bytes_sent)

	return n
}

func (t *TCPProto) recv(sp *iperfStream) int {
	n, err := sp.conn.(*net.TCPConn).Read(sp.buffer)
	if err != nil {
		var serr *net.OpError

		if errors.As(err, &serr) {
			log.Debugf("tcp conn already closed = %v", serr)

			return -1
		}

		log.Errorf("tcp recv err = %T %v", err, err)

		return -2
	}

	if n < 0 {
		return n
	}
	if sp.test.state == TEST_RUNNING {
		sp.result.bytes_received += uint64(n)
		sp.result.bytes_received_this_interval += uint64(n)
	}

	return n
}

func (t *TCPProto) init(test *iperfTest) int {
	if test.noDelay {
		for _, sp := range test.streams {
			err := sp.conn.(*net.TCPConn).SetNoDelay(test.noDelay)
			if err != nil {
				log.Errorf("SetNoDelay err: %v", err)

				return -1
			}
		}
	}

	return 0
}

func (t *TCPProto) statsCallback(test *iperfTest, sp *iperfStream, tempResult *iperf_interval_results) int {
	if test.proto.name() == TCP_NAME {
		rp := sp.result

		saveTCPInfo(sp, tempResult)

		totalRetrans := tempResult.interval_retrans // get the temporarily stored result
		tempResult.interval_retrans = totalRetrans - rp.stream_prev_total_retrans

		rp.stream_retrans += tempResult.interval_retrans
		rp.stream_prev_total_retrans = totalRetrans

		if rp.stream_min_rtt == 0 || tempResult.rtt < rp.stream_min_rtt {
			rp.stream_min_rtt = tempResult.rtt
		}

		if rp.stream_max_rtt == 0 || tempResult.rtt > rp.stream_max_rtt {
			rp.stream_max_rtt = tempResult.rtt
		}

		rp.stream_sum_rtt += tempResult.rtt
		rp.stream_cnt_rtt++
	}

	return 0
}

func (t *TCPProto) teardown(test *iperfTest) int {
	return 0
}
