package main

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/op/go-logging"
	//"github.com/gotestyourself/gotest.tools/assert"
	"gotest.tools/assert"
	//"github.com/stretchr/testify/assert"
)

const portServer = 5021
const addrServer = "127.0.0.1:5021"
const addrClient = "127.0.0.1"

var serverTest, clientTest *iperf_test

func init() {

	logging.SetLevel(logging.ERROR, "iperf")
	logging.SetLevel(logging.ERROR, "rudp")
	/* log settting */

	serverTest = newIperfTest()
	clientTest = newIperfTest()
	serverTest.init()
	clientTest.init()

	serverTest.is_server = true
	serverTest.port = portServer

	clientTest.is_server = false
	clientTest.port = portServer
	clientTest.addr = addrClient

	clientTest.interval = 1000 // 1000 ms
	clientTest.duration = 5    // 5 s for test
	clientTest.stream_num = 1  // 1 stream only
	clientTest.setTestReverse(false)

	//TCPSetting()
	RUDPSetting()
	//KCPSetting()

	//client_test.setting.burst = true
	go serverTest.run_server()
	time.Sleep(time.Second)
}

func TCPSetting() {
	clientTest.setProtocol(TCP_NAME)
	clientTest.no_delay = true
	clientTest.setting.blksize = DEFAULT_TCP_BLKSIZE
	clientTest.setting.burst = false
	clientTest.setting.rate = 1024 * 1024 * 1024 * 1024 // b/s
	clientTest.setting.pacing_time = 100                //ms
}

func RUDPSetting() {
	clientTest.setProtocol(RUDP_NAME)
	clientTest.no_delay = false
	clientTest.setting.blksize = DEFAULT_RUDP_BLKSIZE
	clientTest.setting.burst = true
	clientTest.setting.no_cong = false // false for BBR control
	clientTest.setting.snd_wnd = 10
	clientTest.setting.rcv_wnd = 1024
	clientTest.setting.read_buf_size = DEFAULT_READ_BUF_SIZE
	clientTest.setting.write_buf_size = DEFAULT_WRITE_BUF_SIZE
	clientTest.setting.flush_interval = DEFAULT_FLUSH_INTERVAL
	clientTest.setting.data_shards = 3
	clientTest.setting.parity_shards = 1
}

func KCPSetting() {
	clientTest.setProtocol(KCP_NAME)
	clientTest.no_delay = false
	clientTest.setting.blksize = DEFAULT_RUDP_BLKSIZE
	clientTest.setting.burst = true
	clientTest.setting.no_cong = true // false for BBR control
	clientTest.setting.snd_wnd = 512
	clientTest.setting.rcv_wnd = 1024
	clientTest.setting.read_buf_size = DEFAULT_READ_BUF_SIZE
	clientTest.setting.write_buf_size = DEFAULT_WRITE_BUF_SIZE
	clientTest.setting.flush_interval = DEFAULT_FLUSH_INTERVAL
}

func RecvCheckState(t *testing.T, state int) int {
	buf := make([]byte, 4)
	if n, err := clientTest.ctrl_conn.Read(buf); err == nil {
		s := binary.LittleEndian.Uint32(buf[:])
		log.Debugf("Ctrl conn receive n = %v state = [%v]", n, s)
		//s, err := strconv.Atoi(string(buf[:n]))
		if s != uint32(state) {
			log.Errorf("recv state[%v] != expected state[%v]", s, state)
			t.FailNow()
			return -1
		}
		clientTest.state = uint(state)
		log.Infof("Client Enter %v state", clientTest.state)
	}
	return 0
}

func CreateStreams(t *testing.T) int {
	if rtn := clientTest.createStreams(); rtn < 0 {
		log.Errorf("create_streams failed. rtn = %v", rtn)
		return -1
	}
	// check client state
	assert.Equal(t, uint(len(clientTest.streams)), clientTest.stream_num)
	for _, sp := range clientTest.streams {
		assert.Equal(t, sp.test, clientTest)
		if clientTest.mode == IPERF_SENDER {
			assert.Equal(t, sp.role, SENDER_STREAM)
		} else {
			assert.Equal(t, sp.role, RECEIVER_STREAM)
		}
		assert.Assert(t, sp.result != nil)
		assert.Equal(t, sp.can_send, false) // set true after create_send_timer
		assert.Assert(t, sp.conn != nil)
		assert.Assert(t, sp.send_ticker.ticker == nil) // ticker haven't been created yet
	}
	time.Sleep(time.Millisecond * 10) // ensure server side has created all the streams
	// check server state
	assert.Equal(t, uint(len(serverTest.streams)), clientTest.stream_num)

	for _, sp := range serverTest.streams {
		assert.Equal(t, sp.test, serverTest)

		if serverTest.mode == IPERF_SENDER {
			assert.Equal(t, sp.role, SENDER_STREAM)
		} else {
			assert.Equal(t, sp.role, RECEIVER_STREAM)
		}

		assert.Assert(t, sp.result != nil)
		if serverTest.mode == IPERF_SENDER {
			assert.Equal(t, sp.can_send, true)
			if clientTest.setting.burst == true {
				assert.Assert(t, sp.send_ticker.ticker == nil)
			} else {
				assert.Assert(t, sp.send_ticker.ticker != nil)
			}
		} else {
			assert.Equal(t, sp.can_send, false)
			assert.Assert(t, sp.send_ticker.ticker == nil)
		}

		assert.Assert(t, sp.conn != nil)
	}

	return 0
}

func handleTestStart(t *testing.T) int {
	if rtn := clientTest.initTest(); rtn < 0 {
		log.Errorf("init_test failed. rtn = %v", rtn)

		return -1
	}

	if rtn := clientTest.createClientTimer(); rtn < 0 {
		log.Errorf("create_client_timer failed. rtn = %v", rtn)

		return -1
	}

	if rtn := clientTest.createClientOmitTimer(); rtn < 0 {
		log.Errorf("create_client_omit_timer failed. rtn = %v", rtn)

		return -1
	}

	if clientTest.mode == IPERF_SENDER {
		if rtn := clientTest.createSenderTicker(); rtn < 0 {
			log.Errorf("create_client_send_timer failed. rtn = %v", rtn)

			return -1
		}
	}

	// check client
	for _, sp := range clientTest.streams {
		assert.Assert(t, sp.result.start_time.Before(time.Now().Add(time.Duration(time.Millisecond))))
		assert.Assert(t, sp.test.timer.timer != nil)
		assert.Assert(t, sp.test.stats_ticker.ticker != nil)
		assert.Assert(t, sp.test.report_ticker.ticker != nil)

		if clientTest.mode == IPERF_SENDER {
			assert.Equal(t, sp.can_send, true)
			if clientTest.setting.burst == true {
				assert.Assert(t, sp.send_ticker.ticker == nil)
			} else {
				assert.Assert(t, sp.send_ticker.ticker != nil)
			}
		} else {
			assert.Equal(t, sp.can_send, false)
			assert.Assert(t, sp.send_ticker.ticker == nil)
		}
	}

	// check server, should finish test_start process and enter test_running now
	for _, sp := range serverTest.streams {
		assert.Assert(t, sp.result.start_time.Before(time.Now().Add(time.Duration(time.Millisecond))))
		assert.Assert(t, sp.test.timer.timer != nil)
		assert.Assert(t, sp.test.stats_ticker.ticker != nil)
		assert.Assert(t, sp.test.report_ticker.ticker != nil)
		assert.Equal(t, sp.test.state, uint(TEST_RUNNING))
	}

	return 0
}

func handleTestRunning(t *testing.T) int {
	log.Info("Client enter Test Running state...")
	for i, sp := range clientTest.streams {
		if clientTest.mode == IPERF_SENDER {
			go sp.iperfSend(clientTest)
			log.Infof("Stream %v start sending.", i)
		} else {
			go sp.iperfRecv(clientTest)
			log.Infof("Stream %v start receiving.", i)
		}
	}

	log.Info("Client all Stream start. Wait for finish...")
	// wait for send/write end (triggered by timer)
	//for {
	//	if client_test.done {
	//		time.Sleep(time.Millisecond)
	//		break
	//	}
	//}

	for i := 0; i < int(clientTest.stream_num); i++ {
		s := <-clientTest.ctrl_chan
		assert.Equal(t, s, uint(TEST_END))
	}

	log.Infof("Client All Send Stream closed.")

	clientTest.done = true

	if clientTest.stats_callback != nil {
		clientTest.stats_callback(clientTest)
	}

	if clientTest.setSendState(TEST_END) < 0 {
		log.Errorf("set_send_state failed. %v", TEST_END)

		t.FailNow()
	}

	// check client
	assert.Equal(t, clientTest.done, true)
	assert.Assert(t, clientTest.timer.timer == nil)
	assert.Equal(t, clientTest.state, uint(TEST_END))

	var totalBytes uint64

	for _, sp := range clientTest.streams {
		if clientTest.mode == IPERF_SENDER {
			totalBytes += sp.result.bytes_sent
		} else {
			totalBytes += sp.result.bytes_received
		}
	}

	if clientTest.mode == IPERF_SENDER {
		assert.Equal(t, clientTest.bytes_sent, totalBytes)
		assert.Equal(t, clientTest.bytes_received, uint64(0))
	} else {
		assert.Equal(t, clientTest.bytes_received, totalBytes)
		assert.Equal(t, clientTest.bytes_sent, uint64(0))
	}

	time.Sleep(time.Millisecond * 10) // ensure server change state

	// check server
	assert.Equal(t, serverTest.done, true)
	assert.Equal(t, serverTest.state, uint(IPERF_EXCHANGE_RESULT))

	absoluteBytesDiff := int64(serverTest.bytes_received) - int64(clientTest.bytes_sent)
	if absoluteBytesDiff < 0 {
		absoluteBytesDiff = 0 - absoluteBytesDiff
	}

	if float64(absoluteBytesDiff)/float64(clientTest.bytes_sent) > 0.01 { // if bytes difference larger than 1%
		t.FailNow()
	}

	//assert.Equal(t, server_test.bytes_received, client_test.bytes_sent)
	//assert.Equal(t, server_test.blocks_received, client_test.blocks_sent)		// block num not always same
	totalBytes = 0
	for _, sp := range serverTest.streams {
		if serverTest.mode == IPERF_SENDER {
			totalBytes += sp.result.bytes_sent
		} else {
			totalBytes += sp.result.bytes_received
		}
	}

	if serverTest.mode == IPERF_SENDER {
		assert.Equal(t, serverTest.bytes_sent, totalBytes)
		assert.Equal(t, serverTest.bytes_received, uint64(0))
	} else {
		assert.Equal(t, serverTest.bytes_received, totalBytes)
		assert.Equal(t, serverTest.bytes_sent, uint64(0))
	}
	return 0
}

func handleExchangeResult(t *testing.T) int {
	if rtn := clientTest.exchangeResults(); rtn < 0 {
		log.Errorf("exchange_results failed. rtn = %v", rtn)

		return -1
	}

	// check client
	assert.Equal(t, clientTest.done, true)

	for i, sp := range clientTest.streams {
		ssp := serverTest.streams[i]

		assert.Equal(t, sp.result.bytes_received, ssp.result.bytes_received)
		assert.Equal(t, sp.result.bytes_sent, ssp.result.bytes_sent)
	}

	// check server
	assert.Equal(t, serverTest.state, uint(IPERF_DISPLAY_RESULT))

	return 0
}

/*
	Test case can only be run one by one
*/

/*
func TestCtrlConnect(t *testing.T){
	if rtn := client_test.ConnectServer(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_PARAMS)
	if err := client_test.ctrl_conn.Close(); err != nil {
		log.Errorf("close ctrl_conn failed.")
		t.FailNow()
	}
	if err := server_test.ctrl_conn.Close(); err != nil {
		log.Errorf("close ctrl_conn failed.")
		t.FailNow()
	}
}

func TestExchangeParams(t *testing.T){
	if rtn := client_test.ConnectServer(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_PARAMS)
	if rtn := client_test.exchange_params(); rtn < 0 {
		t.FailNow()
	}

	time.Sleep(time.Second)
	assert.Equal(t, server_test.proto.name(), client_test.proto.name())
	assert.Equal(t, server_test.stream_num, client_test.stream_num)
	assert.Equal(t, server_test.duration, client_test.duration)
	assert.Equal(t, server_test.interval, client_test.interval)
	assert.Equal(t, server_test.no_delay, client_test.no_delay)
}

func TestCreateOneStream(t *testing.T){
	// create only one stream
	if rtn := client_test.ConnectServer(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_PARAMS)
	if rtn := client_test.exchange_params(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_CREATE_STREAM)
	CreateStreams(t)
}

func TestCreateMultiStreams(t *testing.T){
	// create multi streams
	if rtn := client_test.ConnectServer(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_PARAMS)
	client_test.stream_num = 5	// change stream_num before exchange params
	if rtn := client_test.exchange_params(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_CREATE_STREAM)
	if rtn := CreateStreams(t); rtn < 0{
		t.FailNow()
	}
}

func TestTestStart(t *testing.T){
	if rtn := client_test.ConnectServer(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_PARAMS)
	if rtn := client_test.exchange_params(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_CREATE_STREAM)
	if rtn := CreateStreams(t); rtn < 0{
		t.FailNow()
	}
	RecvCheckState(t, TEST_START)
	if rtn := handleTestStart(t); rtn < 0{
		t.FailNow()
	}
	RecvCheckState(t, TEST_RUNNING)
}

func TestTestRunning(t *testing.T){
	if rtn := client_test.ConnectServer(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_PARAMS)
	client_test.stream_num = 2
	if rtn := client_test.exchange_params(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_CREATE_STREAM)
	if rtn := CreateStreams(t); rtn < 0{
		t.FailNow()
	}
	RecvCheckState(t, TEST_START)
	if rtn := handleTestStart(t); rtn < 0{
		t.FailNow()
	}
	RecvCheckState(t, TEST_RUNNING)
	if handleTestRunning(t) < 0{
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_RESULT)
}

func TestExchangeResult(t *testing.T){
	if rtn := client_test.ConnectServer(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_PARAMS)
	client_test.stream_num = 2
	if rtn := client_test.exchange_params(); rtn < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_CREATE_STREAM)
	if rtn := CreateStreams(t); rtn < 0{
		t.FailNow()
	}
	RecvCheckState(t, TEST_START)
	if rtn := handleTestStart(t); rtn < 0{
		t.FailNow()
	}
	RecvCheckState(t, TEST_RUNNING)
	if handleTestRunning(t) < 0{
		t.FailNow()
	}
	RecvCheckState(t, IPERF_EXCHANGE_RESULT)
	if handleExchangeResult(t) < 0 {
		t.FailNow()
	}
	RecvCheckState(t, IPERF_DISPLAY_RESULT)
}
*/

func TestDisplayResult(t *testing.T) {
	if rtn := clientTest.ConnectServer(); rtn < 0 {
		t.FailNow()
	}

	RecvCheckState(t, IPERF_EXCHANGE_PARAMS)
	//client_test.stream_num = 2
	if rtn := clientTest.exchangeParams(); rtn < 0 {
		t.FailNow()
	}

	RecvCheckState(t, IPERF_CREATE_STREAM)
	if rtn := CreateStreams(t); rtn < 0 {
		t.FailNow()
	}

	RecvCheckState(t, TEST_START)
	if rtn := handleTestStart(t); rtn < 0 {
		t.FailNow()
	}

	RecvCheckState(t, TEST_RUNNING)
	if handleTestRunning(t) < 0 {
		t.FailNow()
	}

	RecvCheckState(t, IPERF_EXCHANGE_RESULT)
	if handleExchangeResult(t) < 0 {
		t.FailNow()
	}

	RecvCheckState(t, IPERF_DISPLAY_RESULT)

	clientTest.clientEnd()

	time.Sleep(time.Millisecond * 10) // wait for server
	assert.Equal(t, clientTest.state, uint(IPERF_DONE))
	assert.Equal(t, serverTest.state, uint(IPERF_DONE))
	// check output with your own eyes

	time.Sleep(time.Second * 5) // wait for server
}
