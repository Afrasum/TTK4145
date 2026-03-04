package assigner

import (
	"sanntid/project/config"
	"sanntid/project/elevator"
)

func timeToIdle(e elevator.Elevator) int {
	duration := 0
	for {
		switch e.Behavior {
		case elevator.ElevatorBehaviorIdle:
			return duration
		case elevator.ElevatorBehaviorMoving:
			e.Floor += dirToFloorDelta(e.Direction)
			duration += int(config.TravelTime.Milliseconds())
			if elevator.RequestsShouldStop(e) {
				e = elevator.RequestsClearAtCurrentFloor(e)
				e.Behavior = elevator.ElevatorBehaviorDoorOpen
			}
		case elevator.ElevatorBehaviorDoorOpen:
			e = elevator.RequestsClearAtCurrentFloor(e)
			pair := elevator.RequestsChooseDirection(e)
			e.Direction = pair.Direction
			e.Behavior = pair.Behavior
			duration += int(config.DoorOpenTime.Milliseconds())
		}
	}
}

func dirToFloorDelta(d elevator.Direction) int {
	switch d {
	case elevator.DirUp:
		return 1
	case elevator.DirDown:
		return -1
	default:
		return 0
	}
}

func AssignHallRequests(hallRequests [elevator.N_FLOORS][2]bool, states map[string]elevator.Elevator, localID string) [elevator.N_FLOORS][2]bool {
	var assigned [elevator.N_FLOORS][2]bool

	for floor := 0; floor < elevator.N_FLOORS; floor++ {
		for btn := 0; btn < 2; btn++ {
			if !hallRequests[floor][btn] {
				continue
			}

			bestCost := -1
			bestID := ""

			for id, e := range states {
				sim := e
				sim.Requests[floor][btn] = true
				cost := timeToIdle(sim)

				if bestCost == -1 || cost < bestCost || (cost == bestCost && id < bestID) {
					bestCost = cost
					bestID = id
				}
			}

			if bestID == localID {
				assigned[floor][btn] = true
			}
		}
	}
	return assigned
}
