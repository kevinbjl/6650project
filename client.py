import websocket
import json
import time
import random
import threading

class LagCompensationClient:
    def __init__(self, server_url, latency_range=(20, 500)):
        self.server_url = server_url
        self.latency_range = latency_range
        self.ws = None

    def connect(self):
        self.ws = websocket.WebSocketApp(
            self.server_url,
            on_message=self.on_message,
            on_error=self.on_error,
            on_close=self.on_close
        )
        threading.Thread(target=self.ws.run_forever).start()

    def simulate_shoot(self):
        # Simulate shooting with variable latency
        latency = random.randint(*self.latency_range)
        perceived_pos = {
            'x': random.randint(0, 9),
            'y': random.randint(0, 9)
        }
        shoot_time = int(time.time() * 1000)

        # Simulate network delay
        time.sleep(latency / 1000)

        shoot_data = {
            'type': 'shoot',
            'timestamp': shoot_time,
            'perceived_pos': perceived_pos,
            'latency': latency
        }

        self.ws.send(json.dumps(shoot_data))

    def on_message(self, ws, message):
        result = json.loads(message)
        print(f"Hit Result: {result['hit']}, Latency: {result.get('latency', 'N/A')}ms")

    def on_error(self, ws, error):
        print(f"WebSocket Error: {error}")

    def on_close(self, ws, close_status_code, close_msg):
        print("WebSocket Connection Closed")

def main():
    client = LagCompensationClient('ws://localhost:8080/ws')
    client.connect()

    # Simulate multiple shots
    for _ in range(10):
        client.simulate_shoot()
        time.sleep(random.uniform(0.5, 2))

if __name__ == "__main__":
    main()