package storage

import (
	"encoding/json"
	"os"

	"sanntid/project/elevator"
)

func SaveCabCalls(id string, cabCalls [elevator.N_FLOORS]bool) error {
	data, err := json.Marshal(cabCalls)
	if err != nil {
		return err
	}
	return os.WriteFile("cab_backup_"+id+".json", data, 0644)
}

func LoadCabCalls(id string) [elevator.N_FLOORS]bool {
	var cabCalls [elevator.N_FLOORS]bool
	data, err := os.ReadFile("cab_backup_" + id + ".json")
	if err != nil {
		return cabCalls
	}
	json.Unmarshal(data, &cabCalls)
	return cabCalls
}
