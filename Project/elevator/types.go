package elevator

const (
	N_FLOORS  = 4
	N_BUTTONS = 3
)

type (
	Direction  int
	Behavior   int
	ButtonType int
)

const (
	DirUp Direction = iota
	DirDown
	DirStop
)

const (
	ElevatorBehaviorMoving Behavior = iota
	ElevatorBehaviorIdle
	ElevatorBehaviorDoorOpen
)

const (
	ButtonHallUp ButtonType = iota
	ButtonHallDown
	ButtonCab
)

type DirectionBehaviorPair struct {
	Direction Direction
	Behavior  Behavior
}

type HallRequest struct {
	Active  bool
	Counter uint16 // counter between 0 and 65535, highest number is latest version
	Unknown bool  // accept any incoming value regardless of counter
}

type Elevator struct {
	Floor     int
	Direction Direction
	Behavior  Behavior
	Requests  [N_FLOORS][N_BUTTONS]bool // matrix of all requests, indexed by floor and button type
}
