package elevator

import "time"

type cmdStart struct{ d time.Duration}
type cmdStop struct{}
type cmdTimedOutNow struct{ reply chan bool }

type Timer struct {
	cmd chan any
	out chan struct{}
}

// Creates a timer controller
func New() *Timer {
	t := &Timer{
		cmd: make(chan any),
		out: make(chan struct{}, 1),
	}
	go t.loop()
	return t
}


// Start/restart the timer duration in sec.
func (t *Timer) start(durationSec float64) {
	d := time.Duration(durationSec * float64(time.Second))
	t.cmd <- cmdStart{d: d}
}


// Stops the timer
func (t *Timer) Stop() {
	t.cmd <- cmdStop{}
}

// returns a channel that revices an event each time the timer times out
func (t *Timer) TimedOutC() <-chan struct{} {
	return t.out
}

//optional poll-like check 
func (t *Timer) TimedOutNow() bool {
	reply := make(chan bool, 1)
	t.cmd <- cmdTimedOutNow{reply: reply}
	return <-reply
}

//this idont iknow yet ggwp trying to understand
func (t *Timer) loop() {
	var (
		active bool
		endTime time.Time
		timer *time.Timer
		timerC <-chan time.Time //0 when inactive
	)

	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}

		timer = nil
		timerC = nil

	}

	for {
		select {
		case msg := <-t.cmd:
			switch m := msg.(type) {

			case cmdStart:
				active = true
				endTime = time.Now().Add(m.d)

				stopTimer()
				timer = time.NewTimer(m.d)
				timerC = timer.C 

			case cmdStop:
				active = false
				stopTimer()
			
			case cmdTimedOutNow:
				m.reply <- (active && time.Now().After(endTime))

			}

		case <-timerC:

			active = false
			stopTimer()

			select {
			case t.out <- struct{}{}:
			default:
			}
		}
	}
}

/* how you use it: 
t := timer.New()

t.Start(3.0) // 3 seconds

select {
case <-t.TimedOutC():
	// timed out
case <-time.After(1 * time.Second):
	// something else
}
*/