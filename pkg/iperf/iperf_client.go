package iperf

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"
)

func (test *IperfTest) createStreams() int {
	for i := uint(0); i < test.streamNum; i++ {
		conn, err := test.proto.connect(test)
		if err != nil {
			Log.Errorf("Connect failed. err = %v", err)

			return -1
		}

		var sp *iperfStream

		if test.mode == IPERF_SENDER {
			sp = test.newStream(conn, SENDER_STREAM)
		} else {
			sp = test.newStream(conn, RECEIVER_STREAM)
		}

		test.streams = append(test.streams, sp)
	}

	return 0
}

func (test *IperfTest) createClientTimer() int {
	now := time.Now()
	cd := TimerClientData{p: test}

	test.timer = timerCreate(now, clientTimerProc, cd, test.duration*1000) // convert sec to ms
	times := test.duration * 1000 / test.interval

	test.statsTicker = tickerCreate(now, clientStatsTickerProc, cd, test.interval, times-1)
	test.reportTicker = tickerCreate(now, clientReportTickerProc, cd, test.interval, times-1)

	if test.timer.timer == nil || test.statsTicker.ticker == nil || test.reportTicker.ticker == nil {
		Log.Error("timer create failed.")
	}

	return 0
}

func clientTimerProc(data TimerClientData, now time.Time) {
	Log.Debugf("Enter client_timer_proc")

	test := data.p.(*IperfTest)

	test.timer.done <- true

	test.done = true // will end send/recv in iperf_send/iperf_recv, and then triggered TEST_END

	test.timer.timer = nil
}

func clientStatsTickerProc(data TimerClientData, now time.Time) {
	test := data.p.(*IperfTest)

	if test.done {
		return
	}

	if test.statsCallback != nil {
		test.statsCallback(test)
	}
}

func clientReportTickerProc(data TimerClientData, now time.Time) {
	test := data.p.(*IperfTest)

	if test.done {
		return
	}

	if test.reporterCallback != nil {
		test.reporterCallback(test)
	}
}

func (test *IperfTest) createClientOmitTimer() int {
	// undo, depend on which kind of timer
	return 0
}

func sendTickerProc(data TimerClientData, now time.Time) {
	sp := data.p.(*iperfStream)
	sp.test.checkThrottle(sp, now)
}

func (test *IperfTest) clientEnd() {
	Log.Debugf("Enter client_end")
	for _, sp := range test.streams {
		err := sp.conn.Close()
		if err != nil {
			Log.Errorf("Stream close failed. err = %v", err)

			return
		}
	}

	if test.reporterCallback != nil {
		test.reporterCallback(test)
	}

	test.proto.teardown(test)
	if test.setSendState(IPERF_DONE) < 0 {
		Log.Errorf("set_send_state failed")
	}

	Log.Infof("Client Enter IPerf Done...")

	if test.ctrlConn != nil {
		err := test.ctrlConn.Close()
		if err != nil {
			Log.Errorf("Ctrl conn close failed. err = %v", err)

			return
		}

		test.ctrlChan <- IPERF_DONE // Ensure main loop exits
	}
}

func (test *IperfTest) handleClientCtrlMsg() {
	buf := make([]byte, 4)

	for test.state != IPERF_DONE { // Exit before reading if done
		if n, err := test.ctrlConn.Read(buf); err == nil {
			state := binary.LittleEndian.Uint32(buf[:])

			Log.Debugf("Client Ctrl conn receive n = %v state = [%v]", n, state)

			test.state = uint(state)

			Log.Infof("Client Enter %v state...", test.state)
		} else {
			Log.Errorf("ctrl_conn read failed. err=%T, %v", err, err)

			test.ctrlConn.Close()
			test.ctrlChan <- IPERF_DONE

			return
		}

		switch test.state {
		case IPERF_EXCHANGE_PARAMS:
			if rtn := test.exchangeParams(); rtn < 0 {
				Log.Errorf("exchange_params failed. rtn = %v", rtn)
				test.ctrlChan <- IPERF_DONE

				return
			}
		case IPERF_CREATE_STREAM:
			if rtn := test.createStreams(); rtn < 0 {
				Log.Errorf("create_streams failed. rtn = %v", rtn)
				test.ctrlChan <- IPERF_DONE

				return
			}
		case TEST_START:
			if rtn := test.initTest(); rtn < 0 {
				Log.Errorf("init_test failed. rtn = %v", rtn)
				test.ctrlChan <- IPERF_DONE

				return
			}
			if rtn := test.createClientTimer(); rtn < 0 {
				Log.Errorf("create_client_timer failed. rtn = %v", rtn)
				test.ctrlChan <- IPERF_DONE

				return
			}
			if rtn := test.createClientOmitTimer(); rtn < 0 {
				Log.Errorf("create_client_omit_timer failed. rtn = %v", rtn)
				test.ctrlChan <- IPERF_DONE

				return
			}
			if test.mode == IPERF_SENDER {
				if rtn := test.createSenderTicker(); rtn < 0 {
					Log.Errorf("create_sender_ticker failed. rtn = %v", rtn)
					test.ctrlChan <- IPERF_DONE

					return
				}
			}
		case TEST_RUNNING:
			test.ctrlChan <- TEST_RUNNING
		case IPERF_EXCHANGE_RESULT:
			if rtn := test.exchangeResults(); rtn < 0 {
				Log.Errorf("exchange_results failed. rtn = %v", rtn)
				test.ctrlChan <- IPERF_DONE

				return
			}
		case IPERF_DISPLAY_RESULT:
			test.clientEnd()
		case IPERF_DONE:
			test.ctrlChan <- IPERF_DONE

			return
		case SERVER_TERMINATE:
			oldState := test.state
			test.state = IPERF_DISPLAY_RESULT
			test.reporterCallback(test)
			test.state = oldState
		default:
			Log.Errorf("Unexpected situation with state = %v.", test.state)
			test.ctrlChan <- IPERF_DONE

			return
		}
	}

	test.ctrlChan <- IPERF_DONE // Ensure exit
}

func (test *IperfTest) ConnectServer() int {
	tcpAddr, err := net.ResolveTCPAddr("tcp4", test.addr+":"+strconv.Itoa(int(test.port)))
	if err != nil {
		Log.Errorf("Resolve TCP Addr failed. err = %v, addr = %v", err, test.addr+strconv.Itoa(int(test.port)))

		return -1
	}

	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		Log.Errorf("Connect TCP Addr failed. err = %v, addr = %v", err, test.addr+strconv.Itoa(int(test.port)))

		return -1
	}

	test.ctrlConn = conn
	fmt.Printf("Connect to server %v succeed.\n", test.addr+":"+strconv.Itoa(int(test.port)))

	return 0
}

func (test *IperfTest) runClient() int {

	rtn := test.ConnectServer()
	if rtn < 0 {
		Log.Errorf("ConnectServer failed")

		return -1
	}

	go test.handleClientCtrlMsg()

	var isIperfDone bool = false

	var testEndNum uint = 0

	for isIperfDone != true {
		select {
		case state := <-test.ctrlChan:
			if state == TEST_RUNNING {
				// set non-block for non-UDP test. unfinished
				// Regular mode. Client sends.
				Log.Info("Client enter Test Running state...")
				for i, sp := range test.streams {
					if sp.role == SENDER_STREAM {
						go sp.iperfSend(test)

						Log.Infof("Client Stream %v start sending.", i)
					} else {
						go sp.iperfRecv(test)

						Log.Infof("Client Stream %v start receiving.", i)
					}
				}

				Log.Info("Create all streams finish...")
			} else if state == TEST_END {
				testEndNum++

				if testEndNum < test.streamNum || testEndNum == test.streamNum+1 { // redundant TEST_END signal generate by set_send_state
					continue
				} else if testEndNum > test.streamNum+1 {
					Log.Errorf("Receive more TEST_END signal than expected")

					return -1
				}

				Log.Infof("Client all Stream closed.")

				// test_end_num == test.stream_num. all the stream send TEST_END signal
				test.done = true

				if test.statsCallback != nil {
					test.statsCallback(test)
				}

				if test.setSendState(TEST_END) < 0 {
					Log.Errorf("set_send_state failed. %v", TEST_END)

					return -1
				}

				Log.Info("Client Enter Test End State.")
			} else if state == IPERF_DONE {
				isIperfDone = true
			} else {
				Log.Debugf("Channel Unhandle state [%v]", state)
			}
		}
	}

	return 0
}
