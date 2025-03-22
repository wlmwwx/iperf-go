package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"
)

func (test *iperf_test) createStreams() int {
	for i := uint(0); i < test.stream_num; i++ {
		conn, err := test.proto.connect(test)
		if err != nil {
			log.Errorf("Connect failed. err = %v", err)

			return -1
		}

		var sp *iperf_stream

		if test.mode == IPERF_SENDER {
			sp = test.newStream(conn, SENDER_STREAM)
		} else {
			sp = test.newStream(conn, RECEIVER_STREAM)
		}

		test.streams = append(test.streams, sp)
	}

	return 0
}

func (test *iperf_test) createClientTimer() int {
	now := time.Now()
	cd := TimerClientData{p: test}

	test.timer = timer_create(now, clientTimerProc, cd, test.duration*1000) // convert sec to ms
	times := test.duration * 1000 / test.interval

	test.stats_ticker = ticker_create(now, clientStatsTickerProc, cd, test.interval, times-1)
	test.report_ticker = ticker_create(now, clientReportTickerProc, cd, test.interval, times-1)

	if test.timer.timer == nil || test.stats_ticker.ticker == nil || test.report_ticker.ticker == nil {
		log.Error("timer create failed.")
	}

	return 0
}

func clientTimerProc(data TimerClientData, now time.Time) {
	log.Debugf("Enter client_timer_proc")

	test := data.p.(*iperf_test)
	test.timer.done <- true
	test.done = true // will end send/recv in iperf_send/iperf_recv, and then triggered TEST_END
	test.timer.timer = nil
}

func clientStatsTickerProc(data TimerClientData, now time.Time) {
	test := data.p.(*iperf_test)

	if test.done {
		return
	}

	if test.stats_callback != nil {
		test.stats_callback(test)
	}
}

func clientReportTickerProc(data TimerClientData, now time.Time) {
	test := data.p.(*iperf_test)

	if test.done {
		return
	}

	if test.reporter_callback != nil {
		test.reporter_callback(test)
	}
}

func (test *iperf_test) createClientOmitTimer() int {
	// undo, depend on which kind of timer
	return 0
}

func sendTickerProc(data TimerClientData, now time.Time) {
	sp := data.p.(*iperf_stream)
	sp.test.checkThrottle(sp, now)
}

func (test *iperf_test) clientEnd() {
	log.Debugf("Enter client_end")
	for _, sp := range test.streams {
		err := sp.conn.Close()
		if err != nil {
			log.Errorf("Stream close failed. err = %v", err)

			return
		}
	}

	if test.reporter_callback != nil {
		test.reporter_callback(test)
	}

	test.proto.teardown(test)
	if test.setSendState(IPERF_DONE) < 0 {
		log.Errorf("set_send_state failed")
	}

	log.Infof("Client Enter IPerf Done...")

	if test.ctrl_conn != nil {
		err := test.ctrl_conn.Close()
		if err != nil {
			log.Errorf("Ctrl conn close failed. err = %v", err)

			return
		}

		test.ctrl_chan <- IPERF_DONE // Ensure main loop exits
	}
}

func (test *iperf_test) handleClientCtrlMsg() {
	buf := make([]byte, 4)

	for test.state != IPERF_DONE { // Exit before reading if done
		if n, err := test.ctrl_conn.Read(buf); err == nil {
			state := binary.LittleEndian.Uint32(buf[:])

			log.Debugf("Client Ctrl conn receive n = %v state = [%v]", n, state)

			test.state = uint(state)

			log.Infof("Client Enter %v state...", test.state)
		} else {
			log.Errorf("ctrl_conn read failed. err=%T, %v", err, err)

			test.ctrl_conn.Close()
			test.ctrl_chan <- IPERF_DONE

			return
		}

		switch test.state {
		case IPERF_EXCHANGE_PARAMS:
			if rtn := test.exchangeParams(); rtn < 0 {
				log.Errorf("exchange_params failed. rtn = %v", rtn)
				test.ctrl_chan <- IPERF_DONE

				return
			}
		case IPERF_CREATE_STREAM:
			if rtn := test.createStreams(); rtn < 0 {
				log.Errorf("create_streams failed. rtn = %v", rtn)
				test.ctrl_chan <- IPERF_DONE

				return
			}
		case TEST_START:
			if rtn := test.initTest(); rtn < 0 {
				log.Errorf("init_test failed. rtn = %v", rtn)
				test.ctrl_chan <- IPERF_DONE

				return
			}
			if rtn := test.createClientTimer(); rtn < 0 {
				log.Errorf("create_client_timer failed. rtn = %v", rtn)
				test.ctrl_chan <- IPERF_DONE

				return
			}
			if rtn := test.createClientOmitTimer(); rtn < 0 {
				log.Errorf("create_client_omit_timer failed. rtn = %v", rtn)
				test.ctrl_chan <- IPERF_DONE

				return
			}
			if test.mode == IPERF_SENDER {
				if rtn := test.createSenderTicker(); rtn < 0 {
					log.Errorf("create_sender_ticker failed. rtn = %v", rtn)
					test.ctrl_chan <- IPERF_DONE

					return
				}
			}
		case TEST_RUNNING:
			test.ctrl_chan <- TEST_RUNNING
		case IPERF_EXCHANGE_RESULT:
			if rtn := test.exchangeResults(); rtn < 0 {
				log.Errorf("exchange_results failed. rtn = %v", rtn)
				test.ctrl_chan <- IPERF_DONE

				return
			}
		case IPERF_DISPLAY_RESULT:
			test.clientEnd()
		case IPERF_DONE:
			test.ctrl_chan <- IPERF_DONE

			return
		case SERVER_TERMINATE:
			oldState := test.state
			test.state = IPERF_DISPLAY_RESULT
			test.reporter_callback(test)
			test.state = oldState
		default:
			log.Errorf("Unexpected situation with state = %v.", test.state)
			test.ctrl_chan <- IPERF_DONE

			return
		}
	}

	test.ctrl_chan <- IPERF_DONE // Ensure exit
}

func (test *iperf_test) ConnectServer() int {
	tcpAddr, err := net.ResolveTCPAddr("tcp4", test.addr+":"+strconv.Itoa(int(test.port)))
	if err != nil {
		log.Errorf("Resolve TCP Addr failed. err = %v, addr = %v", err, test.addr+strconv.Itoa(int(test.port)))

		return -1
	}

	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		log.Errorf("Connect TCP Addr failed. err = %v, addr = %v", err, test.addr+strconv.Itoa(int(test.port)))

		return -1
	}

	test.ctrl_conn = conn
	fmt.Printf("Connect to server %v succeed.\n", test.addr+":"+strconv.Itoa(int(test.port)))

	return 0
}
func (test *iperf_test) runClient() int {

	rtn := test.ConnectServer()
	if rtn < 0 {
		log.Errorf("ConnectServer failed")
		return -1
	}

	go test.handleClientCtrlMsg()

	var isIperfDone bool = false

	var testEndNum uint = 0

	for isIperfDone != true {
		select {
		case state := <-test.ctrl_chan:
			if state == TEST_RUNNING {
				// set non-block for non-UDP test. unfinished
				// Regular mode. Client sends.
				log.Info("Client enter Test Running state...")
				for i, sp := range test.streams {
					if sp.role == SENDER_STREAM {
						go sp.iperfSend(test)

						log.Infof("Client Stream %v start sending.", i)
					} else {
						go sp.iperfRecv(test)

						log.Infof("Client Stream %v start receiving.", i)
					}
				}

				log.Info("Create all streams finish...")
			} else if state == TEST_END {
				testEndNum++

				if testEndNum < test.stream_num || testEndNum == test.stream_num+1 { // redundant TEST_END signal generate by set_send_state
					continue
				} else if testEndNum > test.stream_num+1 {
					log.Errorf("Receive more TEST_END signal than expected")

					return -1
				}

				log.Infof("Client all Stream closed.")

				// test_end_num == test.stream_num. all the stream send TEST_END signal
				test.done = true

				if test.stats_callback != nil {
					test.stats_callback(test)
				}

				if test.setSendState(TEST_END) < 0 {
					log.Errorf("set_send_state failed. %v", TEST_END)

					return -1
				}

				log.Info("Client Enter Test End State.")
			} else if state == IPERF_DONE {
				isIperfDone = true
			} else {
				log.Debugf("Channel Unhandle state [%v]", state)
			}
		}
	}
	return 0
}
