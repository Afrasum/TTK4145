package networkmsg

import "sanntid/project/elevator"

type ElevatorState struct {
	Behavior    string `json:"behaviour"`
	Floor       int    `json:"floor"`
	Direction   string `json:"direction"`
	CabRequests []bool `json:"cabRequests"`
}

type NetworkMessage struct {
	SenderID     string                                      `json:"senderID"`
	State        ElevatorState                               `json:"state"`
	HallRequests [elevator.N_FLOORS][2]bool                  `json:"hallRequests"`
	Seq          uint64                                      `json:"seq"`
}

func behaviorToString(b elevator.Behavior) string {
	switch b {
	case elevator.ElevatorBehaviorMoving:
		return "moving"
	case elevator.ElevatorBehaviorDoorOpen:
		return "doorOpen"
	default:
		return "idle"
	}
}

func stringToBehavior(s string) elevator.Behavior {
	switch s {
	case "moving":
		return elevator.ElevatorBehaviorMoving
	case "doorOpen":
		return elevator.ElevatorBehaviorDoorOpen
	default:
		return elevator.ElevatorBehaviorIdle
	}
}

func directionToString(d elevator.Direction) string {
	switch d {
	case elevator.DirUp:
		return "up"
	case elevator.DirDown:
		return "down"
	default:
		return "stop"
	}
}

func stringToDirection(s string) elevator.Direction {
	switch s {
	case "up":
		return elevator.DirUp
	case "down":
		return elevator.DirDown
	default:
		return elevator.DirStop
	}
}

func ElevatorToState(e elevator.Elevator) ElevatorState {
	cabReqs := make([]bool, elevator.N_FLOORS)
	for f := 0; f < elevator.N_FLOORS; f++ {
		cabReqs[f] = e.Requests[f][elevator.ButtonCab]
	}
	return ElevatorState{
		Behavior:    behaviorToString(e.Behavior),
		Floor:       e.Floor,
		Direction:   directionToString(e.Direction),
		CabRequests: cabReqs,
	}
}

func StateToElevator(s ElevatorState) elevator.Elevator {
	e := elevator.Elevator{
		Floor:     s.Floor,
		Direction: stringToDirection(s.Direction),
		Behavior:  stringToBehavior(s.Behavior),
	}
	for f := 0; f < elevator.N_FLOORS; f++ {
		if f < len(s.CabRequests) {
			e.Requests[f][elevator.ButtonCab] = s.CabRequests[f]
		}
	}
	return e
}
