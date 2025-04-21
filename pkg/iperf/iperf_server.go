package iperf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

func (test *IperfTest) serverListen() int {
	listenAddr := ":"
	listenAddr += strconv.Itoa(int(test.port))

	var err error

	test.listener, err = net.Listen("tcp", listenAddr)

	if err != nil {
		return -1
	}
	fmt.Printf("Server listening on %v\n", test.port)

	return 0
}

func (test *IperfTest) handleServerCtrlMsg() {
	buf := make([]byte, 4) // only for ctrl state

	defer func() {
		// Ensure control connection is closed
		if test.ctrlConn != nil {
			test.ctrlConn.Close()
		}
	}()

	for {
		if n, err := test.ctrlConn.Read(buf); err == nil {
			state := binary.LittleEndian.Uint32(buf[:])

			Log.Debugf("Ctrl conn receive n = %v state = [%v]", n, state)

			test.state = uint(state)
		} else {
			var serr *net.OpError

			if errors.As(err, &serr) {
				Log.Info("Client control connection close. err = %T %v", serr, serr)
			}

			return
		}

		switch test.state {
		case TEST_START:
			break
		case TEST_END:
			Log.Infof("Server Enter Test End state...")

			test.done = true

			if test.statsCallback != nil {
				test.statsCallback(test)
			}

			test.closeAllStreams()

			/* exchange result mode */
			if test.setSendState(IPERF_EXCHANGE_RESULT) < 0 {
				Log.Errorf("set_send_state error")
				return
			}

			Log.Infof("Server Enter Exchange Result state...")

			if test.exchangeResults() < 0 {
				Log.Errorf("exchange result failed")
				return
			}

			/* display result mode */
			if test.setSendState(IPERF_DISPLAY_RESULT) < 0 {
				Log.Errorf("set_send_state error")
				return
			}

			Log.Infof("Server Enter Display Result state...")
			if test.reporterCallback != nil {
				test.reporterCallback(test)
			}

			// After displaying results, move to DONE state
			if test.setSendState(IPERF_DONE) < 0 {
				Log.Errorf("set_send_state error")
				return
			}

		case IPERF_DONE:
			test.state = IPERF_DONE
			Log.Debugf("Server reach IPERF_DONE")

			// Clean up resources
			if test.timer.timer != nil {
				test.timer.timer.Stop()
			}
			if test.statsTicker.ticker != nil {
				test.statsTicker.ticker.Stop()
			}
			if test.reportTicker.ticker != nil {
				test.reportTicker.ticker.Stop()
			}

			test.proto.teardown(test)
			test.ctrlChan <- IPERF_DONE
			return

		case CLIENT_TERMINATE:
			oldState := test.state

			test.state = IPERF_DISPLAY_RESULT
			test.reporterCallback(test)
			test.state = oldState

			test.closeAllStreams()

			Log.Infof("Client is terminated.")

			test.state = IPERF_DONE
			test.ctrlChan <- IPERF_DONE
			return

		default:
			Log.Errorf("Unexpected situation with state = %v.", test.state)
			test.ctrlChan <- IPERF_DONE
			return
		}
	}
}

func (test *IperfTest) createServerTimer() int {
	now := time.Now()

	cd := TimerClientData{p: test}

	test.timer = timerCreate(now, serverTimerProc, cd, (test.duration+5)*1000) // convert sec to ms, add 5 sec to ensure client end first

	times := test.duration * 1000 / test.interval

	test.statsTicker = tickerCreate(now, serverStatsTickerProc, cd, test.interval, times-1)
	test.reportTicker = tickerCreate(now, serverReportTickerProc, cd, test.interval, times-1)

	if test.timer.timer == nil || test.statsTicker.ticker == nil || test.reportTicker.ticker == nil {
		Log.Error("timer create failed.")
	}

	return 0
}

func serverTimerProc(data TimerClientData, now time.Time) {
	Log.Debugf("Enter server_timer_proc")

	test := data.p.(*IperfTest)

	if test.done {
		return
	}

	test.done = true

	// close all streams
	for _, sp := range test.streams {
		err := sp.conn.Close()
		if err != nil {
			Log.Errorf("Close stream conn failed. err = %v", err)

			return
		}
	}

	test.timer.done <- true
	//test.ctrl_conn.Close()		//  ctrl conn should be closed at last
	//log.Infof("Server exceed duration. Close control connection.")
}

func serverStatsTickerProc(data TimerClientData, now time.Time) {
	test := data.p.(*IperfTest)

	if test.done {
		return
	}

	if test.statsCallback != nil {
		test.statsCallback(test)
	}
}

func serverReportTickerProc(data TimerClientData, now time.Time) {
	test := data.p.(*IperfTest)

	if test.done {
		return
	}

	if test.reporterCallback != nil {
		test.reporterCallback(test)
	}
}

func (test *IperfTest) createServerOmitTimer() int {
	// undo, depend on which kind of timer
	return 0
}

func (test *IperfTest) runServer() int {
	Log.Debugf("Enter run_server")

	if test.serverListen() < 0 {
		Log.Error("Listen failed")
		return -1
	}

	for {
		// Reset test state for new connection
		test.state = IPERF_START
		test.done = false
		test.streams = nil // Clear previous streams
		test.bytesReceived = 0
		test.blocksReceived = 0
		test.bytesSent = 0
		test.blocksSent = 0
		test.ctrlChan = make(chan uint, 5) // Create new control channel

		Log.Info("Enter Iperf start state...")

		// start
		conn, err := test.listener.Accept()
		if err != nil {
			Log.Error("Accept failed")
			return -2
		}

		test.ctrlConn = conn

		fmt.Printf("Accept connection from client: %v\n", conn.RemoteAddr())
		// exchange params
		if test.setSendState(IPERF_EXCHANGE_PARAMS) < 0 {
			Log.Error("set_send_state error.")
			test.ctrlConn.Close()
			continue
		}

		Log.Info("Enter Exchange Params state...")

		if test.exchangeParams() < 0 {
			Log.Error("exchange params failed.")
			test.ctrlConn.Close()
			continue
		}

		ctrlDone := make(chan struct{})
		go func() {
			test.handleServerCtrlMsg()
			close(ctrlDone)
		}()

		if test.isServer == true {
			listener, err := test.proto.listen(test)
			if err != nil {
				Log.Error("proto listen error.")
				test.ctrlConn.Close()
				<-ctrlDone // Wait for handleServerCtrlMsg to finish
				continue
			}

			test.protoListener = listener
		}

		// create streams
		if test.setSendState(IPERF_CREATE_STREAM) < 0 {
			Log.Error("set_send_state error.")
			test.ctrlConn.Close()
			<-ctrlDone // Wait for handleServerCtrlMsg to finish
			continue
		}

		Log.Info("Enter Create Stream state...")

	ServerLoop:
		for {
			select {
			case state := <-test.ctrlChan:
				Log.Debugf("Ctrl channel receive state [%v]", state)

				if state == IPERF_DONE {
					Log.Info("Test completed, waiting for next client...")
					<-ctrlDone // Wait for handleServerCtrlMsg to finish
					break ServerLoop
				} else if state == IPERF_CREATE_STREAM {
					var streamNum uint = 0

					for streamNum < test.streamNum {
						protoConn, err := test.proto.accept(test)
						if err != nil {
							Log.Error("proto accept error.")
							test.ctrlConn.Close()
							<-ctrlDone // Wait for handleServerCtrlMsg to finish
							break ServerLoop
						}

						streamNum++

						var sp *iperfStream

						if test.mode == IPERF_SENDER {
							sp = test.newStream(protoConn, SENDER_STREAM)
						} else {
							sp = test.newStream(protoConn, RECEIVER_STREAM)
						}

						if sp == nil {
							Log.Error("Create new stream failed.")
							test.ctrlConn.Close()
							<-ctrlDone // Wait for handleServerCtrlMsg to finish
							break ServerLoop
						}

						test.streams = append(test.streams, sp)

						Log.Debugf("create new stream, stream_num = %v, target stream num = %v", streamNum, test.streamNum)
					}

					if streamNum == test.streamNum {
						if test.setSendState(TEST_START) != 0 {
							Log.Errorf("set_send_state error")
							test.ctrlConn.Close()
							<-ctrlDone // Wait for handleServerCtrlMsg to finish
							break ServerLoop
						}

						Log.Info("Enter Test Start state...")

						if test.initTest() < 0 {
							Log.Errorf("Init test failed.")
							test.ctrlConn.Close()
							<-ctrlDone // Wait for handleServerCtrlMsg to finish
							break ServerLoop
						}

						if test.createServerTimer() < 0 {
							Log.Errorf("Create Server timer failed.")
							test.ctrlConn.Close()
							<-ctrlDone // Wait for handleServerCtrlMsg to finish
							break ServerLoop
						}

						if test.createServerOmitTimer() < 0 {
							Log.Errorf("Create Server Omit timer failed.")
							test.ctrlConn.Close()
							<-ctrlDone // Wait for handleServerCtrlMsg to finish
							break ServerLoop
						}

						if test.mode == IPERF_SENDER {
							if rtn := test.createSenderTicker(); rtn < 0 {
								Log.Errorf("create_sender_ticker failed. rtn = %v", rtn)
								test.ctrlConn.Close()
								<-ctrlDone // Wait for handleServerCtrlMsg to finish
								break ServerLoop
							}
						}

						if test.setSendState(TEST_RUNNING) != 0 {
							Log.Errorf("set_send_state error")
							test.ctrlConn.Close()
							<-ctrlDone // Wait for handleServerCtrlMsg to finish
							break ServerLoop
						}
					}
				} else if state == TEST_RUNNING {
					// Regular mode. Server receives.
					Log.Info("Enter Test Running state...")

					for i, sp := range test.streams {
						if sp.role == SENDER_STREAM {
							go sp.iperfSend(test)
							Log.Infof("Server Stream %v start sending.", i)
						} else {
							go sp.iperfRecv(test)
							Log.Infof("Server Stream %v start receiving.", i)
						}
					}

					Log.Info("Server all streams start...")
				} else if state == TEST_END {
					continue
				} else {
					Log.Debugf("Channel Unhandle state [%v]", state)
				}
			case <-ctrlDone:
				// Control message handler has finished
				break ServerLoop
			}
		}

		// Clean up before accepting new connection
		if test.protoListener != nil && test.protoListener != test.listener {
			test.protoListener.Close()
		}
	}
}
