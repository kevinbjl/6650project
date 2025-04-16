# Introduction

This is a simple project that implements the lag compensation mechanism used in online games.

# Architecture

Server (Go):

1. Records game state
2. Stores historical target positions in Redis every 25ms
3. Implements rewind-based hit detection
4. Exposes WebSocket API

Client (Python/JS):

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
Client and server performs clock synchronization every second. The client will calculate the offset based on the synchronization result.

Client sends shoot command with:

1. Timestamp
2. Offset
3. Shooter's aiming position

Server:

1. Receives command
2. Rewinds game state to command's perceived time based on offset
3. Calculates hit detection
4. Returns result with server-side verification

# Algorithm Overview

Note that all timestamps are relative to the start time (depending on the perspective).

The client sends a sync request every second. In the sync request, it includes its current timestamp, let's call it T_0.
When the server receives this request, it'll take a timestamp (let's call it T_1). The server will then respond with a message as soon as possible and take a timestamp upon sending it (let's call it T_2). This response message will include:

1. The timestamp when the server receives the sync request (T_1)
2. The timestamp wehn the server sends back a response (T_2)

Upon receiving this response message, client will record its timestamp (let's call it T_3).
The client then calculates two things:

1. latency = (T_0+T_3)/2
2. offset = T_1 - (T_0 + latency)

So whenever a client sends a shoot request to the server, it includes its current timestamp, and the offset. when the server receives it, the server simply rewinds to its timescamp at (client's shoot time + offset).

# How to run

First of all, make sure you have redis installed.

1. Start redis server:
   `redis-server`
2. (Optional) start redis CLI in another terminal:
   `redis-cli`

   Then monitor real-time activities:
   `monitor`

3. Run the server:
   `cd server`
   `go run .`

4. Run the client:

   For 2D client:
   `python client.py`

   For 3D client:
   `python -m http.server 8000`

   Then open `https://localhost:8000` in the browser
