package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

func (test *iperfTest) server_listen() int {
	listen_addr := ":"
	listen_addr += strconv.Itoa(int(test.port))
	var err error
	test.listener, err = net.Listen("tcp", listen_addr)
	if err != nil {
		return -1
	}
	fmt.Printf("Server listening on %v\n", test.port)
	return 0
}

func (test *iperfTest) handleServerCtrlMsg() {
	buf := make([]byte, 4) // only for ctrl state
	for {
		if n, err := test.ctrlConn.Read(buf); err == nil {
			state := binary.LittleEndian.Uint32(buf[:])
			log.Debugf("Ctrl conn receive n = %v state = [%v]", n, state)
			//if err != nil {
			//	log.Errorf("Convert string to int failed. s = %v", string(buf[:n]))
			//	return
			//}
			test.state = uint(state)
		} else {
			if serr, ok := err.(*net.OpError); ok {
				log.Info("Client control connection close. err = %T %v", serr, serr)
				test.ctrlConn.Close()
			} else if err == os.ErrClosed || err == io.ErrClosedPipe || err == io.EOF {
				log.Info("Client control connection close. err = %T %v", serr, serr)
			} else {
				log.Errorf("ctrl_conn read failed. err=%T, %v", err, err)
				test.ctrlConn.Close()
			}
			return
		}

		switch test.state {
		case TEST_START:
			break
		case TEST_END:
			log.Infof("Server Enter Test End state...")
			test.done = true
			if test.statsCallback != nil {
				test.statsCallback(test)
			}
			test.closeAllStreams()

			/* exchange result mode */
			if test.setSendState(IPERF_EXCHANGE_RESULT) < 0 {
				log.Errorf("set_send_state error")
				return
			}
			log.Infof("Server Enter Exchange Result state...")
			if test.exchangeResults() < 0 {
				log.Errorf("exchange result failed")
				return
			}

			/* display result mode */
			if test.setSendState(IPERF_DISPLAY_RESULT) < 0 {
				log.Errorf("set_send_state error")
				return
			}
			log.Infof("Server Enter Display Result state...")
			if test.reporterCallback != nil { // why call these again
				test.reporterCallback(test)
			}
			//if test.display_results() < 0 {
			//	log.Errorf("display result failed")
			//	return
			//}
			// on_test_finish undo
		case IPERF_DONE:
			test.state = IPERF_DONE
			log.Debugf("Server reach IPERF_DONE")
			test.ctrlChan <- IPERF_DONE
			test.proto.teardown(test)
			return
		case CLIENT_TERMINATE: //not used yet
			old_state := test.state
			test.state = IPERF_DISPLAY_RESULT
			test.reporterCallback(test)
			test.state = old_state

			test.closeAllStreams()
			log.Infof("Client is terminated.")
			test.state = IPERF_DONE
			break
		default:
			log.Errorf("Unexpected situation with state = %v.", test.state)
			return
		}
	}
}

func (test *iperfTest) create_server_timer() int {
	now := time.Now()
	cd := TimerClientData{p: test}
	test.timer = timer_create(now, server_timer_proc, cd, (test.duration+5)*1000) // convert sec to ms, add 5 sec to ensure client end first
	times := test.duration * 1000 / test.interval
	test.statsTicker = ticker_create(now, server_stats_ticker_proc, cd, test.interval, times-1)
	test.reportTicker = ticker_create(now, server_report_ticker_proc, cd, test.interval, times-1)
	if test.timer.timer == nil || test.statsTicker.ticker == nil || test.reportTicker.ticker == nil {
		log.Error("timer create failed.")
	}
	return 0
}

func server_timer_proc(data TimerClientData, now time.Time) {
	log.Debugf("Enter server_timer_proc")
	test := data.p.(*iperfTest)
	if test.done {
		return
	}
	test.done = true
	// close all streams
	for _, sp := range test.streams {
		sp.conn.Close()
	}
	test.timer.done <- true
	//test.ctrl_conn.Close()		//  ctrl conn should be closed at last
	//log.Infof("Server exceed duration. Close control connection.")
}

func server_stats_ticker_proc(data TimerClientData, now time.Time) {
	test := data.p.(*iperfTest)
	if test.done {
		return
	}
	if test.statsCallback != nil {
		test.statsCallback(test)
	}
}

func server_report_ticker_proc(data TimerClientData, now time.Time) {
	test := data.p.(*iperfTest)
	if test.done {
		return
	}
	if test.reporterCallback != nil {
		test.reporterCallback(test)
	}
}

func (test *iperfTest) create_server_omit_timer() int {
	// undo, depend on which kind of timer
	return 0
}

func (test *iperfTest) run_server() int {
	log.Debugf("Enter run_server")

	if test.server_listen() < 0 {
		log.Error("Listen failed")
		return -1
	}
	test.state = IPERF_START
	log.Info("Enter Iperf start state...")
	// start
	conn, err := test.listener.Accept()
	if err != nil {
		log.Error("Accept failed")
		return -2
	}
	test.ctrlConn = conn
	fmt.Printf("Accept connection from client: %v\n", conn.RemoteAddr())
	// exchange params
	if test.setSendState(IPERF_EXCHANGE_PARAMS) < 0 {
		log.Error("set_send_state error.")
		return -3
	}
	log.Info("Enter Exchange Params state...")

	if test.exchangeParams() < 0 {
		log.Error("exchange params failed.")
		return -3
	}

	go test.handleServerCtrlMsg() // coroutine handle control msg

	if test.isServer == true {
		listener, err := test.proto.listen(test)
		if err != nil {
			log.Error("proto listen error.")
			return -4
		}
		test.protoListener = listener
	}

	// create streams
	if test.setSendState(IPERF_CREATE_STREAM) < 0 {
		log.Error("set_send_state error.")
		return -3
	}
	log.Info("Enter Create Stream state...")

	var is_iperf_done bool = false
	for is_iperf_done != true {
		select {
		case state := <-test.ctrlChan:
			log.Debugf("Ctrl channel receive state [%v]", state)
			if state == IPERF_DONE {
				return 0
			} else if state == IPERF_CREATE_STREAM {
				var stream_num uint = 0
				for stream_num < test.streamNum {
					proto_conn, err := test.proto.accept(test)
					if err != nil {
						log.Error("proto accept error.")
						return -4
					}
					stream_num++
					var sp *iperfStream
					if test.mode == IPERF_SENDER {
						sp = test.newStream(proto_conn, SENDER_STREAM)
					} else {
						sp = test.newStream(proto_conn, RECEIVER_STREAM)
					}

					if sp == nil {
						log.Error("Create new strema failed.")
						return -4
					}
					test.streams = append(test.streams, sp)
					log.Debugf("create new stream, stream_num = %v, target stream num = %v", stream_num, test.streamNum)
				}
				if stream_num == test.streamNum {
					if test.setSendState(TEST_START) != 0 {
						log.Errorf("set_send_state error")
						return -5
					}
					log.Info("Enter Test Start state...")
					if test.initTest() < 0 {
						log.Errorf("Init test failed.")
						return -5
					}
					if test.create_server_timer() < 0 {
						log.Errorf("Create Server timer failed.")
						return -6
					}
					if test.create_server_omit_timer() < 0 {
						log.Errorf("Create Server Omit timer failed.")
						return -7
					}
					if test.mode == IPERF_SENDER {
						if rtn := test.createSenderTicker(); rtn < 0 {
							log.Errorf("create_sender_ticker failed. rtn = %v", rtn)
							return -7
						}
					}
					if test.setSendState(TEST_RUNNING) != 0 {
						log.Errorf("set_send_state error")
						return -8
					}
				}
			} else if state == TEST_RUNNING {
				// Regular mode. Server receives.
				log.Info("Enter Test Running state...")
				for i, sp := range test.streams {
					if sp.role == SENDER_STREAM {
						go sp.iperfSend(test)
						log.Infof("Server Stream %v start sending.", i)
					} else {
						go sp.iperfRecv(test)
						log.Infof("Server Stream %v start receiving.", i)
					}
				}
				log.Info("Server all streams start...")
			} else if state == TEST_END {
				continue
			} else if state == IPERF_DONE {
				is_iperf_done = true
			} else {
				log.Debugf("Channel Unhandle state [%v]", state)
			}
		}
	}
	log.Debugf("Server side done.")
	return 0
}
