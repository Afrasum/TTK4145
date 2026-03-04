package message

import "sanntid/project/elevator"

type ElevatorMessage struct {
	ID           int
	Floor        int
	Direction    elevator.Direction
	Behavior     elevator.Behavior
	CabRequests  [elevator.N_FLOORS][2]bool
	HallRequests [elevator.N_FLOORS][2]bool
}
