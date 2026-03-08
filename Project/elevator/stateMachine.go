package elevator

import (
	"Driver-go/elevio"
	"fmt"
)

func setAllLights(es Elevator) {
	for floor := range N_FLOORS {
		for button := range N_BUTTONS {
			elevio.SetButtonLamp(elevio.ButtonType(button), floor, es.Requests[floor][button])
		}
	}
}

func SetHallLamps(hallActive [N_FLOORS][N_HALL_BUTTONS]bool) {
	for floor := range N_FLOORS {
		elevio.SetButtonLamp(elevio.ButtonType(ButtonHallUp), floor, hallActive[floor][0])
		elevio.SetButtonLamp(elevio.ButtonType(ButtonHallDown), floor, hallActive[floor][1])
	}
}

func directionToString(d Direction) string {
	switch d {
	case DirUp:
		return "up"
	case DirDown:
		return "down"
	default:
		return "stop"
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
	fmt.Println("[FSM] init between floors → moving down")
	elevio.SetMotorDirection(elevio.MD_Down)
	e.Direction = DirDown
	e.Behavior = ElevatorBehaviorMoving
}

func FsmOnFloorArrival(e *Elevator, newFloor int) (startTimer bool) {
	e.Floor = newFloor
	elevio.SetFloorIndicator(e.Floor)

	if e.Behavior == ElevatorBehaviorMoving && requestsShouldStop(*e) {
		fmt.Printf("[FSM] arrived floor %d → door open\n", newFloor)
		elevio.SetMotorDirection(elevio.MD_Stop)
		elevio.SetDoorOpenLamp(true)
		*e = requestsClearAtCurrentFloor(*e)
		setAllLights(*e)
		e.Behavior = ElevatorBehaviorDoorOpen
		return true
	}
	fmt.Printf("[FSM] passed floor %d\n", newFloor)
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
		fmt.Printf("[FSM] door timeout floor %d → door open again\n", e.Floor)
		*e = requestsClearAtCurrentFloor(*e)
		setAllLights(*e)
		return true
	case ElevatorBehaviorMoving:
		fmt.Printf("[FSM] door closed floor %d → moving %s\n", e.Floor, directionToString(e.Direction))
		elevio.SetDoorOpenLamp(false)
		elevio.SetMotorDirection(dirToMotor(e.Direction))
	case ElevatorBehaviorIdle:
		fmt.Printf("[FSM] door closed floor %d → idle\n", e.Floor)
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
			fmt.Printf("[FSM] request floor %d → door open\n", btnFloor)
			elevio.SetDoorOpenLamp(true)
			*e = requestsClearAtCurrentFloor(*e)
			startTimer = true
		case ElevatorBehaviorMoving:
			fmt.Printf("[FSM] request floor %d → moving %s\n", btnFloor, directionToString(e.Direction))
			elevio.SetMotorDirection(dirToMotor(e.Direction))
		}
	}
	setAllLights(*e)
	return startTimer
}
