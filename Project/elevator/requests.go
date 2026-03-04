package elevator

func requestAbove(e Elevator) bool {
	for f := e.Floor + 1; f < N_FLOORS; f++ {
		for b := 0; b < N_BUTTONS; b++ {
			if e.Requests[f][b] {
				return true
			}
		}
	}
	return false
}

func requestBelow(e Elevator) bool {
	for f := e.Floor - 1; f >= 0; f-- {
		for b := 0; b < N_BUTTONS; b++ {
			if e.Requests[f][b] {
				return true
			}
		}
	}
	return false
}

func requestAtCurrentFloor(e Elevator) bool {
	for b := 0; b < N_BUTTONS; b++ {
		if e.Requests[e.Floor][b] {
			return true
		}
	}
	return false
}

func RequestsChooseDirection(e Elevator) DirectionBehaviorPair {
	switch e.Direction {
	case DirUp:
		if requestAbove(e) {
			return DirectionBehaviorPair{DirUp, ElevatorBehaviorMoving}
		} else if requestAtCurrentFloor(e) {
			return DirectionBehaviorPair{DirDown, ElevatorBehaviorDoorOpen}
		} else if requestBelow(e) {
			return DirectionBehaviorPair{DirDown, ElevatorBehaviorMoving}
		}
		return DirectionBehaviorPair{DirStop, ElevatorBehaviorIdle}

	case DirDown:
		if requestBelow(e) {
			return DirectionBehaviorPair{DirDown, ElevatorBehaviorMoving}
		} else if requestAtCurrentFloor(e) {
			return DirectionBehaviorPair{DirUp, ElevatorBehaviorDoorOpen}
		} else if requestAbove(e) {
			return DirectionBehaviorPair{DirUp, ElevatorBehaviorMoving}
		}
		return DirectionBehaviorPair{DirStop, ElevatorBehaviorIdle}

	case DirStop:
		if requestAtCurrentFloor(e) {
			return DirectionBehaviorPair{DirStop, ElevatorBehaviorDoorOpen}
		} else if requestAbove(e) {
			return DirectionBehaviorPair{DirUp, ElevatorBehaviorMoving}
		} else if requestBelow(e) {
			return DirectionBehaviorPair{DirDown, ElevatorBehaviorMoving}
		}
		return DirectionBehaviorPair{DirStop, ElevatorBehaviorIdle}

	default:
		panic("requestsChooseDirection: invalid direction")
	}
}

func RequestsShouldStop(e Elevator) bool {
	switch e.Direction {
	case DirUp:
		return e.Requests[e.Floor][ButtonHallUp] ||
			e.Requests[e.Floor][ButtonCab] ||
			!requestAbove(e)
	case DirDown:
		return e.Requests[e.Floor][ButtonHallDown] ||
			e.Requests[e.Floor][ButtonCab] ||
			!requestBelow(e)
	case DirStop:
		return true
	default:
		panic("requestsShouldStop: invalid direction")
	}
}

func requestsShouldClearImmediately(e Elevator, btnFloor int, btnType ButtonType) bool {
	return e.Floor == btnFloor &&
		(btnType == ButtonCab ||
			(btnType == ButtonHallUp && e.Direction == DirUp) ||
			(btnType == ButtonHallDown && e.Direction == DirDown) ||
			e.Direction == DirStop)
}

func RequestsClearAtCurrentFloor(e Elevator) Elevator {
	e.Requests[e.Floor][ButtonCab] = false
	switch e.Direction {
	case DirUp:
		if !requestAbove(e) && !e.Requests[e.Floor][ButtonHallUp] {
			e.Requests[e.Floor][ButtonHallDown] = false
		}
		e.Requests[e.Floor][ButtonHallUp] = false
	case DirDown:
		if !requestBelow(e) && !e.Requests[e.Floor][ButtonHallDown] {
			e.Requests[e.Floor][ButtonHallUp] = false
		}
		e.Requests[e.Floor][ButtonHallDown] = false
	case DirStop:
		e.Requests[e.Floor][ButtonHallUp] = false
		e.Requests[e.Floor][ButtonHallDown] = false
	}
	return e
}
