package assigner

import ( "os/exec"
 		"fmt"
 		"encoding/json" 
 		"runtime")


type HRAElevState struct {
	Behavior    string      `json:"behaviour"`
	Floor       int         `json:"floor"`
	Direction   string      `json:"direction"`
	CabRequests []bool      `json:"cabRequests"`
}

type HRAInput struct {
	HallRequests    [][2]bool                   `json:"hallRequests"`
	States          map[string]HRAElevState     `json:"states"`
}

func AssignRequests(localID string,
	allStates map[string]HRAElevState,
	hallRequests [][2]bool) map[string][][2]bool {


    	hraExecutable := ""
    	switch runtime.GOOS {
   		case "linux":   hraExecutable = "hall_request_assigner"
   		case "windows": hraExecutable = "hall_request_assigner.exe"
    	default:        panic("OS not supported")
	}

	input := HRAInput{
		HallRequests: hallRequests,
		States: allStates,
	}

	jsonBytes, err := json.Marshal(input)
	if err != nil{
		fmt.Println("json.Marshal error: ", err)
		return nil
	}

	ret, err := exec.Command("../hall_request_assigner/+hraExecutable"+hraExecutable, "-i", string(jsonBytes)).CombinedOutput()
    if err != nil {
        fmt.Println("HRA exec error:", err)
        return nil
    }

	output := new(map[string][][2]bool)
    if err = json.Unmarshal(ret, output); err != nil {
        fmt.Println("HRA unmarshal error:", err)
        return nil
    }

	return *output
}