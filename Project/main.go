package main

import (
	"flag"
	"fmt"
	"time"

	"Driver-go/elevio"
	"sanntid/project/assigner"
	"sanntid/project/config"
	"sanntid/project/elevator"
	"sanntid/project/network/bcast"
	"sanntid/project/network/message"
	"sanntid/project/network/peers"
)

func main() {
	var id, port string
	flag.StringVar(&id, "id", "", "Unique ID for the elevator")
	flag.StringVar(&port, "port", "localhost:15657", "Simulator address")
	flag.Parse()

	if id == "" {
		panic("--id required")
	}

	elevio.Init(port, elevator.N_FLOORS)

	e := elevator.Elevator{
		Direction: elevator.DirStop,
		Behavior:  elevator.ElevatorBehaviorIdle,
	}

	if elevio.GetFloor() == -1 {
		elevator.FsmOnInitBetweenFloors(&e)
	}

	buttonCh     := make(chan elevio.ButtonEvent)
	floorCh      := make(chan int)
	obstrCh      := make(chan bool)
	txCh         := make(chan message.ElevatorMessage)
	rxCh         := make(chan message.ElevatorMessage)
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

	var hallRequests [elevator.N_FLOORS][2]bool
	peerStates  := make(map[string]elevator.Elevator)
	obstructed  := false

	doorTimer       := time.NewTimer(0); <-doorTimer.C
	motorWatchdog   := time.NewTimer(0); <-motorWatchdog.C
	broadcastTicker := time.NewTicker(config.BroadcastInterval)

	for {
		select {
		case btn := <-buttonCh:
			if elevator.ButtonType(btn.Button) == elevator.ButtonCab {
				if elevator.FsmOnRequestButtonPress(&e, btn.Floor, elevator.ButtonCab) {
					doorTimer.Reset(config.DoorOpenTime)
				}
			} else {
				hallRequests[btn.Floor][btn.Button] = true
				peerStates[id] = e
				applyAssigned(&e, assigner.AssignHallRequests(hallRequests, peerStates, id), doorTimer)
			}
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}

		case floor := <-floorCh:
			motorWatchdog.Reset(config.MotorWatchdogTime)
			if elevator.FsmOnFloorArrival(&e, floor) {
				doorTimer.Reset(config.DoorOpenTime)
				clearServedHall(&e, &hallRequests, floor)
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
			hallRequests = [elevator.N_FLOORS][2]bool{}

		case msg := <-rxCh:
			if msg.ID == id {
				continue
			}
			for f := 0; f < elevator.N_FLOORS; f++ {
				hallRequests[f][0] = hallRequests[f][0] || msg.HallRequests[f][0]
				hallRequests[f][1] = hallRequests[f][1] || msg.HallRequests[f][1]
			}
			peerStates[msg.ID] = message.ToElevator(msg)
			peerStates[id] = e
			applyAssigned(&e, assigner.AssignHallRequests(hallRequests, peerStates, id), doorTimer)
			if e.Behavior == elevator.ElevatorBehaviorMoving {
				motorWatchdog.Reset(config.MotorWatchdogTime)
			}

		case pu := <-peerUpdateCh:
			for _, lost := range pu.Lost {
				delete(peerStates, lost)
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

func clearServedHall(e *elevator.Elevator, hallRequests *[elevator.N_FLOORS][2]bool, floor int) {
	for btn := 0; btn < 2; btn++ {
		if !e.Requests[floor][btn] {
			hallRequests[floor][btn] = false
		}
	}
}
