import pygame
import websocket
import json
import threading
import time

# Constants
WIDTH, HEIGHT = 1280, 720
SIDEBAR_WIDTH = 200
GAME_WIDTH = WIDTH - SIDEBAR_WIDTH
TARGET_RADIUS = 5 # TODO: this should be stored in the server
CROSSHAIR_SIZE = 15
MUZZLE_FLASH_TIME = 100  # Flash duration in milliseconds
HIT_MARKER_TIME = 500   # Hit marker display duration in milliseconds
MAX_SIMULATED_LATENCY = 500  # Maximum simulated latency in milliseconds
POSITION_HISTORY_SIZE = 100  # Number of positions to keep in history

# Initialize Pygame
pygame.init()
screen = pygame.display.set_mode((WIDTH, HEIGHT))
pygame.display.set_caption("Lag Compensation Test")
clock = pygame.time.Clock()

# Load Assets
flash_image = pygame.image.load("./assets/muzzle-flash.png")  # Muzzle flash
background = pygame.image.load("./assets/Screenshot-Va.jpg")  # Replace with your background
flash_image = pygame.transform.scale(flash_image, (70, 70))
background = pygame.transform.scale(background, (GAME_WIDTH, HEIGHT))

# WebSocket
client_start_time = int(time.time() * 1000)
target_position = None  # Current position
position_history = []  # Buffer of past positions
last_hit_result = None
hit_marker_start_time = 0
server_offset = 0  # Clock offset between client and server
measured_latency = 0  # Measured one-way latency in ms
last_sync_time = 0  # Time when last sync was sent
sync_interval = 1.0  # Sync every 1 second
simulated_latency = 0  # Simulated latency in milliseconds
slider_rect = pygame.Rect(GAME_WIDTH + 20, 100, SIDEBAR_WIDTH - 40, 20)  # Slider rectangle
slider_knob_rect = pygame.Rect(GAME_WIDTH + 20, 100, 10, 20)  # Slider knob rectangle
is_dragging = False  # Track if slider is being dragged
compensation_enabled = True  # Whether lag compensation is enabled
toggle_rect = pygame.Rect(GAME_WIDTH + 20, 300, SIDEBAR_WIDTH - 40, 30)  # Toggle button rectangle

def on_error(ws, error):
    print(f"WebSocket Error: {error}")

def on_message(ws, message):
    global target_position, last_hit_result, hit_marker_start_time, server_offset, measured_latency, last_sync_time, position_history
    try:
        data = json.loads(message)
        
        if data["type"] == "position":
            # Add timestamp to position data
            pos_data = {
                "x": data["position"]["x"], 
                "y": data["position"]["y"],
                "timestamp": int(time.time() * 1000 - client_start_time)
            }
            position_history.append(pos_data)
            
            # Keep only the last POSITION_HISTORY_SIZE positions
            if len(position_history) > POSITION_HISTORY_SIZE:
                position_history.pop(0)
                
        elif data["type"] == "hit_result":
            last_hit_result = data
            hit_marker_start_time = pygame.time.get_ticks()
            print(f"Hit result: {data}")  # Debug print
        elif data["type"] == "sync_response":
            # Get all timestamps
            t0 = data["clientTime"]  # When we sent
            t1 = data["serverRecvTime"]  # When server received
            t2 = data["serverSendTime"]  # When server sent
            t3 = int(time.time() * 1000 - client_start_time)  # When we received
            
            # Calculate latency (one-way)
            rtt = t3 - t0
            measured_latency = rtt // 2
            
            # Calculate offset: T1 - (T0 + latency)
            server_offset = t1 - (t0 + measured_latency)
            
            print(f"Clock synchronized - Offset: {server_offset}ms, Latency: {measured_latency}ms")
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

def send_shoot(ws, x, y):
    # Add simulated latency to the timestamp
    current_time = int(time.time() * 1000 - client_start_time)
    shoot_data = {
        "type": "shoot",
        "timestamp": current_time - simulated_latency,  # Subtract latency to simulate delay
        "x": x,
        "y": y,
        "offset": server_offset if compensation_enabled else 0,  # Only include offset if compensation is enabled
        "compensation_enabled": compensation_enabled  # Tell server whether to use compensation
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
    pygame.draw.line(screen, color, (GAME_WIDTH//2 - size, HEIGHT//2 - size), 
                    (GAME_WIDTH//2 + size, HEIGHT//2 + size), thickness)
    pygame.draw.line(screen, color, (GAME_WIDTH//2 - size, HEIGHT//2 + size), 
                    (GAME_WIDTH//2 + size, HEIGHT//2 - size), thickness)
    
    # Draw hit/miss text
    font = pygame.font.SysFont(None, 36)
    text = "HIT" if hit_result["hit"] else "MISS"
    text_surface = font.render(text, True, color)
    text_rect = text_surface.get_rect(center=(GAME_WIDTH//2, HEIGHT//2 - 40))
    screen.blit(text_surface, text_rect)

def draw_sidebar():
    # Draw sidebar background
    pygame.draw.rect(screen, (50, 50, 50), (GAME_WIDTH, 0, SIDEBAR_WIDTH, HEIGHT))
    
    # Draw title
    font = pygame.font.SysFont(None, 24)
    title = font.render("Latency Simulator", True, (255, 255, 255))
    screen.blit(title, (GAME_WIDTH + 20, 20))
    
    # Draw slider section
    slider_y = 80
    slider_label = font.render("Simulated Latency:", True, (255, 255, 255))
    screen.blit(slider_label, (GAME_WIDTH + 20, slider_y))
    
    # Draw slider background
    slider_rect.y = slider_y + 30
    pygame.draw.rect(screen, (100, 100, 100), slider_rect)
    
    # Calculate knob position based on simulated latency
    knob_x = GAME_WIDTH + 20 + (simulated_latency / MAX_SIMULATED_LATENCY) * (SIDEBAR_WIDTH - 40)
    slider_knob_rect.x = int(knob_x)
    slider_knob_rect.y = slider_rect.y
    
    # Draw slider knob
    pygame.draw.rect(screen, (200, 200, 200), slider_knob_rect)
    
    # Draw latency value
    latency_text = font.render(f"{simulated_latency}ms", True, (255, 255, 255))
    screen.blit(latency_text, (GAME_WIDTH + 20, slider_rect.y + 30))
    
    # Draw real latency section
    real_latency_y = slider_rect.y + 70
    real_latency_label = font.render("Real Latency:", True, (255, 255, 255))
    screen.blit(real_latency_label, (GAME_WIDTH + 20, real_latency_y))
    real_latency_value = font.render(f"{measured_latency}ms", True, (255, 255, 255))
    screen.blit(real_latency_value, (GAME_WIDTH + 20, real_latency_y + 30))
    
    # Draw compensation toggle section
    toggle_y = real_latency_y + 70
    toggle_label = font.render("Lag Compensation:", True, (255, 255, 255))
    screen.blit(toggle_label, (GAME_WIDTH + 20, toggle_y))
    
    # Draw toggle button
    toggle_rect.y = toggle_y + 30
    toggle_color = (0, 255, 0) if compensation_enabled else (255, 0, 0)
    pygame.draw.rect(screen, toggle_color, toggle_rect)
    toggle_text = font.render("ON" if compensation_enabled else "OFF", True, (0, 0, 0))
    text_rect = toggle_text.get_rect(center=toggle_rect.center)
    screen.blit(toggle_text, text_rect)

def get_delayed_position():
    if not position_history:
        return None
        
    current_time = int(time.time() * 1000 - client_start_time)
    target_time = current_time - simulated_latency
    
    # Find the position closest to the target time
    closest_pos = None
    min_time_diff = float('inf')
    
    for pos in position_history:
        time_diff = abs(pos["timestamp"] - target_time)
        if time_diff < min_time_diff:
            min_time_diff = time_diff
            closest_pos = pos
            
    return closest_pos

def game_loop():
    global target_position, last_hit_result, server_offset, measured_latency, last_sync_time, simulated_latency, is_dragging, compensation_enabled
    
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
    
    while running:
        current_time = time.time()
        
        # Sync clocks every second
        if current_time - last_sync_check_time > sync_interval:
            send_sync(ws)
            last_sync_check_time = current_time
        
        # Draw game background
        screen.blit(background, (0, 0))
        
        for event in pygame.event.get():
            if event.type == pygame.QUIT:
                running = False
            elif event.type == pygame.MOUSEBUTTONDOWN:
                if event.button == 1:  # Left click
                    if slider_knob_rect.collidepoint(event.pos):
                        is_dragging = True
                    elif toggle_rect.collidepoint(event.pos):
                        compensation_enabled = not compensation_enabled
                    else:
                        mx, my = pygame.mouse.get_pos()
                        if mx < GAME_WIDTH:  # Only shoot if clicking in game area
                            send_shoot(ws, mx, my)
                            muzzle_flash = True
                            flash_start_time = pygame.time.get_ticks()
            elif event.type == pygame.MOUSEBUTTONUP:
                if event.button == 1:  # Left click
                    is_dragging = False
            elif event.type == pygame.MOUSEMOTION:
                if is_dragging:
                    # Update simulated latency based on slider position
                    relative_x = event.pos[0] - (GAME_WIDTH + 20)
                    simulated_latency = int((relative_x / (SIDEBAR_WIDTH - 40)) * MAX_SIMULATED_LATENCY)
                    simulated_latency = max(0, min(simulated_latency, MAX_SIMULATED_LATENCY))
        
        # Get delayed position based on simulated latency
        delayed_pos = get_delayed_position()
        
        # Draw target if position exists
        if delayed_pos:
            pygame.draw.circle(screen, (255, 0, 0), (delayed_pos["x"], delayed_pos["y"]), TARGET_RADIUS)
        
        # Draw crosshair
        mx, my = pygame.mouse.get_pos()
        if mx < GAME_WIDTH:  # Only draw crosshair in game area
            pygame.draw.line(screen, (0, 255, 0), (mx - CROSSHAIR_SIZE, my), (mx + CROSSHAIR_SIZE, my), 2)
            pygame.draw.line(screen, (0, 255, 0), (mx, my - CROSSHAIR_SIZE), (mx, my + CROSSHAIR_SIZE), 2)
        
        # Draw muzzle flash if active
        if muzzle_flash:
            screen.blit(flash_image, (WIDTH // 2 - 5, HEIGHT - 300))
            if pygame.time.get_ticks() - flash_start_time > MUZZLE_FLASH_TIME:
                muzzle_flash = False
        
        # Draw hit marker
        draw_hit_marker(screen, last_hit_result)
        
        # Draw sidebar
        draw_sidebar()
        
        pygame.display.flip()
        clock.tick(60)
    
    ws.close()
    pygame.quit()

if __name__ == "__main__":
    game_loop()