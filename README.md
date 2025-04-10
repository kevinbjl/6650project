# Introduction

This is a simple project that implements the lag compensation mechanism used in online games.

# Architecture

Server (Go):

1. Maintains game state
2. Stores historical player/target positions in Redis
3. Implements rewind-based hit detection
4. Exposes WebSocket API

Client (Python):

1. Simulates network latency
2. Receives target position from the server
3. Sends shooting commands
4. Receives hit detection results
5. Provides visualization to the user

Key Components:

1. Position Tracking
2. Latency Simulation
3. Rewind-based Hit Detection
4. State Synchronization

Proposed Workflow:

Client sends shoot command with:

1. Timestamp
2. Shooter's aiming position
3. Target position
4. Simulated network delay

Server:

1. Receives command
2. Rewinds game state to command's perceived time
3. Calculates hit detection
4. Returns result with server-side verification

# How to run

First of all, make sure you have redis installed.

1. Start redis server:
   `redis-server`
2. (Optional) start redis CLI in another terminal:
   `redis-cli`

   Then monitor real-time activities:
   `monitor`

3. Run the client:
   `python client.py`
