package main

import (
	"bytes"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"
)

const (
	nFloors      = 4
	nElev        = 3
	speed        = 0.4 // floors per second
	epsilon      = 0.05
	btnReleaseDur = 200 * time.Millisecond
)

var ports = [nElev]string{"15657", "15658", "15659"}

type ElevState struct {
	mu sync.Mutex

	motorDir   int8
	position   float64
	atFloor    int
	floorIndic int

	doorOpen bool
	stopLamp bool

	// lamps set by elevator program
	buttonLamps [nFloors][3]bool // [floor][0=hallUp, 1=hallDown, 2=cab]

	// inputs read by elevator program
	hallButtons    [nFloors][2]bool
	cabButtons     [nFloors]bool
	hallBtnRelease [nFloors][2]time.Time
	cabBtnRelease  [nFloors]time.Time

	obstruction bool
	motorStop   bool
	connected   bool
}

var elevs [nElev]ElevState
var selected int // 0-based, protected by selectedMu
var selectedMu sync.Mutex

func main() {
	// init elevators
	for i := range nElev {
		elevs[i].atFloor = 0
		elevs[i].position = 0.0
		elevs[i].floorIndic = 0
	}

	// start TCP servers
	for i := range nElev {
		go tcpServer(i)
	}

	// start physics goroutines
	for i := range nElev {
		go physics(i)
	}

	// start renderer
	go render()

	// handle OS signals for clean exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		restoreTerminal()
		fmt.Print("\033[2J\033[H")
		os.Exit(0)
	}()

	// keyboard loop runs in main goroutine
	keyboard()
}

// ── TCP server ────────────────────────────────────────────────────────────────

func tcpServer(idx int) {
	ln, err := net.Listen("tcp", ":"+ports[idx])
	if err != nil {
		fmt.Fprintf(os.Stderr, "elev%d: listen error: %v\n", idx+1, err)
		return
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}

		elevs[idx].mu.Lock()
		elevs[idx].connected = true
		elevs[idx].mu.Unlock()

		handleConn(idx, conn)

		elevs[idx].mu.Lock()
		elevs[idx].connected = false
		elevs[idx].motorDir = 0
		elevs[idx].mu.Unlock()
	}
}

func handleConn(idx int, conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 4)
	for {
		if _, err := readFull(conn, buf); err != nil {
			return
		}
		cmd := buf[0]
		switch cmd {
		case 1: // SetMotorDirection
			dir := int8(buf[1])
			elevs[idx].mu.Lock()
			if !elevs[idx].motorStop {
				elevs[idx].motorDir = dir
			}
			elevs[idx].mu.Unlock()

		case 2: // SetButtonLamp
			btn := int(buf[1])
			floor := int(buf[2])
			val := buf[3] != 0
			elevs[idx].mu.Lock()
			if floor >= 0 && floor < nFloors && btn >= 0 && btn < 3 {
				elevs[idx].buttonLamps[floor][btn] = val
			}
			elevs[idx].mu.Unlock()

		case 3: // SetFloorIndicator
			floor := int(buf[1])
			elevs[idx].mu.Lock()
			if floor >= 0 && floor < nFloors {
				elevs[idx].floorIndic = floor
			}
			elevs[idx].mu.Unlock()

		case 4: // SetDoorOpenLamp
			elevs[idx].mu.Lock()
			elevs[idx].doorOpen = buf[1] != 0
			elevs[idx].mu.Unlock()

		case 5: // SetStopLamp
			elevs[idx].mu.Lock()
			elevs[idx].stopLamp = buf[1] != 0
			elevs[idx].mu.Unlock()

		case 6: // GetButton → [0, val, 0, 0]
			btn := int(buf[1])
			floor := int(buf[2])
			elevs[idx].mu.Lock()
			var val bool
			if floor >= 0 && floor < nFloors {
				if btn == 2 {
					val = elevs[idx].cabButtons[floor]
				} else if btn >= 0 && btn < 2 {
					val = elevs[idx].hallButtons[floor][btn]
				}
			}
			elevs[idx].mu.Unlock()
			resp := [4]byte{0, 0, 0, 0}
			if val {
				resp[1] = 1
			}
			conn.Write(resp[:]) //nolint

		case 7: // GetFloor → [0, atFloor, floor, 0]
			elevs[idx].mu.Lock()
			af := elevs[idx].atFloor
			elevs[idx].mu.Unlock()
			resp := [4]byte{0, 0, 0, 0}
			if af >= 0 {
				resp[1] = 1
				resp[2] = byte(af)
			}
			conn.Write(resp[:]) //nolint

		case 8: // GetStop → [0, val, 0, 0]
			elevs[idx].mu.Lock()
			s := elevs[idx].stopLamp
			elevs[idx].mu.Unlock()
			resp := [4]byte{0, 0, 0, 0}
			if s {
				resp[1] = 1
			}
			conn.Write(resp[:]) //nolint

		case 9: // GetObstruction → [0, val, 0, 0]
			elevs[idx].mu.Lock()
			o := elevs[idx].obstruction
			elevs[idx].mu.Unlock()
			resp := [4]byte{0, 0, 0, 0}
			if o {
				resp[1] = 1
			}
			conn.Write(resp[:]) //nolint
		}
	}
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// ── Physics ───────────────────────────────────────────────────────────────────

func physics(idx int) {
	ticker := time.NewTicker(10 * time.Millisecond)
	prev := time.Now()
	for t := range ticker.C {
		dt := t.Sub(prev).Seconds()
		prev = t

		e := &elevs[idx]
		e.mu.Lock()

		// auto-release hall buttons
		now := time.Now()
		for f := range nFloors {
			for b := range 2 {
				if e.hallButtons[f][b] && !e.hallBtnRelease[f][b].IsZero() && now.After(e.hallBtnRelease[f][b]) {
					e.hallButtons[f][b] = false
					e.hallBtnRelease[f][b] = time.Time{}
				}
			}
			if e.cabButtons[f] && !e.cabBtnRelease[f].IsZero() && now.After(e.cabBtnRelease[f]) {
				e.cabButtons[f] = false
				e.cabBtnRelease[f] = time.Time{}
			}
		}

		// movement: only when connected and not stopped
		if e.connected && !e.motorStop && e.motorDir != 0 {
			e.position += float64(e.motorDir) * speed * dt
			if e.position < 0.0 {
				e.position = 0.0
			}
			if e.position > float64(nFloors-1) {
				e.position = float64(nFloors - 1)
			}
		}

		// floor sensor
		rounded := math.Round(e.position)
		if math.Abs(e.position-rounded) < epsilon {
			e.atFloor = int(rounded)
		} else {
			e.atFloor = -1
		}

		e.mu.Unlock()
	}
}

// ── Render ────────────────────────────────────────────────────────────────────

func render() {
	ticker := time.NewTicker(33 * time.Millisecond) // ~30 fps
	for range ticker.C {
		drawFrame()
	}
}

// W is the inner column width (between | chars). 3 cols = 3*W+4 total.
// W=22 → 70 chars total, fits in 80-col terminal.
const W = 22

const sep = "+----------------------+----------------------+----------------------+"

func lamp(on bool) string {
	if on {
		return "[*]"
	}
	return "[ ]"
}

// col formats s to exactly W chars: left-aligned, padded or truncated.
func col(s string) string {
	switch {
	case len(s) == W:
		return s
	case len(s) > W:
		return s[:W]
	default:
		return fmt.Sprintf("%-*s", W, s)
	}
}

type snap struct {
	motorDir   int8
	position   float64
	atFloor    int
	floorIndic int
	doorOpen   bool
	stopLamp   bool
	lamps      [nFloors][3]bool
	hall       [nFloors][2]bool
	cab        [nFloors]bool
	obstruct   bool
	motorStop  bool
	connected  bool
}

func drawFrame() {
	var s [nElev]snap
	for i := range nElev {
		elevs[i].mu.Lock()
		s[i] = snap{
			motorDir:   elevs[i].motorDir,
			position:   elevs[i].position,
			atFloor:    elevs[i].atFloor,
			floorIndic: elevs[i].floorIndic,
			doorOpen:   elevs[i].doorOpen,
			stopLamp:   elevs[i].stopLamp,
			lamps:      elevs[i].buttonLamps,
			hall:       elevs[i].hallButtons,
			cab:        elevs[i].cabButtons,
			obstruct:   elevs[i].obstruction,
			motorStop:  elevs[i].motorStop,
			connected:  elevs[i].connected,
		}
		elevs[i].mu.Unlock()
	}

	selectedMu.Lock()
	sel := selected
	selectedMu.Unlock()

	var buf bytes.Buffer
	row := func(a, b, c string) {
		fmt.Fprintf(&buf, "|%s|%s|%s|\n", col(a), col(b), col(c))
	}

	buf.WriteString("\033[H\033[2J")
	fmt.Fprintf(&buf, "Elevator Simulator  [Elev %d selected]\n\n", sel+1)
	buf.WriteString(sep + "\n")

	// title row
	titles := [nElev]string{}
	for i := range nElev {
		st := "CONNECTED"
		if !s[i].connected {
			st = "waiting..."
		}
		titles[i] = fmt.Sprintf(" E%d:%-5s %s", i+1, ports[i], st)
	}
	row(titles[0], titles[1], titles[2])
	buf.WriteString(sep + "\n")

	// column header
	hdr := " Fl  Dn   Up   Cab"
	row(hdr, hdr, hdr)

	// floor rows, top to bottom
	for f := nFloors - 1; f >= 0; f-- {
		cells := [nElev]string{}
		for i := range nElev {
			arrow := "  "
			if math.Abs(s[i].position-float64(f)) < 0.2 {
				switch {
				case s[i].motorDir > 0:
					arrow = ">>"
				case s[i].motorDir < 0:
					arrow = "<<"
				default:
					arrow = "--"
				}
			}
			cells[i] = fmt.Sprintf(" %s%d %s %s %s",
				arrow, f,
				lamp(s[i].lamps[f][1]),
				lamp(s[i].lamps[f][0]),
				lamp(s[i].lamps[f][2]))
		}
		row(cells[0], cells[1], cells[2])
	}
	buf.WriteString(sep + "\n")

	// position / direction row
	posRow := [nElev]string{}
	for i := range nElev {
		dir := "Idle"
		if s[i].motorDir > 0 {
			dir = "Up"
		} else if s[i].motorDir < 0 {
			dir = "Dn"
		}
		posRow[i] = fmt.Sprintf(" %.2f %-4s Fl%d", s[i].position, dir, s[i].floorIndic)
	}
	row(posRow[0], posRow[1], posRow[2])

	// door / obstruction row
	doorRow := [nElev]string{}
	for i := range nElev {
		door := "closed"
		if s[i].doorOpen {
			door = "OPEN  "
		}
		obs := "OFF"
		if s[i].obstruct {
			obs = "ON "
		}
		doorRow[i] = fmt.Sprintf(" Dr:%-6s Ob:%s", door, obs)
	}
	row(doorRow[0], doorRow[1], doorRow[2])

	// motor stop row
	motorRow := [nElev]string{}
	for i := range nElev {
		mtr := "OK"
		if s[i].motorStop {
			mtr = "STOPPED"
		}
		motorRow[i] = fmt.Sprintf(" Motor: %s", mtr)
	}
	row(motorRow[0], motorRow[1], motorRow[2])
	buf.WriteString(sep + "\n")

	buf.WriteString("\n [1/2/3] Select elev\n")
	buf.WriteString(" [q/w/e/r] HallUp 0-3  [a/s/d/f] HallDn 0-3\n")
	buf.WriteString(" [z/x/c/v] Cab 0-3     [o] Obstr  [m] Motor  [Q] Quit\n")

	os.Stdout.Write(buf.Bytes())
}

// ── Keyboard ──────────────────────────────────────────────────────────────────

var sttyFlag string

func restoreTerminal() {
	exec.Command("stty", sttyFlag, "/dev/tty", "sane").Run() //nolint
	fmt.Print("\033[?25h")                                    // show cursor
}

func keyboard() {
	if runtime.GOOS == "darwin" {
		sttyFlag = "-f"
	} else {
		sttyFlag = "-F"
	}
	exec.Command("stty", sttyFlag, "/dev/tty", "raw", "-echo").Run() //nolint
	fmt.Print("\033[?25l")                                            // hide cursor
	defer restoreTerminal()

	tty, err := os.Open("/dev/tty")
	if err != nil {
		fmt.Println("cannot open /dev/tty:", err)
		return
	}
	defer tty.Close()

	buf := make([]byte, 1)
	for {
		n, err := tty.Read(buf)
		if err != nil || n == 0 {
			continue
		}
		handleKey(buf[0])
	}
}

func pressHall(floor, btn int) {
	// Hall buttons are shared: set in ALL elevators
	for i := range nElev {
		elevs[i].mu.Lock()
		elevs[i].hallButtons[floor][btn] = true
		elevs[i].hallBtnRelease[floor][btn] = time.Now().Add(btnReleaseDur)
		elevs[i].mu.Unlock()
	}
}

func pressCab(floor int) {
	selectedMu.Lock()
	i := selected
	selectedMu.Unlock()

	elevs[i].mu.Lock()
	elevs[i].cabButtons[floor] = true
	elevs[i].cabBtnRelease[floor] = time.Now().Add(btnReleaseDur)
	elevs[i].mu.Unlock()
}

func toggleObstruction() {
	selectedMu.Lock()
	i := selected
	selectedMu.Unlock()

	elevs[i].mu.Lock()
	elevs[i].obstruction = !elevs[i].obstruction
	elevs[i].mu.Unlock()
}

func toggleMotorStop() {
	selectedMu.Lock()
	i := selected
	selectedMu.Unlock()

	elevs[i].mu.Lock()
	elevs[i].motorStop = !elevs[i].motorStop
	if elevs[i].motorStop {
		elevs[i].motorDir = 0
	}
	elevs[i].mu.Unlock()
}

func handleKey(ch byte) {
	switch ch {
	case '1':
		selectedMu.Lock()
		selected = 0
		selectedMu.Unlock()
	case '2':
		selectedMu.Lock()
		selected = 1
		selectedMu.Unlock()
	case '3':
		selectedMu.Lock()
		selected = 2
		selectedMu.Unlock()

	// Hall UP: q w e r → floors 0 1 2 3
	case 'q':
		pressHall(0, 0)
	case 'w':
		pressHall(1, 0)
	case 'e':
		pressHall(2, 0)
	case 'r':
		pressHall(3, 0)

	// Hall DOWN: a s d f → floors 0 1 2 3
	case 'a':
		pressHall(0, 1)
	case 's':
		pressHall(1, 1)
	case 'd':
		pressHall(2, 1)
	case 'f':
		pressHall(3, 1)

	// Cab: z x c v → floors 0 1 2 3
	case 'z':
		pressCab(0)
	case 'x':
		pressCab(1)
	case 'c':
		pressCab(2)
	case 'v':
		pressCab(3)

	// Obstruction toggle
	case 'o':
		toggleObstruction()

	// Motor stop toggle
	case 'm':
		toggleMotorStop()

	// Quit: Q or Ctrl-C
	case 'Q', 3:
		restoreTerminal()
		fmt.Print("\033[2J\033[H")
		os.Exit(0)
	}
}
