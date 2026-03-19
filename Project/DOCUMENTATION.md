# Elevator project for group 34. 
===============================


"Create software for controlling `n` elevators working in parallel across `m` floors."


## Modules
-------------------------------

### Assigner
- Assignes hall orders to the best elevator for each order. 
- Uses the provided hall_request_assigner.

### Backup
- Handles backup of cab calls in case of an elevator that crashes.
    - Stores cab calls in 3 seperate json files for redundancy.
    - In case of a crash, the cab orders are stored and will be resumed when the elevator presumes operation. 
- Also has process pairs functionality in case of a crash.
    - One primary process and one backup. Primary sens heartbeat every 0.1 seconds. If backup doesnt hear any heartbeats for 3 seconds, it becomes the primary and spawns a new backup. 


### Config 
- Defines various global variables used in the project

### Elevator
- Handles requests and hall requests
- Defines several types and structs used in the elevator 
- The elevator state machine

### Network
- Handles communication between elevators by using UDP 
    - Messages between elevators
    - Keep tracks of if peers are lost or active

### Main

The main.go module is the head of the system that coordinates everything.
It initializes all the required modules and connects them into one cohesive,
functional elevator system. Its responsibilities can be divided into two
parts: startup and runtime.

Startup responsibilities:
- Parses command-line arguments such as elevator ID and port, and validates them.
- Starts backup and heartbeat functionality.
- Connects to the elevator server.
- Initializes the elevator hardware interface.
- Creates the local elevator state.
- Checks whether the elevator starts between floors and initializes accordingly.
- Loads saved cab calls from backup storage.
- Creates channels for communication between goroutines.
- Starts polling for button presses, floor sensor updates, and obstruction signals.
- Starts the broadcaster and receiver for network communication.
- Starts peer discovery and peer update handling.
- Initializes timers, watchdogs, and periodic tickers.

Runtime responsibilities:
- Runs the main event loop.
- Handles cab and hall button presses.
- Updates the finite state machine on floor arrivals.
- Handles door timer and obstruction events.
- Monitors the motor watchdog for fault detection.
- Receives and processes messages from peer elevators.
- Updates the local view of active peers and shared hall requests.
- Triggers the hall request assigner when reassignment is needed.
- Applies assigned hall requests to the local elevator.
- Periodically broadcasts the local elevator state.
- Monitors hall requests for timeout and force-serves them if necessary.
- Updates lamps and saves cab calls when the state changes.







