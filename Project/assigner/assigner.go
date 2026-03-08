package assigner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"sanntid/project/elevator"
)

type hraElevState struct {
	Behavior    string `json:"behaviour"`
	Floor       int    `json:"floor"`
	Direction   string `json:"direction"`
	CabRequests []bool `json:"cabRequests"`
}

type hraInput struct {
	HallRequests [][elevator.N_HALL_BUTTONS]bool `json:"hallRequests"`
	States       map[string]hraElevState `json:"states"`
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

func toHRAState(e elevator.Elevator) hraElevState {
	cab := make([]bool, elevator.N_FLOORS)
	for f := 0; f < elevator.N_FLOORS; f++ {
		cab[f] = e.Requests[f][elevator.ButtonCab]
	}
	behavior := behaviorToString(e.Behavior)
	// Assigner rejects moving off an end floor — happens during init between floors
	// before the first floor sensor event updates e.Floor
	if e.Behavior == elevator.ElevatorBehaviorMoving {
		if e.Direction == elevator.DirDown && e.Floor <= 0 {
			behavior = "idle"
		} else if e.Direction == elevator.DirUp && e.Floor >= elevator.N_FLOORS-1 {
			behavior = "idle"
		}
	}
	return hraElevState{
		Behavior:    behavior,
		Floor:       e.Floor,
		Direction:   directionToString(e.Direction),
		CabRequests: cab,
	}
}

func binaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		panic("assigner: could not find executable path: " + err.Error())
	}
	dir := filepath.Dir(exe)
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(dir, "assigner", "hall_request_assigner", "hall_request_assigner.exe")
	default:
		return filepath.Join(dir, "assigner", "hall_request_assigner", "hall_request_assigner")
	}
}

// AssignHallRequests returns which hall requests localID should serve.
func AssignHallRequests(
	hallRequests [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]elevator.HallRequest,
	states map[string]elevator.Elevator,
	localID string,
) [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]bool {

	hraStates := make(map[string]hraElevState)
	for id, e := range states {
		hraStates[id] = toHRAState(e)
	}

	hrSlice := make([][elevator.N_HALL_BUTTONS]bool, elevator.N_FLOORS)
	for f := 0; f < elevator.N_FLOORS; f++ {
		hrSlice[f] = [elevator.N_HALL_BUTTONS]bool{hallRequests[f][0].Active, hallRequests[f][1].Active}
	}

	input := hraInput{HallRequests: hrSlice, States: hraStates}
	jsonBytes, err := json.Marshal(input)
	if err != nil {
		fmt.Println("assigner: json.Marshal error:", err)
		return [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]bool{}
	}

	out, err := exec.Command(binaryPath(), "-i", string(jsonBytes)).CombinedOutput()
	if err != nil {
		fmt.Printf("assigner: exec error: %v\ninput: %s\noutput: %s\n", err, jsonBytes, out)
		return [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]bool{}
	}

	var result map[string][][elevator.N_HALL_BUTTONS]bool
	if err = json.Unmarshal(out, &result); err != nil {
		fmt.Println("assigner: unmarshal error:", err)
		return [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]bool{}
	}

	var assigned [elevator.N_FLOORS][elevator.N_HALL_BUTTONS]bool
	for f, pair := range result[localID] {
		if f < elevator.N_FLOORS {
			assigned[f] = pair
		}
	}
	return assigned
}
