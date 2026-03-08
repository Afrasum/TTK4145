package elevator

// cyclicIsAfter reports whether incoming is "after" local in a cyclic uint16 counter.
// Special cases handle the wrap boundary: if local is at max and incoming is 0,
// the incoming counter has just wrapped and is considered newer (returns true).
// If local is 0 and incoming is at max, incoming is the old pre-wrap value (returns false).
func CyclicIsAfter(incoming, local uint16) bool {
	const maxCounter = 65535
	if local == maxCounter && incoming == 0 {
		return true
	}
	if local == 0 && incoming == maxCounter {
		return false
	}
	return incoming > local
}

// MergeHallRequest combines local hall-request state with a peer's state.
// Unknown means we have no prior state (e.g., after restart) and accept the peer's
// value unconditionally. When both counters are at max, Active fields are OR-ed to
// avoid losing a request during counter synchronization.
func MergeHallRequest(ours, theirs HallRequest) HallRequest {
	const maxCounter = 65535
	if ours.Unknown {
		return HallRequest{Active: theirs.Active, Counter: theirs.Counter, Unknown: false}
	}
	if ours.Counter == maxCounter && theirs.Counter == maxCounter {
		return HallRequest{Active: ours.Active || theirs.Active, Counter: maxCounter}
	}
	if CyclicIsAfter(theirs.Counter, ours.Counter) {
		return theirs
	}
	return ours
}

// ConfirmedHallRequests extracts the Active field from each HallRequest into a plain bool array.
func ConfirmedHallRequests(hallRequests [N_FLOORS][N_HALL_BUTTONS]HallRequest) [N_FLOORS][N_HALL_BUTTONS]bool {
	var out [N_FLOORS][N_HALL_BUTTONS]bool
	for floor := range hallRequests {
		for btn := range hallRequests[floor] {
			out[floor][btn] = hallRequests[floor][btn].Active
		}
	}
	return out
}
