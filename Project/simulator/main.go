package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	numFloors    = 4
	numElevators = 3
	numButtons   = 3 // HallUp=0, HallDown=1, Cab=2

	basePort = 15657

	physTickMs   = 10
	speed        = 0.4 // floors per second (2.5s per floor, matches config.TravelTime)
	floorEpsilon = 0.02

	hallHoldMs = 200 // how long hall button presses last
	renderFPS  = 30
)

// Command IDs matching driver-go/elevio/elevator_io.go
const (
	cmdSetMotorDir       = 1
	cmdSetButtonLamp     = 2
	cmdSetFloorIndicator = 3
	cmdSetDoorOpenLamp   = 4
	cmdSetStopLamp       = 5
	cmdGetButton         = 6
	cmdGetFloor          = 7
	cmdGetStop           = 8
	cmdGetObstruction    = 9
)

type ElevatorSim struct {
	mu sync.Mutex

	// Physics
	motorDir int     // -1, 0, 1
	position float64 // 0.0 to 3.0
	currFloor int    // -1 means between floors, 0-3 at floor

	// Lamps
	buttonLamps [numFloors][numButtons]bool
	floorIndic  int
	doorOpen    bool
	stopLamp    bool

	// Inputs (simulated from keyboard)
	buttons     [numFloors][numButtons]bool
	obstruction bool
	motorStop   bool // simulated motor failure

	// Connection state
	connected bool
	listener  net.Listener
}

var (
	elevators    [numElevators]*ElevatorSim
	selectedElev int // 0-2, which elevator is selected for cab/obstruction/motor

	// Hall button release timers
	hallTimers [numFloors][2]*time.Timer // [floor][0=up, 1=down]
)

func main() {
	// Initialize elevators
	for i := 0; i < numElevators; i++ {
		elevators[i] = &ElevatorSim{
			position:  0.5,
			currFloor: -1,
		}
	}

	// Start TCP servers
	for i := 0; i < numElevators; i++ {
		go tcpServer(i)
	}

	// Start physics goroutines
	for i := 0; i < numElevators; i++ {
		go physicsLoop(i)
	}

	// Start render goroutine
	go renderLoop()

	// Handle cleanup on exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		restoreTerminal()
		os.Exit(0)
	}()

	// Set terminal to raw mode and handle keyboard
	setRawTerminal()
	defer restoreTerminal()

	keyboardLoop()
}

// --- Terminal raw mode ---

func setRawTerminal() {
	// Save and set raw mode using stty
	exec.Command("stty", "-F", "/dev/stdin", "cbreak", "-echo").Run()
	// Try macOS variant too
	exec.Command("stty", "cbreak", "-echo").Run()
}

func restoreTerminal() {
	exec.Command("stty", "-F", "/dev/stdin", "sane").Run()
	exec.Command("stty", "sane").Run()
	// Show cursor
	fmt.Print("\033[?25h")
}

// --- TCP server ---

func tcpServer(idx int) {
	port := basePort + idx
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to listen on port %d: %v\n", port, err)
		os.Exit(1)
	}
	elevators[idx].listener = ln

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		// Disable Nagle for low-latency 4-byte messages
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
		}
		// Only allow one connection at a time per elevator
		elevators[idx].mu.Lock()
		elevators[idx].connected = true
		elevators[idx].mu.Unlock()

		handleConnection(idx, conn)

		elevators[idx].mu.Lock()
		elevators[idx].connected = false
		// Stop motor on disconnect
		elevators[idx].motorDir = 0
		elevators[idx].mu.Unlock()
	}
}

func handleConnection(idx int, conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 4)
	for {
		_, err := readFull(conn, buf)
		if err != nil {
			return
		}

		cmd := buf[0]
		e := elevators[idx]

		switch cmd {
		case cmdSetMotorDir:
			dir := int(int8(buf[1])) // byte 255 -> -1
			e.mu.Lock()
			e.motorDir = dir
			e.mu.Unlock()

		case cmdSetButtonLamp:
			btnType := int(buf[1])
			floor := int(buf[2])
			value := buf[3] != 0
			if floor >= 0 && floor < numFloors && btnType >= 0 && btnType < numButtons {
				e.mu.Lock()
				e.buttonLamps[floor][btnType] = value
				e.mu.Unlock()
			}

		case cmdSetFloorIndicator:
			floor := int(buf[1])
			if floor >= 0 && floor < numFloors {
				e.mu.Lock()
				e.floorIndic = floor
				e.mu.Unlock()
			}

		case cmdSetDoorOpenLamp:
			value := buf[1] != 0
			e.mu.Lock()
			e.doorOpen = value
			e.mu.Unlock()

		case cmdSetStopLamp:
			value := buf[1] != 0
			e.mu.Lock()
			e.stopLamp = value
			e.mu.Unlock()

		case cmdGetButton:
			btnType := int(buf[1])
			floor := int(buf[2])
			resp := [4]byte{cmdGetButton, 0, 0, 0}
			if floor >= 0 && floor < numFloors && btnType >= 0 && btnType < numButtons {
				e.mu.Lock()
				if e.buttons[floor][btnType] {
					resp[1] = 1
				}
				e.mu.Unlock()
			}
			_, err := conn.Write(resp[:])
			if err != nil {
				return
			}

		case cmdGetFloor:
			resp := [4]byte{cmdGetFloor, 0, 0, 0}
			e.mu.Lock()
			f := e.currFloor
			e.mu.Unlock()
			if f >= 0 {
				resp[1] = 1
				resp[2] = byte(f)
			} else {
				resp[1] = 0
				resp[2] = 0
			}
			_, err := conn.Write(resp[:])
			if err != nil {
				return
			}

		case cmdGetStop:
			resp := [4]byte{cmdGetStop, 0, 0, 0}
			_, err := conn.Write(resp[:])
			if err != nil {
				return
			}

		case cmdGetObstruction:
			resp := [4]byte{cmdGetObstruction, 0, 0, 0}
			e.mu.Lock()
			if e.obstruction {
				resp[1] = 1
			}
			e.mu.Unlock()
			_, err := conn.Write(resp[:])
			if err != nil {
				return
			}
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

// --- Physics simulation ---

func physicsLoop(idx int) {
	ticker := time.NewTicker(physTickMs * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		e := elevators[idx]
		e.mu.Lock()

		dir := e.motorDir
		if e.motorStop {
			dir = 0 // Motor failure: ignore motor commands
		}

		// Update position
		delta := float64(dir) * speed * float64(physTickMs) / 1000.0
		e.position += delta

		// Clamp to valid range
		if e.position < 0 {
			e.position = 0
		}
		if e.position > float64(numFloors-1) {
			e.position = float64(numFloors - 1)
		}

		// Floor sensor: check if near an integer floor
		e.currFloor = -1
		for f := 0; f < numFloors; f++ {
			diff := e.position - float64(f)
			if diff < 0 {
				diff = -diff
			}
			if diff < floorEpsilon {
				e.currFloor = f
				break
			}
		}

		e.mu.Unlock()
	}
}

// --- Keyboard input ---

func keyboardLoop() {
	buf := make([]byte, 1)
	for {
		_, err := os.Stdin.Read(buf)
		if err != nil {
			continue
		}
		ch := buf[0]

		switch ch {
		// Select elevator
		case '1':
			selectedElev = 0
		case '2':
			selectedElev = 1
		case '3':
			selectedElev = 2

		// Hall UP buttons (floors 0-3)
		case 'q':
			pressHallButton(0, 0) // floor 0, HallUp
		case 'w':
			pressHallButton(1, 0) // floor 1, HallUp
		case 'e':
			pressHallButton(2, 0) // floor 2, HallUp
		case 'r':
			pressHallButton(3, 0) // floor 3, HallUp

		// Hall DOWN buttons (floors 0-3)
		case 'a':
			pressHallButton(0, 1) // floor 0, HallDown
		case 's':
			pressHallButton(1, 1) // floor 1, HallDown
		case 'd':
			pressHallButton(2, 1) // floor 2, HallDown
		case 'f':
			pressHallButton(3, 1) // floor 3, HallDown

		// Cab buttons (floors 0-3, selected elevator only)
		case 'z':
			pressCabButton(selectedElev, 0)
		case 'x':
			pressCabButton(selectedElev, 1)
		case 'c':
			pressCabButton(selectedElev, 2)
		case 'v':
			pressCabButton(selectedElev, 3)

		// Obstruction toggle
		case 'o':
			e := elevators[selectedElev]
			e.mu.Lock()
			e.obstruction = !e.obstruction
			e.mu.Unlock()

		// Motor stop toggle
		case 'm':
			e := elevators[selectedElev]
			e.mu.Lock()
			e.motorStop = !e.motorStop
			e.mu.Unlock()

		// Quit
		case 'Q':
			restoreTerminal()
			os.Exit(0)
		}
	}
}

func pressHallButton(floor, btnType int) {
	// Hall buttons are shared: set on ALL elevators (momentary, 200ms)
	for i := 0; i < numElevators; i++ {
		e := elevators[i]
		e.mu.Lock()
		e.buttons[floor][btnType] = true
		e.mu.Unlock()
	}

	// Cancel existing timer if any
	if hallTimers[floor][btnType] != nil {
		hallTimers[floor][btnType].Stop()
	}

	// Auto-release after 200ms
	hallTimers[floor][btnType] = time.AfterFunc(hallHoldMs*time.Millisecond, func() {
		for i := 0; i < numElevators; i++ {
			e := elevators[i]
			e.mu.Lock()
			e.buttons[floor][btnType] = false
			e.mu.Unlock()
		}
	})
}

func pressCabButton(idx, floor int) {
	e := elevators[idx]
	e.mu.Lock()
	e.buttons[floor][2] = true // Cab = button type 2
	e.mu.Unlock()

	// Momentary press, 200ms
	time.AfterFunc(hallHoldMs*time.Millisecond, func() {
		e.mu.Lock()
		e.buttons[floor][2] = false
		e.mu.Unlock()
	})
}

// --- Rendering ---

func renderLoop() {
	// Hide cursor
	fmt.Print("\033[?25l")
	// Clear screen
	fmt.Print("\033[2J")

	ticker := time.NewTicker(time.Second / renderFPS)
	defer ticker.Stop()

	var sb strings.Builder

	for range ticker.C {
		sb.Reset()

		// Move cursor to top-left
		sb.WriteString("\033[H")

		// Snapshot all elevator state
		type elevSnap struct {
			motorDir    int
			position    float64
			currFloor   int
			buttonLamps [numFloors][numButtons]bool
			floorIndic  int
			doorOpen    bool
			stopLamp    bool
			buttons     [numFloors][numButtons]bool
			obstruction bool
			motorStop   bool
			connected   bool
		}

		var snaps [numElevators]elevSnap
		for i := 0; i < numElevators; i++ {
			e := elevators[i]
			e.mu.Lock()
			snaps[i] = elevSnap{
				motorDir:    e.motorDir,
				position:    e.position,
				currFloor:   e.currFloor,
				buttonLamps: e.buttonLamps,
				floorIndic:  e.floorIndic,
				doorOpen:    e.doorOpen,
				stopLamp:    e.stopLamp,
				buttons:     e.buttons,
				obstruction: e.obstruction,
				motorStop:   e.motorStop,
				connected:   e.connected,
			}
			e.mu.Unlock()
		}

		sel := selectedElev

		// Title
		sb.WriteString(fmt.Sprintf(" Elevator Simulator                    [Selected: Elev %d]\033[K\n", sel+1))
		sb.WriteString("\033[K\n")

		// Header
		sb.WriteString(" +")
		for i := 0; i < numElevators; i++ {
			sb.WriteString(fmt.Sprintf("----- Elev %d ------+", i+1))
		}
		sb.WriteString("\033[K\n")

		// Connection status
		sb.WriteString(" |")
		for i := 0; i < numElevators; i++ {
			port := basePort + i
			connStr := "\033[32mCONN\033[0m"
			if !snaps[i].connected {
				connStr = "\033[31m....\033[0m"
			}
			sb.WriteString(fmt.Sprintf(" Port: %d  %s |", port, connStr))
		}
		sb.WriteString("\033[K\n")

		// Separator
		sb.WriteString(" +")
		for i := 0; i < numElevators; i++ {
			sb.WriteString("-------------------+")
		}
		sb.WriteString("\033[K\n")

		// Floor rows (top to bottom: floor 3 down to floor 0)
		for f := numFloors - 1; f >= 0; f-- {
			sb.WriteString(" |")
			for i := 0; i < numElevators; i++ {
				s := &snaps[i]

				// Hall down lamp
				hallDn := " "
				if f < numFloors-1 { // No hall-down on top floor... actually show all for simplicity
					if s.buttonLamps[f][1] {
						hallDn = "\033[33m*\033[0m"
					} else {
						hallDn = " "
					}
				}

				// Hall up lamp
				hallUp := " "
				if f > 0 || true { // Show all
					if s.buttonLamps[f][0] {
						hallUp = "\033[33m*\033[0m"
					} else {
						hallUp = " "
					}
				}

				// Cab lamp
				cabLamp := " "
				if s.buttonLamps[f][2] {
					cabLamp = "\033[33m*\033[0m"
				}

				// Format based on which buttons are valid at this floor
				// Floor 0: no hall-down, Floor 3: no hall-up
				var dnStr, upStr string
				if f == numFloors-1 {
					dnStr = "   "
					upStr = fmt.Sprintf("[%s]", hallUp)
				} else if f == 0 {
					dnStr = fmt.Sprintf("[%s]", hallDn)
					upStr = "   "
				} else {
					dnStr = fmt.Sprintf("[%s]", hallDn)
					upStr = fmt.Sprintf("[%s]", hallUp)
				}

				sb.WriteString(fmt.Sprintf("  %d  %s %s  [%s]  |", f, dnStr, upStr, cabLamp))
			}
			sb.WriteString("\033[K\n")
		}

		// Separator
		sb.WriteString(" +")
		for i := 0; i < numElevators; i++ {
			sb.WriteString("-------------------+")
		}
		sb.WriteString("\033[K\n")

		// Position and direction
		sb.WriteString(" |")
		for i := 0; i < numElevators; i++ {
			s := &snaps[i]
			arrow := "--"
			if s.motorDir > 0 {
				arrow = ">>"
			} else if s.motorDir < 0 {
				arrow = "<<"
			}
			if s.motorStop {
				arrow = "!!"
			}

			floorStr := "betw"
			if s.currFloor >= 0 {
				floorStr = fmt.Sprintf("%d   ", s.currFloor)
			}
			sb.WriteString(fmt.Sprintf("  %s Floor %s    |", arrow, floorStr))
		}
		sb.WriteString("\033[K\n")

		// Door
		sb.WriteString(" |")
		for i := 0; i < numElevators; i++ {
			s := &snaps[i]
			doorStr := "closed"
			if s.doorOpen {
				doorStr = "\033[33mOPEN  \033[0m"
			}
			sb.WriteString(fmt.Sprintf("  Door: %-10s |", doorStr))
		}
		sb.WriteString("\033[K\n")

		// Obstruction
		sb.WriteString(" |")
		for i := 0; i < numElevators; i++ {
			s := &snaps[i]
			obStr := "OFF"
			if s.obstruction {
				obStr = "\033[31mON \033[0m"
			}
			sb.WriteString(fmt.Sprintf("  Obstr: %-9s |", obStr))
		}
		sb.WriteString("\033[K\n")

		// Motor
		sb.WriteString(" |")
		for i := 0; i < numElevators; i++ {
			s := &snaps[i]
			motStr := "OK"
			if s.motorStop {
				motStr = "\033[31mSTOPPED\033[0m"
			}
			sb.WriteString(fmt.Sprintf("  Motor: %-9s |", motStr))
		}
		sb.WriteString("\033[K\n")

		// Bottom separator
		sb.WriteString(" +")
		for i := 0; i < numElevators; i++ {
			sb.WriteString("-------------------+")
		}
		sb.WriteString("\033[K\n")

		// Key hints
		sb.WriteString("\033[K\n")
		sb.WriteString(" Hall: [q]0^ [w]1^ [e]2^ [r]3^  [a]0v [s]1v [d]2v [f]3v\033[K\n")
		sb.WriteString(" Cab:  [z]0  [x]1  [c]2  [v]3   [o]bstr [m]otor  [Q]uit\033[K\n")
		sb.WriteString("\033[K\n")

		// Write everything at once
		fmt.Print(sb.String())
	}
}
