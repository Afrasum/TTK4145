package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"sanntid/project/elevator"
)

func loadCabCalls(id string) ([elevator.N_FLOORS]bool, error) {

	var cab [elevator.N_FLOORS]bool
	data, err := os.ReadFile("cab_" + id + ".json")
	if err != nil {

		return cab, nil // no backups are OK at first startup
	}
	err = json.Unmarshal(data, &cab)
	return cab, err
}

func cabOrderBackup(e elevator.Elevator, id string) error {
	var cab [elevator.N_FLOORS]bool
	for floor := range cab {
		cab[floor] = e.Requests[floor][elevator.ButtonCab]

	}

	data, _ := json.Marshal(cab)

	if err := os.WriteFile("cab_"+id+".json", data, 0644); err != nil {

		return fmt.Errorf("cab save failed: %w", err)

	}

	//Confirmation read
	confirmed, err := loadCabCalls(id)
	if err != nil || confirmed != cab {
		return fmt.Errorf("cab save verification failed")
	}
	return nil

}
