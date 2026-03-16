package message

import (
	"sanntid/project/config"
	"sanntid/project/elevator"
)

type ElevatorMessage struct {
	Token        string
	ID           string
	Floor        int
	Direction    elevator.Direction
	Behavior     elevator.Behavior
	CabRequests  [elevator.N_FLOORS]bool
	HallRequests [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]elevator.HallRequest
	HeardPeers   []string
}

func FromElevator(id string, e elevator.Elevator, hallRequests [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]elevator.HallRequest, heardPeers []string) ElevatorMessage {
	var cab [elevator.N_FLOORS]bool
	for f := range e.Requests {
		cab[f] = e.Requests[f][elevator.ButtonCab]
	}
	return ElevatorMessage{
		Token:        config.GroupToken,
		ID:           id,
		Floor:        e.Floor,
		Direction:    e.Direction,
		Behavior:     e.Behavior,
		CabRequests:  cab,
		HallRequests: hallRequests,
		HeardPeers:   heardPeers,
	}
}

func ToElevator(m ElevatorMessage) elevator.Elevator {
	var e elevator.Elevator
	e.Floor = m.Floor
	e.Direction = m.Direction
	e.Behavior = m.Behavior
	for f := 0; f < elevator.N_FLOORS; f++ {
		e.Requests[f][elevator.ButtonCab] = m.CabRequests[f]
	}
	return e
}
