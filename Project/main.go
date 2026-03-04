package main

import (
	"Driver-go/elevio"
	"fmt"
	"time"

	"sanntid/project/elevator"
)

const doorOpenTime = 3 * time.Second

func main() {
	fmt.Println("Starting elevator")

	elevio.Init("localhost:15657", elevator.N_FLOORS)

	e := elevator.Elevator{
		Direction: elevator.DirStop,
		Behavior:  elevator.ElevatorBehaviorIdle,
	}

	if elevio.GetFloor() == -1 {
		elevator.FsmOnInitBetweenFloors(&e)
	}

	buttonCh := make(chan elevio.ButtonEvent)
	floorCh := make(chan int)
	doorTimer := time.NewTimer(0)
	<-doorTimer.C

	go elevio.PollButtons(buttonCh)
	go elevio.PollFloorSensor(floorCh)

	for {
		select {
		case buttonEvent := <-buttonCh:
			if elevator.FsmOnRequestButtonPress(&e, buttonEvent.Floor, elevator.ButtonType(buttonEvent.Button)) {
				doorTimer.Reset(doorOpenTime)
			}

		case floor := <-floorCh:
			if elevator.FsmOnFloorArrival(&e, floor) {
				doorTimer.Reset(doorOpenTime)
			}

		case <-doorTimer.C:
			if elevator.FsmOnDoorTimeout(&e) {
				elevator.FsmOnDoorTimeout(&e)
			}
		}
	}
}
