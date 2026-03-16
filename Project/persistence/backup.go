package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"sanntid/project/elevator"
)

func cabPaths(id string) [3]string {
	return [3]string{
		fmt.Sprintf("cab_%s.json", id),
		fmt.Sprintf("cab_%s_2.json", id),
		fmt.Sprintf("cab_%s_3.json", id),
	}
}

func LoadCabCalls(id string) ([elevator.N_FLOORS]bool, error) {
	paths := cabPaths(id)
	var results [][elevator.N_FLOORS]bool
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cab [elevator.N_FLOORS]bool
		if json.Unmarshal(data, &cab) == nil {
			results = append(results, cab)
		}
	}
	if len(results) == 0 {
		return [elevator.N_FLOORS]bool{}, nil // no backups are OK at first startup
	}
	// Majority vote: pick the value that at least 2 copies agree on
	for i := 0; i < len(results); i++ {
		count := 0
		for j := 0; j < len(results); j++ {
			if results[i] == results[j] {
				count++
			}
		}
		if count >= 2 {
			return results[i], nil
		}
	}
	// No majority — fallback to first readable copy
	return results[0], nil
}

func SaveCabCalls(e elevator.Elevator, id string) error {
	var cab [elevator.N_FLOORS]bool
	for floor := range cab {
		cab[floor] = e.Requests[floor][elevator.ButtonCab]
	}
	data, _ := json.Marshal(cab)

	for _, path := range cabPaths(id) {
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("cab save failed (%s): %w", path, err)
		}
	}

	// Verification: read back and confirm majority matches what we wrote
	confirmed, err := LoadCabCalls(id)
	if err != nil || confirmed != cab {
		return fmt.Errorf("cab save verification failed")
	}
	return nil
}
