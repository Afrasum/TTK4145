package main

import (
	"Driver-go/elevio"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"sanntid/project/assigner"
	"sanntid/project/config"
	"sanntid/project/elevator"
	"sanntid/project/network/bcast"
	"sanntid/project/network/message"
	"sanntid/project/network/peers"
	"sanntid/project/persistence"
)

const hallCounterN = 255 // max value of counter 



func main() {
	var id, port string
	flag.StringVar(&id, "id", "", "Unique ID for the elevator")
	flag.StringVar(&port, "port", "15657", "Elevator server: port number OR host:port")
	flag.Parse()

	if id == "" {
		panic("--id required")
	}

	listenForPrimary(id)
	fmt.Printf("[main] became primary (id=%s)\n", id)
	go sendHeartbeat(id)
	startBackup(id, port)

	// Accept plain port number or full host:port
	if !strings.Contains(port, ":") {
		port = "localhost:" + port
	}
	fmt.Printf("[main] connecting to elevator at %s\n", port)
	elevio.Init(port, elevator.N_FLOORS)
	fmt.Println("[main] elevator connected, starting FSM")


	

	e := elevator.Elevator{
		Direction: elevator.DirStop,
		Behavior:  elevator.ElevatorBehaviorIdle,
	}

	if elevio.GetFloor() == -1 {
		elevator.FsmOnInitBetweenFloors(&e)
	}

	cab, err := persistence.LoadCabCalls(id)
	if err == nil {
		for floor := range cab {
			if cab[floor] {
				e.Requests[floor][elevator.ButtonCab] = true
			}
		}
	}

	var hallRequests [elevator.N_FLOORS][2]elevator.HallRequest
	hasReachedN := [elevator.N_FLOORS][2]map[string]bool{}
	for floor := range hasReachedN {
		for btn := range hasReachedN[floor] {
			hasReachedN[floor][btn] = make(map[string]bool)
		}
	}



	buttonCh := make(chan elevio.ButtonEvent)
	floorCh := make(chan int)
	obstrCh := make(chan bool)
	txCh := make(chan message.ElevatorMessage)
	rxCh := make(chan message.ElevatorMessage)
	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool)

	go elevio.PollButtons(buttonCh)
	go elevio.PollFloorSensor(floorCh)
	go elevio.PollObstructionSwitch(obstrCh)
	go bcast.Transmitter(config.BcastPort, txCh)
	go bcast.Receiver(config.BcastPort, rxCh)
	go peers.Transmitter(config.PeerPort, id, peerTxEnable)
	go peers.Receiver(config.PeerPort, peerUpdateCh)

	peerTxEnable <- true

	peerStates := make(map[string]elevator.Elevator)
	obstructed := false

	doorTimer := time.NewTimer(0)
	<-doorTimer.C
	motorWatchdog := time.NewTimer(0)
	<-motorWatchdog.C
	broadcastTicker := time.NewTicker(config.BroadcastInterval)

	for {
		select {
		case btn := <-buttonCh:
			if elevator.ButtonType(btn.Button) == elevator.ButtonCab {
				e.Requests[btn.Floor][elevator.ButtonCab] = true
				if err := persistence.SaveCabCalls(e, id); err != nil {
					fmt.Println("Warning:", err)
				}
				if elevator.FsmOnRequestButtonPress(&e, btn.Floor, elevator.ButtonCab) {
					doorTimer.Reset(config.DoorOpenTime)
				}
			} else {
				hallRequests[btn.Floor][btn.Button].Active = true
				hallRequests[btn.Floor][btn.Button].Counter++
				elevator.SetHallLamps(confirmedHallRequests(hallRequests))
				peerStates[id] = e
				applyAssigned(&e, assigner.AssignHallRequests(hallRequests, peerStates, id), doorTimer)
			}
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}

		case floor := <-floorCh:
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}
			if elevator.FsmOnFloorArrival(&e, floor) {
				doorTimer.Reset(config.DoorOpenTime)
				clearServedHall(&e, &hallRequests, floor)
				elevator.SetHallLamps(confirmedHallRequests(hallRequests))
				if err := persistence.SaveCabCalls(e, id); err != nil {
					fmt.Println("Warning:", err)
				}
			}

		case <-doorTimer.C:
			if obstructed {
				doorTimer.Reset(config.DoorOpenTime)
				continue
			}
			if elevator.FsmOnDoorTimeout(&e) {
				doorTimer.Reset(config.DoorOpenTime)
			}
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			} else if e.Behavior == elevator.ElevatorBehaviorIdle {
				if !motorWatchdog.Stop() {
					select {
					case <-motorWatchdog.C:
					default:
					}
				}
			}

		case obs := <-obstrCh:
			obstructed = obs
			if obstructed && e.Behavior == elevator.ElevatorBehaviorDoorOpen {
				doorTimer.Reset(config.DoorOpenTime)
			}

		case <-motorWatchdog.C:
			fmt.Println("Motor watchdog triggered")
			elevio.SetMotorDirection(elevio.MD_Stop)
			e.Behavior = elevator.ElevatorBehaviorIdle
			e.Direction = elevator.DirStop

		case msg := <-rxCh:
			if msg.ID == id {
				continue
			}
			for floor := 0; floor < elevator.N_FLOORS; floor++ {
				for btn := 0; btn < 2; btn++ {
					hallRequests[floor][btn] = mergeHallRequests(hallRequests[floor][btn], msg.HallRequests[floor][btn])
					if msg.HallRequests[floor][btn].Counter == hallCounterN {
						hasReachedN[floor][btn][msg.ID] = true
					} else {
						delete(hasReachedN[floor][btn], msg.ID)
					}
					// Track self so barrier can trigger when we also reach N
					if hallRequests[floor][btn].Counter == hallCounterN {
						hasReachedN[floor][btn][id] = true
					} else {
						delete(hasReachedN[floor][btn], id)
					}
					if hallRequests[floor][btn].Counter == hallCounterN {
						hallAtN := true
						for peerID := range peerStates {
							if !hasReachedN[floor][btn][peerID] {
								hallAtN = false
								break
							}
						}

						if hallAtN {
							hallRequests[floor][btn].Counter = 0
							hasReachedN[floor][btn] = make(map[string]bool)
						}
					}
					
				}

			}

			peerStates[msg.ID] = message.ToElevator(msg)
			peerStates[id] = e
			elevator.SetHallLamps(confirmedHallRequests(hallRequests))
			applyAssigned(&e, assigner.AssignHallRequests(hallRequests, peerStates, id), doorTimer)
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}

		case pu := <-peerUpdateCh:
			for _, lost := range pu.Lost {
				delete(peerStates, lost)
				for floor := range hasReachedN {
					for btn := range hasReachedN[floor] {
						delete(hasReachedN[floor][btn], lost)
					}
				}
			}
			if len(peerStates) == 0 {
				for floor := range hallRequests {
					for btn := range hallRequests[floor] {
						hallRequests[floor][btn].Unknown = true
					}
				}
			}
			peerStates[id] = e
			applyAssigned(&e, assigner.AssignHallRequests(hallRequests, peerStates, id), doorTimer)
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}

		case <-broadcastTicker.C:
			txCh <- message.FromElevator(id, e, hallRequests)
		}
	}
}

func applyAssigned(e *elevator.Elevator, assigned [elevator.N_FLOORS][2]bool, doorTimer *time.Timer) {
	for f := 0; f < elevator.N_FLOORS; f++ {
		for btn := 0; btn < 2; btn++ {
			if assigned[f][btn] && !e.Requests[f][btn] {
				if elevator.FsmOnRequestButtonPress(e, f, elevator.ButtonType(btn)) {
					doorTimer.Reset(config.DoorOpenTime)
				}
			}
		}
	}
}

func clearServedHall(e *elevator.Elevator, hallRequests *[elevator.N_FLOORS][2]elevator.HallRequest, floor int) {
	for btn := 0; btn < 2; btn++ {
		if !e.Requests[floor][btn] {
			hallRequests[floor][btn].Active = false
			hallRequests[floor][btn].Counter++
		}
	}
}

func heartbeatPort(id string) string {
	n, err := strconv.Atoi(id)
	if err != nil {
		panic(fmt.Sprintf("elevator id must be a number, got %q", id))
	}
	return fmt.Sprintf(":%d", 30000+n)
}

func listenForPrimary(id string) {
	addr, _ := net.ResolveUDPAddr("udp", heartbeatPort(id))
	var conn *net.UDPConn
	for {
		var err error
		conn, err = net.ListenUDP("udp", addr)
		if err == nil {
			break
		}
		fmt.Println("[listenForPrimary] port busy, retrying...")
		time.Sleep(200 * time.Millisecond)
	}
	defer conn.Close()
	fmt.Println("[listenForPrimary] listening for primary heartbeat...")

	buf := make([]byte, 128)
	for {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("[listenForPrimary] no heartbeat, becoming primary")
			return
		}
		if string(buf[:n]) == id {
			continue
		}
		fmt.Printf("[listenForPrimary] heartbeat from %q, waiting...\n", string(buf[:n]))
	}
}

func startBackup(id, port string) {
	exe, _ := os.Executable()
	fmt.Printf("[startBackup] spawning backup: %s --id=%s --port=%s\n", exe, id, port)
	args := `'` + exe + `' --id=` + id + ` --port=` + port
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("osascript", "-e", `tell app "Terminal" to do script "`+args+`"`)
	} else {
		cmd = exec.Command("gnome-terminal", "--", "bash", "-c", args+"; read")
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("[startBackup] could not open terminal window:", err)
	}
}

func sendHeartbeat(id string) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1"+heartbeatPort(id))
	conn, _ := net.DialUDP("udp", nil, addr)
	defer conn.Close()
	for {
		conn.Write([]byte(id))
		time.Sleep(100 * time.Millisecond)
	}
}


func cyclicIsAfter(incoming,local uint8) bool {
	//is b after a in cyclic order 
	if local == hallCounterN && incoming == 0 {return true} // accept reset
	if local == 0 && incoming == hallCounterN {return false} // others must reset
	return incoming > local
}

func mergeHallRequests(ours, theirs elevator.HallRequest) elevator.HallRequest {
	if ours.Unknown {
		return elevator.HallRequest{Active: theirs.Active, Counter: theirs.Counter, Unknown: false}
	}
	if cyclicIsAfter(theirs.Counter, ours.Counter) {
		return theirs
	}
	return ours
}


func confirmedHallRequests(hallRequests [elevator.N_FLOORS][2]elevator.HallRequest) [elevator.N_FLOORS][2]bool {
	var out [elevator.N_FLOORS][2]bool
	for floor := range hallRequests {
		for btn := range hallRequests[floor] {
			out[floor][btn] = hallRequests[floor][btn].Active
		}
	}
	return out 
}


