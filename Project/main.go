package main

import (
	"Driver-go/elevio"
	"flag"
	"fmt"
	"os"
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

func main() {
	var id, port string
	flag.StringVar(&id, "id", "", "Unique ID for the elevator")
	flag.StringVar(&port, "port", "15657", "Elevator server: port number OR host:port")
	flag.Parse()

	if id == "" {
		fmt.Fprintln(os.Stderr, "error: --id is required")
		os.Exit(1)
	}
	if n, err := strconv.Atoi(id); err != nil || n < 0 || n > 99 {
		fmt.Fprintf(os.Stderr, "error: --id must be an integer 0–99, got %q\n", id)
		os.Exit(1)
	}

	listenForPrimary(id)
	fmt.Printf("[main] became primary (id=%s)\n", id)
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

	var hallRequests [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]elevator.HallRequest

	// peersAtMaxCounter[floor][btn] tracks which peers have reported counter == config.HallCounterN.
	// The counter wraps to 0 only after all known peers confirm they've seen it at max,
	// preventing any node from misreading a newly-wrapped counter as stale.
	peersAtMaxCounter := [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]map[string]bool{}
	for floor := range peersAtMaxCounter {
		for btn := range peersAtMaxCounter[floor] {
			peersAtMaxCounter[floor][btn] = make(map[string]bool)
		}
	}

	buttonCh := make(chan elevio.ButtonEvent)
	floorCh := make(chan int)
	obstrCh := make(chan bool)
	txCh := make(chan message.ElevatorMessage)
	rxCh := make(chan message.ElevatorMessage)
	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool)

	go sendHeartbeat(id)
	go elevio.PollButtons(buttonCh)
	go elevio.PollFloorSensor(floorCh)
	go elevio.PollObstructionSwitch(obstrCh)
	go bcast.Transmitter(config.BcastPort, txCh)
	go bcast.Receiver(config.BcastPort, rxCh)
	go peers.Transmitter(config.PeerPort, id, peerTxEnable)
	go peers.Receiver(config.PeerPort, peerUpdateCh)

	peerTxEnable <- true

	peerStates := make(map[string]elevator.Elevator)
	unreachablePeers := make(map[string]bool)
	var hallRequestActivatedAt [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]time.Time
	obstructed := false

	doorTimer := time.NewTimer(0)
	<-doorTimer.C
	motorWatchdog := time.NewTimer(0)
	<-motorWatchdog.C
	broadcastTicker := time.NewTicker(config.BroadcastInterval)
	hallWatchdogTicker := time.NewTicker(config.HallWatchdogCheckInterval)

	// Async assigner: runs hall_request_assigner binary in a goroutine
	// so the main loop is never blocked by subprocess execution.
	assignerResultCh := make(chan [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]bool, 1)
	assignerInFlight := false
	needsReassign := false
	triggerAssigner := func() {
		if assignerInFlight {
			needsReassign = true
			return
		}
		assignerInFlight = true
		needsReassign = false
		hrCopy := hallRequests
		psCopy := make(map[string]elevator.Elevator, len(peerStates))
		for k, v := range peerStates {
			if !unreachablePeers[k] {
				psCopy[k] = v
			}
		}
		go func() {
			assignerResultCh <- assigner.AssignHallRequests(hrCopy, psCopy, id)
		}()
	}

	// Trigger FSM to serve cab calls loaded from disk
	if err == nil {
		for floor, active := range cab {
			if active {
				if elevator.FsmOnRequestButtonPress(&e, floor, elevator.ButtonCab) {
					doorTimer.Reset(config.DoorOpenTime)
				}
			}
		}
		if e.Behavior == elevator.ElevatorBehaviorMoving {
			motorWatchdog.Reset(config.MotorWatchdogTime)
		}
	}

	for {
		select {
		case btn := <-buttonCh:
			if elevator.ButtonType(btn.Button) == elevator.ButtonCab {
				prevBehavior := e.Behavior
				e.Requests[btn.Floor][elevator.ButtonCab] = true
				if err := persistence.SaveCabCalls(e, id); err != nil {
					fmt.Println("Warning:", err)
				}
				if elevator.FsmOnRequestButtonPress(&e, btn.Floor, elevator.ButtonCab) {
					doorTimer.Reset(config.DoorOpenTime)
				}
				if e.Behavior == elevator.ElevatorBehaviorMoving && prevBehavior != elevator.ElevatorBehaviorMoving {
					motorWatchdog.Reset(config.MotorWatchdogTime)
				}
			} else {
				hallRequests[btn.Floor][btn.Button].Active = true
				hallRequests[btn.Floor][btn.Button].Counter++
				if hallRequestActivatedAt[btn.Floor][btn.Button].IsZero() {
					hallRequestActivatedAt[btn.Floor][btn.Button] = time.Now()
				}
				txCh <- message.FromElevator(id, e, hallRequests, peerIDs(peerStates))
				elevator.SetHallLamps(elevator.ConfirmedHallRequests(hallRequests))
				peerStates[id] = e
				triggerAssigner()
			}

		case floor := <-floorCh:
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}
			if elevator.FsmOnFloorArrival(&e, floor) {
				doorTimer.Reset(config.DoorOpenTime)
				if !motorWatchdog.Stop() {
					select {
					case <-motorWatchdog.C:
					default:
					}
				}
				clearServedHall(&e, &hallRequests, &hallRequestActivatedAt, floor)
				elevator.SetHallLamps(elevator.ConfirmedHallRequests(hallRequests))
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
				clearServedHall(&e, &hallRequests, &hallRequestActivatedAt, e.Floor)
				elevator.SetHallLamps(elevator.ConfirmedHallRequests(hallRequests))
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
			fmt.Println("[watchdog] motor stuck — setting idle, will reassign")
			elevio.SetMotorDirection(elevio.MD_Stop)
			elevio.SetDoorOpenLamp(false)
			e.Behavior = elevator.ElevatorBehaviorIdle
			e.Direction = elevator.DirStop

		case msg := <-rxCh:
			if msg.ID == id {
				continue
			}
			if msg.Token != config.GroupToken {
				continue // silently drop messages from other groups on the same network
			}
			heardByRemote := false
			for _, pid := range msg.HeardPeers {
				if pid == id {
					heardByRemote = true
					break
				}
			}
			if heardByRemote {
				delete(unreachablePeers, msg.ID)
			} else {
				unreachablePeers[msg.ID] = true
			}
			handleRemoteMsg(msg, id, &hallRequests, &peersAtMaxCounter, peerStates, e, triggerAssigner)
			for f := 0; f < elevator.N_FLOORS; f++ {
				for b := 0; b < elevator.N_HALL_BUTTONS; b++ {
					if hallRequests[f][b].Active && hallRequestActivatedAt[f][b].IsZero() {
						hallRequestActivatedAt[f][b] = time.Now()
					}
				}
			}

		case peerUpdate := <-peerUpdateCh:
			for _, lost := range peerUpdate.Lost {
				delete(peerStates, lost)
				delete(unreachablePeers, lost)
				for floor := range peersAtMaxCounter {
					for btn := range peersAtMaxCounter[floor] {
						delete(peersAtMaxCounter[floor][btn], lost)
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
			triggerAssigner()

		case result := <-assignerResultCh:
			assignerInFlight = false
			prevBehavior := e.Behavior
			applyAssigned(&e, result, doorTimer)
			if e.Behavior == elevator.ElevatorBehaviorDoorOpen {
				clearServedHall(&e, &hallRequests, &hallRequestActivatedAt, e.Floor)
				elevator.SetHallLamps(elevator.ConfirmedHallRequests(hallRequests))
			}
			if e.Behavior == elevator.ElevatorBehaviorMoving && prevBehavior != elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}
			if needsReassign {
				triggerAssigner()
			}

		case <-broadcastTicker.C:
			txCh <- message.FromElevator(id, e, hallRequests, peerIDs(peerStates))

		case <-hallWatchdogTicker.C:
			now := time.Now()
			for f := 0; f < elevator.N_FLOORS; f++ {
				for b := 0; b < elevator.N_HALL_BUTTONS; b++ {
					if hallRequests[f][b].Active &&
						!hallRequestActivatedAt[f][b].IsZero() &&
						now.Sub(hallRequestActivatedAt[f][b]) > config.HallRequestWatchdogTime {
						fmt.Printf("[watchdog] hall request floor %d btn %d timed out — force-serving\n", f, b)
						prevBehavior := e.Behavior
						if elevator.FsmOnRequestButtonPress(&e, f, elevator.ButtonType(b)) {
							doorTimer.Reset(config.DoorOpenTime)
						}
						if e.Behavior == elevator.ElevatorBehaviorMoving && prevBehavior != elevator.ElevatorBehaviorMoving {
							motorWatchdog.Reset(config.MotorWatchdogTime)
						}
						hallRequestActivatedAt[f][b] = now
					}
				}
			}
		}
	}
}

// handleRemoteMsg processes an incoming peer message: merges hall-request state,
// updates peer state and lamp state, and triggers the hall-request assigner.
func handleRemoteMsg(
	msg message.ElevatorMessage,
	localID string,
	hallRequests *[elevator.N_FLOORS][elevator.N_HALL_BUTTONS]elevator.HallRequest,
	peersAtMaxCounter *[elevator.N_FLOORS][elevator.N_HALL_BUTTONS]map[string]bool,
	peerStates map[string]elevator.Elevator,
	localElevator elevator.Elevator,
	triggerAssigner func(),
) {
	for floor := 0; floor < elevator.N_FLOORS; floor++ {
		for btn := 0; btn < elevator.N_HALL_BUTTONS; btn++ {
			(*hallRequests)[floor][btn] = elevator.MergeHallRequest((*hallRequests)[floor][btn], msg.HallRequests[floor][btn])
			// Track whether this peer has seen the counter at max
			if msg.HallRequests[floor][btn].Counter == config.HallCounterN {
				(*peersAtMaxCounter)[floor][btn][msg.ID] = true
			} else {
				delete((*peersAtMaxCounter)[floor][btn], msg.ID)
			}
			// Track whether we ourselves are now at max
			if (*hallRequests)[floor][btn].Counter == config.HallCounterN {
				(*peersAtMaxCounter)[floor][btn][localID] = true
			} else {
				delete((*peersAtMaxCounter)[floor][btn], localID)
			}
			// Wrap counter to 0 once all known peers have confirmed seeing max
			if (*hallRequests)[floor][btn].Counter == config.HallCounterN {
				allAtMaxCount := true
				for peerID := range peerStates {
					if !(*peersAtMaxCounter)[floor][btn][peerID] {
						allAtMaxCount = false
						break
					}
				}
				if allAtMaxCount {
					(*hallRequests)[floor][btn].Counter = 0
					(*peersAtMaxCounter)[floor][btn] = make(map[string]bool)
				}
			}
		}
	}
	peerStates[msg.ID] = message.ToElevator(msg)
	peerStates[localID] = localElevator
	elevator.SetHallLamps(elevator.ConfirmedHallRequests(*hallRequests))
	triggerAssigner()
}

func applyAssigned(e *elevator.Elevator, assigned [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]bool, doorTimer *time.Timer) {
	for f := 0; f < elevator.N_FLOORS; f++ {
		for btn := 0; btn < elevator.N_HALL_BUTTONS; btn++ {
			if assigned[f][btn] && !e.Requests[f][btn] {
				if elevator.FsmOnRequestButtonPress(e, f, elevator.ButtonType(btn)) {
					doorTimer.Reset(config.DoorOpenTime)
				}
			}
		}
	}
}

func clearServedHall(e *elevator.Elevator, hallRequests *[elevator.N_FLOORS][elevator.N_HALL_BUTTONS]elevator.HallRequest, timestamps *[elevator.N_FLOORS][elevator.N_HALL_BUTTONS]time.Time, floor int) {
	for btn := 0; btn < elevator.N_HALL_BUTTONS; btn++ {
		if !e.Requests[floor][btn] {
			hallRequests[floor][btn].Active = false
			hallRequests[floor][btn].Counter++
			timestamps[floor][btn] = time.Time{}
		}
	}
}

func peerIDs(peerStates map[string]elevator.Elevator) []string {
	ids := make([]string, 0, len(peerStates))
	for id := range peerStates {
		ids = append(ids, id)
	}
	return ids
}
