package main

import (
	"Driver-go/elevio"
	"Network-go/network/bcast"
	"Network-go/network/peers"
	"flag"
	"fmt"
	"time"

	"sanntid/project/assigner"
	"sanntid/project/config"
	"sanntid/project/elevator"
	"sanntid/project/networkmsg"
	"sanntid/project/storage"
)

func setHallLights(hallRequests [elevator.N_FLOORS][2]bool) {
	for floor := 0; floor < elevator.N_FLOORS; floor++ {
		elevio.SetButtonLamp(elevio.BT_HallUp, floor, hallRequests[floor][0])
		elevio.SetButtonLamp(elevio.BT_HallDown, floor, hallRequests[floor][1])
	}
}

func main() {
	var id string
	var port string
	flag.StringVar(&id, "id", "", "Elevator ID")
	flag.StringVar(&port, "port", "localhost:15657", "Simulator address")
	flag.Parse()

	if id == "" {
		fmt.Println("Error: --id flag is required")
		return
	}

	fmt.Printf("Starting elevator %s on %s\n", id, port)

	elevio.Init(port, elevator.N_FLOORS)

	e := elevator.Elevator{
		Direction: elevator.DirStop,
		Behavior:  elevator.ElevatorBehaviorIdle,
	}

	// Restore cab calls from backup
	cabBackup := storage.LoadCabCalls(id)
	for f := 0; f < elevator.N_FLOORS; f++ {
		e.Requests[f][elevator.ButtonCab] = cabBackup[f]
	}

	if elevio.GetFloor() == -1 {
		elevator.FsmOnInitBetweenFloors(&e)
	}

	// Channels
	buttonCh := make(chan elevio.ButtonEvent)
	floorCh := make(chan int)
	obstrCh := make(chan bool)
	networkTxCh := make(chan networkmsg.NetworkMessage)
	networkRxCh := make(chan networkmsg.NetworkMessage)
	peerUpdateCh := make(chan peers.PeerUpdate)
	peerTxEnable := make(chan bool)

	doorTimer := time.NewTimer(0)
	<-doorTimer.C
	motorWatchdog := time.NewTimer(0)
	<-motorWatchdog.C
	broadcastTicker := time.NewTicker(config.BroadcastInterval)

	go elevio.PollButtons(buttonCh)
	go elevio.PollFloorSensor(floorCh)
	go elevio.PollObstructionSwitch(obstrCh)

	go peers.Transmitter(config.PeerPort, id, peerTxEnable)
	go peers.Receiver(config.PeerPort, peerUpdateCh)
	go bcast.Transmitter(config.BcastPort, networkTxCh)
	go bcast.Receiver(config.BcastPort, networkRxCh)

	var hallRequests [elevator.N_FLOORS][2]bool
	peerStates := make(map[string]elevator.Elevator)
	var seq uint64
	obstructed := false
	peerAlive := true
	obstrTimer := time.NewTimer(0)
	<-obstrTimer.C

	// If we have restored cab calls, set lights and handle them
	elevator.SetAllLights(e)
	setHallLights(hallRequests)

	// If restored cab calls exist and we're idle, trigger processing
	if e.Behavior == elevator.ElevatorBehaviorIdle {
		pair := elevator.RequestsChooseDirection(e)
		if pair.Behavior != elevator.ElevatorBehaviorIdle {
			e.Direction = pair.Direction
			e.Behavior = pair.Behavior
			if pair.Behavior == elevator.ElevatorBehaviorDoorOpen {
				elevio.SetDoorOpenLamp(true)
				e = elevator.RequestsClearAtCurrentFloor(e)
				elevator.SetAllLights(e)
				doorTimer.Reset(config.DoorOpenTime)
			} else if pair.Behavior == elevator.ElevatorBehaviorMoving {
				elevio.SetMotorDirection(elevio.MotorDirection(dirToMotorInt(e.Direction)))
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}
		}
	}

	for {
		select {
		case buttonEvent := <-buttonCh:
			btnType := elevator.ButtonType(buttonEvent.Button)
			btnFloor := buttonEvent.Floor

			if btnType == elevator.ButtonCab {
				if elevator.FsmOnRequestButtonPress(&e, btnFloor, btnType) {
					doorTimer.Reset(config.DoorOpenTime)
				}
				saveCabCalls(id, &e)
				if e.Behavior == elevator.ElevatorBehaviorMoving {
					motorWatchdog.Reset(config.MotorWatchdogTime)
				}
			} else {
				hallRequests[btnFloor][int(btnType)] = true
				setHallLights(hallRequests)

				// Run assigner with current states
				peerStates[id] = e
				assigned := assigner.AssignHallRequests(hallRequests, peerStates, id)
				applyAssigned(&e, assigned, doorTimer)
				if e.Behavior == elevator.ElevatorBehaviorMoving {
					motorWatchdog.Reset(config.MotorWatchdogTime)
				}
			}

		case floor := <-floorCh:
			motorWatchdog.Reset(config.MotorWatchdogTime)
			if elevator.FsmOnFloorArrival(&e, floor) {
				doorTimer.Reset(config.DoorOpenTime)
				clearServedHallRequests(&e, &hallRequests, floor)
				setHallLights(hallRequests)
				saveCabCalls(id, &e)
			}

		case <-doorTimer.C:
			if obstructed {
				doorTimer.Reset(config.DoorOpenTime)
				continue
			}
			if elevator.FsmOnDoorTimeout(&e) {
				doorTimer.Reset(config.DoorOpenTime)
			}
			saveCabCalls(id, &e)
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}

		case obstruction := <-obstrCh:
			obstructed = obstruction
			if obstructed {
				if e.Behavior == elevator.ElevatorBehaviorDoorOpen {
					doorTimer.Reset(config.DoorOpenTime)
				}
				obstrTimer.Reset(10 * time.Second)
			} else {
				obstrTimer.Stop()
				if !peerAlive {
					peerAlive = true
					peerTxEnable <- true
				}
			}

		case <-obstrTimer.C:
			// Obstruction held too long — disable peer transmission and clear hall requests
			peerAlive = false
			peerTxEnable <- false
			hallRequests = [elevator.N_FLOORS][2]bool{}
			setHallLights(hallRequests)

		case <-motorWatchdog.C:
			fmt.Println("Motor watchdog triggered! Clearing hall requests.")
			hallRequests = [elevator.N_FLOORS][2]bool{}
			setHallLights(hallRequests)
			elevio.SetMotorDirection(elevio.MD_Stop)
			e.Behavior = elevator.ElevatorBehaviorIdle
			e.Direction = elevator.DirStop
			peerAlive = false
			peerTxEnable <- false

		case msg := <-networkRxCh:
			if msg.SenderID == id {
				continue
			}

			// Merge hall requests (OR)
			for f := 0; f < elevator.N_FLOORS; f++ {
				for b := 0; b < 2; b++ {
					hallRequests[f][b] = hallRequests[f][b] || msg.HallRequests[f][b]
				}
			}
			setHallLights(hallRequests)

			// Update peer state
			peerElev := networkmsg.StateToElevator(msg.State)
			peerStates[msg.SenderID] = peerElev

			// Run assigner
			peerStates[id] = e
			assigned := assigner.AssignHallRequests(hallRequests, peerStates, id)
			applyAssigned(&e, assigned, doorTimer)
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}

		case peerUpdate := <-peerUpdateCh:
			for _, lostPeer := range peerUpdate.Lost {
				fmt.Printf("Lost peer: %s\n", lostPeer)
				delete(peerStates, lostPeer)
			}
			if len(peerUpdate.Lost) > 0 {
				// Reassign with remaining peers
				peerStates[id] = e
				assigned := assigner.AssignHallRequests(hallRequests, peerStates, id)
				applyAssigned(&e, assigned, doorTimer)
				if e.Behavior == elevator.ElevatorBehaviorMoving {
					motorWatchdog.Reset(config.MotorWatchdogTime)
				}
			}

		case <-broadcastTicker.C:
			if !peerAlive {
				continue
			}
			seq++
			msg := networkmsg.NetworkMessage{
				SenderID:     id,
				State:        networkmsg.ElevatorToState(e),
				HallRequests: hallRequests,
				Seq:          seq,
			}
			networkTxCh <- msg
		}
	}
}

func dirToMotorInt(d elevator.Direction) int {
	switch d {
	case elevator.DirUp:
		return 1
	case elevator.DirDown:
		return -1
	default:
		return 0
	}
}

func saveCabCalls(id string, e *elevator.Elevator) {
	var cabCalls [elevator.N_FLOORS]bool
	for f := 0; f < elevator.N_FLOORS; f++ {
		cabCalls[f] = e.Requests[f][elevator.ButtonCab]
	}
	storage.SaveCabCalls(id, cabCalls)
}

func clearServedHallRequests(e *elevator.Elevator, hallRequests *[elevator.N_FLOORS][2]bool, floor int) {
	for btn := 0; btn < 2; btn++ {
		if !e.Requests[floor][btn] {
			hallRequests[floor][btn] = false
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
