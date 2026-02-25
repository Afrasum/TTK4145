package fsm

import (
	"fsm"
	"elevator"
	"time"
)



// set all lights according to the current requests.
func setAllLights(e Elevator) {


	for floor := 0; floor < N_FLOORS; floor++ {
		for btn := 0; btn < N_BUTTONS; btn++ {
			elevatorRequestButtonLight(floor, btn, e.Requests[floor][btn])
		}

	}
}


// initialize the elevator state machine. 
// called at the start of the program, all elevators are driven to first floor
func FsmOnInitBetweenFloors(e *Elevator) {
	elevatorMotorDirection(DirDown)
	e.direction = DirDown
	e.behavior = ElevatorBehaviorMoving

}

//allocate request to elevator 
func FsmOnrequestButtonPressed(e *Elevator, floor int, btn ButtonType) {
	fmt.println("Button pressed: ", floor, btn)
	elevatorPrint(*e)

	switch  e.behavior {

	case elevatorBehaviorIdle:
		e.requests[floor][btn] = true
		pair := requestsChooseDirection(*e)
		e.direction = pair.Direction
		e.behavior = pair.Behavior

		switch pair.behavior {
			
		case elevatorBehaviorMoving:
			elevatorMotorDirection(e.direction)
		case elevatorBehaviorDoorOpen:
			elevatorDoorOpenLight(true)
			timerStart(e.Config.doorOpenTime)
			*e = requestsClearAtCurrentFloor(*e)

		case elevatorBehaviorIdle:
			// do nothing 
		}
		




	case elevatorBehaviorMoving:
		e.requests[floor][btn] = true

	
	
	case elevatorBehaviorDoorOpen:
		if requestShouldClearImmediately(*e, btnFloor, btnType) {

		timerStart(e.Config.doorOpenTime)
		} else{
			e.requests[floor][btn] = true
		}
		
		
	}

	setAllLights(*e)
	fmt.println("New state: ")
	elevatorPrint(*e)
}


//Arrival on floor 
func FsmOnFloorArrival(e *Elevator, newFloor int) {
	fmt.println("Arrived at floor: ", newFloor)
	elevatorPrint(*e)
	e.floor = newFloor
	elevatorFloorIndicator(e.floor)

	switch e.Behavior {

		case elevatorBehaviorMoving:
			if elevatorShouldStop(*e) {
				elevatorMotorDirection(DirStop)
				elevatorDoorOpenLight(true)
				*e = requestsClearAtCurrentFloor(*e)
				timerStart(e.Config.doorOpenTime)
				setAllLights(*e)
				e.behavior = ElevatorBehaviorDoorOpen
			}

		}
	fmt.println("New state: ")
	elevatorPrint(*e)
}



//Door timeout
func FsmOnDoorTimeout(e *Elevator) {
	fmt.println("Door timeout")
	elevatorPrint(*e)

	switch e.Behavior {

	case elevatorBehaviorDoorOpen:
		pair := requestsChooseDirection(*e)
		e.Direction = pair.Direction
		e.Behavior = pair.Behavior

		switch e.Behavior {
		case elevatorBehaviorDoorOpen:
			timerStart(e.Config.doorOpenTime)
			*e = requestsClearAtCurrentFloor(*e)
			setAllLights(*e)

		case elevatorBehaviorMoving, elevatorBehaviorIdle:
			elevatorDoorOpenLight(false)
			elevatorMotorDirection(e.Direction)
			
		}

	}
	fmt.println("New state: ")
	elevatorPrint(*e)
}