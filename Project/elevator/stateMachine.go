package elevator

import "Driver-go/elevio"

func setAllLights(es Elevator) {
	for floor := range N_FLOORS {
		for button := range N_BUTTONS {
			elevio.SetButtonLamp(elevio.ButtonType(button), floor, es.Requests[floor][button])
		}
	}
}

func SetHallLamps(hallActive [N_FLOORS][2]bool) {
	for floor := range N_FLOORS {
		elevio.SetButtonLamp(elevio.ButtonType(ButtonHallUp), floor, hallActive[floor][0])
		elevio.SetButtonLamp(elevio.ButtonType(ButtonHallDown), floor, hallActive[floor][1])
	}
}

func dirToMotor(d Direction) elevio.MotorDirection {
	switch d {
	case DirUp:
		return elevio.MD_Up
	case DirDown:
		return elevio.MD_Down
	default:
		return elevio.MD_Stop
	}
}

func FsmOnInitBetweenFloors(e *Elevator) {
	elevio.SetMotorDirection(elevio.MD_Down)
	e.Direction = DirDown
	e.Behavior = ElevatorBehaviorMoving
}

func FsmOnFloorArrival(e *Elevator, newFloor int) (startTimer bool) {
	e.Floor = newFloor
	elevio.SetFloorIndicator(e.Floor)

	if e.Behavior == ElevatorBehaviorMoving && requestsShouldStop(*e) {
		elevio.SetMotorDirection(elevio.MD_Stop)
		elevio.SetDoorOpenLamp(true)
		*e = requestsClearAtCurrentFloor(*e)
		setAllLights(*e)
		e.Behavior = ElevatorBehaviorDoorOpen
		return true
	}
	return false
}

func FsmOnDoorTimeout(e *Elevator) (startTimer bool) {
	if e.Behavior != ElevatorBehaviorDoorOpen {
		return false
	}

	pair := requestsChooseDirection(*e)
	e.Direction = pair.Direction
	e.Behavior = pair.Behavior

	switch e.Behavior {
	case ElevatorBehaviorDoorOpen:
		*e = requestsClearAtCurrentFloor(*e)
		setAllLights(*e)
		return true
	case ElevatorBehaviorMoving, ElevatorBehaviorIdle:
		elevio.SetDoorOpenLamp(false)
		elevio.SetMotorDirection(dirToMotor(e.Direction))
	}
	return false
}

func FsmOnRequestButtonPress(e *Elevator, btnFloor int, btnType ButtonType) (startTimer bool) {
	switch e.Behavior {
	case ElevatorBehaviorDoorOpen:
		if requestsShouldClearImmediately(*e, btnFloor, btnType) {
			return true
		}
		e.Requests[btnFloor][btnType] = true
	case ElevatorBehaviorMoving:
		e.Requests[btnFloor][btnType] = true
	case ElevatorBehaviorIdle:
		e.Requests[btnFloor][btnType] = true
		pair := requestsChooseDirection(*e)
		e.Direction = pair.Direction
		e.Behavior = pair.Behavior
		switch pair.Behavior {
		case ElevatorBehaviorDoorOpen:
			elevio.SetDoorOpenLamp(true)
			*e = requestsClearAtCurrentFloor(*e)
			startTimer = true
		case ElevatorBehaviorMoving:
			elevio.SetMotorDirection(dirToMotor(e.Direction))
		}
	}
	setAllLights(*e)
	return startTimer
}
