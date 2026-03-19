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







