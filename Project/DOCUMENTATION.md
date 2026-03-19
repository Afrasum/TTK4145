Elevator project for group 34. 
===============================


"Create software for controlling `n` elevators working in parallel across `m` floors."


Modules
-------------------------------

### Assigner



### Backup


### Config 

### Elevator

### Network

### Main
The main.go module is the head of the system that coordinates everything. It initializes all the required modules and connects them into one cohesive, functional elevator system. Its responsibilities can be divided into two parts: startup and runtime.


Startup resposnibility: 
 - Parses command line arguments such as elevator id and port and validates it. 
 - Starts backup and herthbeat functionality. 
 - Connects to the elvator server.
 - Initilaizes elevator hardweare interface. 
 - Creates the local elevator state. 
 - Check if the elevator starts between floors and initlaize accordinlgy.
 - loadssaved cab calls from backupstroage.
 - creates channles for communication between go rutines. 
 - starts polling for botton press, fllor sensor updates, and obstruction signals.
 - start the broadcaster and reciver for network communication 
 - starts peer discovery and peer upodate handling 
 - initlaizez timers ,watch dog nd peridoci ticks.

Runtime responsibility:
 - runs the main event loop
 - handle cab u
 - 
 







