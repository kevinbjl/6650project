import pygame
import websocket
import json
import threading
import time

# Constants
WIDTH, HEIGHT = 1280, 720
TARGET_RADIUS = 5 # TODO: this should be stored in the server
CROSSHAIR_SIZE = 15
MUZZLE_FLASH_TIME = 100  # Flash duration in milliseconds
HIT_MARKER_TIME = 500   # Hit marker display duration in milliseconds

# Initialize Pygame
pygame.init()
screen = pygame.display.set_mode((WIDTH, HEIGHT))
pygame.display.set_caption("Lag Compensation Test")
clock = pygame.time.Clock()

# Load Assets
flash_image = pygame.image.load("./assets/muzzle-flash.png")  # Muzzle flash
background = pygame.image.load("./assets/Screenshot-Va.jpg")  # Replace with your background
flash_image = pygame.transform.scale(flash_image, (70, 70))
background = pygame.transform.scale(background, (WIDTH, HEIGHT))

# WebSocket
client_start_time = int(time.time() * 1000)
target_position = None  # Single position instead of list
last_hit_result = None
hit_marker_start_time = 0
server_offset = 0  # Clock offset between client and server
measured_latency = 0  # Measured one-way latency in ms
last_sync_time = 0  # Time when last sync was sent
last_ping_time = 0  # Time when last ping was sent
ping_interval = 1.0  # Send ping every 1 second
sync_interval = 5.0  # Full sync every 5 seconds

def on_error(ws, error):
    print(f"WebSocket Error: {error}")

def on_message(ws, message):
    global target_position, last_hit_result, hit_marker_start_time, server_offset, measured_latency, last_sync_time, last_ping_time
    try:
        data = json.loads(message)
        
        if data["type"] == "position":
            target_position = {
                "x": data["position"]["x"], 
                "y": data["position"]["y"]
            }
        elif data["type"] == "hit_result":
            last_hit_result = data
            hit_marker_start_time = pygame.time.get_ticks()
            print(f"Hit result: {data}")  # Debug print
        elif data["type"] == "sync_response":
            # Calculate RTT and latency
            client_time = data["clientTime"]
            server_time = data["serverTime"]
            current_time = int(time.time() * 1000 - client_start_time)
            
            # RTT = (current_time - last_sync_time)
            rtt = current_time - last_sync_time
            measured_latency = rtt // 2  # One-way latency
            
            # Calculate clock offset
            server_offset = server_time - client_time
            
            print(f"Clock synchronized - Offset: {server_offset}ms, RTT: {rtt}ms, Latency: {measured_latency}ms")
            
            # Send latency update to server
            send_latency_update(ws, rtt)
        elif data["type"] == "pong":
            # Calculate RTT from ping-pong
            current_time = int(time.time() * 1000 - client_start_time)
            rtt = current_time - last_ping_time
            measured_latency = rtt // 2  # One-way latency
            
            # Update the displayed latency
            print(f"Ping RTT: {rtt}ms, Latency: {measured_latency}ms")
    except Exception as e:
        print(f"Error processing message: {e}")

def send_sync(ws):
    global last_sync_time
    last_sync_time = int(time.time() * 1000 - client_start_time)
    sync_data = {
        "type": "sync",
        "timestamp": last_sync_time
    }
    try:
        ws.send(json.dumps(sync_data))
    except Exception as e:
        print(f"WebSocket send error: {e}")

def send_ping(ws):
    global last_ping_time
    last_ping_time = int(time.time() * 1000 - client_start_time)
    ping_data = {
        "type": "ping",
        "timestamp": last_ping_time
    }
    try:
        ws.send(json.dumps(ping_data))
    except Exception as e:
        print(f"WebSocket send error: {e}")

def send_latency_update(ws, rtt):
    latency_data = {
        "type": "latency_update",
        "rtt": rtt
    }
    try:
        ws.send(json.dumps(latency_data))
    except Exception as e:
        print(f"WebSocket send error: {e}")

def send_shoot(ws, x, y):
    shoot_data = {
        "type": "shoot",
        "timestamp": int(time.time() * 1000 - client_start_time),
        "x": x,
        "y": y,
    }
    try:
        ws.send(json.dumps(shoot_data))
    except Exception as e:
        print(f"WebSocket send error: {e}")

def draw_hit_marker(screen, hit_result):
    if not hit_result:
        return
    
    current_time = pygame.time.get_ticks()
    if current_time - hit_marker_start_time > HIT_MARKER_TIME:
        return

    # Draw hit marker
    color = (0, 255, 0) if hit_result["hit"] else (255, 0, 0)
    size = 20
    thickness = 2
    
    # Draw X mark
    pygame.draw.line(screen, color, (WIDTH//2 - size, HEIGHT//2 - size), 
                    (WIDTH//2 + size, HEIGHT//2 + size), thickness)
    pygame.draw.line(screen, color, (WIDTH//2 - size, HEIGHT//2 + size), 
                    (WIDTH//2 + size, HEIGHT//2 - size), thickness)

def game_loop():
    global target_position, last_hit_result, server_offset, measured_latency, last_sync_time, last_ping_time
    
    # Enable WebSocket tracing for debugging
    websocket.enableTrace(True)
    
    # Create WebSocket with callback methods
    ws = websocket.WebSocketApp(
        "ws://localhost:8080/ws", 
        on_message=on_message,
        on_error=on_error,
    )
    
    # Start WebSocket in a separate thread
    ws_thread = threading.Thread(target=ws.run_forever, daemon=True)
    ws_thread.start()
    
    # Wait for connection to establish
    time.sleep(1)
    
    # Initial clock synchronization
    send_sync(ws)
    
    running = True
    muzzle_flash = False
    flash_start_time = 0
    last_sync_check_time = time.time()
    last_ping_check_time = time.time()
    
    while running:
        current_time = time.time()
        
        # Sync clocks every 5 seconds
        if current_time - last_sync_check_time > sync_interval:
            send_sync(ws)
            last_sync_check_time = current_time
        
        # Send ping every 1 second for more frequent latency updates
        if current_time - last_ping_check_time > ping_interval:
            send_ping(ws)
            last_ping_check_time = current_time
        
        screen.blit(background, (0, 0))
        
        for event in pygame.event.get():
            if event.type == pygame.QUIT:
                running = False
            elif event.type == pygame.MOUSEBUTTONDOWN:
                mx, my = pygame.mouse.get_pos()
                send_shoot(ws, mx, my)
                muzzle_flash = True
                flash_start_time = pygame.time.get_ticks()
        
        # Draw target if position exists
        if target_position:
            pygame.draw.circle(screen, (255, 0, 0), (target_position["x"], target_position["y"]), TARGET_RADIUS)
        
        # Draw crosshair
        mx, my = pygame.mouse.get_pos()
        pygame.draw.line(screen, (0, 255, 0), (mx - CROSSHAIR_SIZE, my), (mx + CROSSHAIR_SIZE, my), 2)
        pygame.draw.line(screen, (0, 255, 0), (mx, my - CROSSHAIR_SIZE), (mx, my + CROSSHAIR_SIZE), 2)
        
        # Draw muzzle flash if active
        if muzzle_flash:
            screen.blit(flash_image, (WIDTH // 2 - 25, HEIGHT - 250))
            if pygame.time.get_ticks() - flash_start_time > MUZZLE_FLASH_TIME:
                muzzle_flash = False
        
        # Draw hit marker
        draw_hit_marker(screen, last_hit_result)
        
        # Display latency information
        font = pygame.font.SysFont(None, 24)
        latency_text = font.render(f"Latency: {measured_latency}ms", True, (255, 255, 255))
        screen.blit(latency_text, (10, 10))
        
        pygame.display.flip()
        clock.tick(60)
    
    ws.close()
    pygame.quit()

if __name__ == "__main__":
    game_loop()