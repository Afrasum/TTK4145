package config

import "time"

const (
	HallCounterN      = 65535 // max value of uint16 counter
	HeartbeatBasePort = 30000 // UDP port base for primary/backup heartbeat; port = base + id

	DoorOpenTime      = 3 * time.Second
	TravelTime        = 2500 * time.Millisecond
	MotorWatchdogTime = 5 * time.Second

	PeerPort          = 15647
	BcastPort         = 16569
	BroadcastInterval = 100 * time.Millisecond

	HallRequestWatchdogTime   = 10 * time.Second
	HallWatchdogCheckInterval = 2 * time.Second

	// GroupToken is included in every broadcast message so we silently drop
	// messages from other groups running on the same network segment.
	GroupToken = "sanntid_gr42_xK9m"
)
