package iperf

import "time"

type ITimer struct {
	timer *time.Timer
	done  chan bool
}

type ITicker struct {
	ticker *time.Ticker
	done   chan bool
}

type TimerClientData struct {
	p interface{}
}
type timerProc func(data TimerClientData, now time.Time)

func timerCreate(now time.Time, proc timerProc, data TimerClientData, dur uint /* in ms */) ITimer {
	realDur := time.Now().Sub(now) + time.Duration(dur)*time.Millisecond
	timer := time.NewTimer(realDur)

	done := make(chan bool, 1)

	go func() {
		defer timer.Stop()

		for {
			select {
			case <-done:
				Log.Debugf("Timer recv done. dur: %v", dur)

				return
			case t := <-timer.C:
				proc(data, t)
			}
		}
	}()

	itimer := ITimer{timer: timer, done: done}

	return itimer
}

func tickerCreate(_ time.Time, proc timerProc, data TimerClientData, interval uint /* in ms */, max_times uint) ITicker {
	ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)

	done := make(chan bool, 1)

	go func() {
		var cnt uint = 0

		defer ticker.Stop()

		for {
			select {
			case <-done:
				Log.Debugf("Ticker recv done. interval:%v", interval)

				return
			case t := <-ticker.C:
				if cnt >= max_times {
					return
				}
				proc(data, t)
				cnt++
			}
		}
	}()

	iticker := ITicker{ticker: ticker, done: done}

	return iticker
}
