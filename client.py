import pygame
import websocket
import json
import threading
import time
import math

# Constants
WIDTH, HEIGHT = 1280, 720
SIDEBAR_WIDTH = 200
GAME_WIDTH = WIDTH - SIDEBAR_WIDTH
TARGET_RADIUS = 30 # TODO: this should be stored in the server
CROSSHAIR_SIZE = 15
MUZZLE_FLASH_TIME = 100  # Flash duration in milliseconds
HIT_MARKER_TIME = 500   # Hit marker display duration in milliseconds
MAX_SIMULATED_LATENCY = 500  # Maximum simulated latency in milliseconds
POSITION_HISTORY_SIZE = 100  # Number of positions to keep in history
HIT_POINT_TIME = 3000  # How long to show hit point in milliseconds
KILLCAM_SIZE = 200  # Size of the kill cam box
KILLCAM_SCALE = 2.0  # How much to zoom in on the target

# Initialize Pygame
pygame.init()
screen = pygame.display.set_mode((WIDTH, HEIGHT))
pygame.display.set_caption("Lag Compensation Test")
clock = pygame.time.Clock()

# Load Assets
flash_image = pygame.image.load("./assets/muzzle-flash.png")  # Muzzle flash
background = pygame.image.load("./assets/Screenshot-Va.jpg")  # Replace with your background
target_image = pygame.image.load("./assets/target.png")  # Target image
flash_image = pygame.transform.scale(flash_image, (70, 70))
background = pygame.transform.scale(background, (GAME_WIDTH, HEIGHT))
target_image = pygame.transform.scale(target_image, (TARGET_RADIUS * 4, TARGET_RADIUS * 4))  # Scale target to appropriate size

# WebSocket
client_start_time = int(time.time() * 1000)
target_position = None  # Current position
position_history = []  # Buffer of past positions
last_hit_result = None
hit_marker_start_time = 0
hit_point_start_time = 0  # Time when hit point started showing
last_hit_point = None  # Store the last hit point coordinates
last_hit_target_pos = None  # Store the target position at the time of hit
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
    global target_position, last_hit_result, hit_marker_start_time, hit_point_start_time, last_hit_point, last_hit_target_pos, server_offset, measured_latency, last_sync_time, position_history
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
            if data["hit"]:
                hit_point_start_time = pygame.time.get_ticks()
                # Store the hit point coordinates
                last_hit_point = {
                    "x": data["hit_x"],
                    "y": data["hit_y"]
                }
                # Store the target position at the time of hit
                last_hit_target_pos = {
                    "x": data["target_x"],
                    "y": data["target_y"]
                }
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

def draw_hit_point():
    if not last_hit_point:
        return
        
    current_time = pygame.time.get_ticks()
    if current_time - hit_point_start_time > HIT_POINT_TIME:
        return
        
    # Draw a larger red dot at the hit point
    pygame.draw.circle(screen, (255, 0, 0), (last_hit_point["x"], last_hit_point["y"]), 5)
    # Draw a thicker white outline around the hit point
    pygame.draw.circle(screen, (255, 255, 255), (last_hit_point["x"], last_hit_point["y"]), 6, 2)
    # Draw a crosshair at the hit point
    size = 8
    pygame.draw.line(screen, (255, 255, 255), 
                    (last_hit_point["x"] - size, last_hit_point["y"]), 
                    (last_hit_point["x"] + size, last_hit_point["y"]), 1)
    pygame.draw.line(screen, (255, 255, 255), 
                    (last_hit_point["x"], last_hit_point["y"] - size), 
                    (last_hit_point["x"], last_hit_point["y"] + size), 1)

def draw_killcam():
    if not last_hit_point or not last_hit_target_pos:
        return
        
    current_time = pygame.time.get_ticks()
    if current_time - hit_point_start_time > HIT_POINT_TIME:
        return
        
    # Create a surface for the kill cam
    killcam_surface = pygame.Surface((KILLCAM_SIZE, KILLCAM_SIZE))
    killcam_surface.fill((0, 0, 0))  # Black background
    
    # Calculate the center of the target in the kill cam
    target_center_x = KILLCAM_SIZE // 2
    target_center_y = KILLCAM_SIZE // 2
    
    # Calculate the scaled target size
    scaled_target_size = int(TARGET_RADIUS * 4 * KILLCAM_SCALE)
    
    # Draw the target in the kill cam
    scaled_target = pygame.transform.scale(target_image, (scaled_target_size, scaled_target_size))
    target_x = target_center_x - scaled_target_size // 2
    target_y = target_center_y - scaled_target_size // 2
    killcam_surface.blit(scaled_target, (target_x, target_y))
    
    # Calculate the hit point relative to the target center
    hit_x = last_hit_point["x"] - last_hit_target_pos["x"]
    hit_y = last_hit_point["y"] - last_hit_target_pos["y"]
    
    # Scale the hit point coordinates
    scaled_hit_x = target_center_x + int(hit_x * KILLCAM_SCALE)
    scaled_hit_y = target_center_y + int(hit_y * KILLCAM_SCALE)
    
    # Draw a larger pulsing effect around the hit point
    pulse_size = 15 + int(8 * math.sin(current_time / 100))  # Larger pulsing size
    pygame.draw.circle(killcam_surface, (0, 255, 0, 128), (scaled_hit_x, scaled_hit_y), pulse_size)
    
    # Draw a second pulsing circle for more emphasis
    pulse_size2 = 8 + int(4 * math.sin(current_time / 150))  # Different frequency
    pygame.draw.circle(killcam_surface, (0, 255, 0, 200), (scaled_hit_x, scaled_hit_y), pulse_size2)
    
    # Draw a larger green dot at the hit point
    pygame.draw.circle(killcam_surface, (0, 255, 0), (scaled_hit_x, scaled_hit_y), 8)
    
    # Draw a thicker white outline around the hit point
    pygame.draw.circle(killcam_surface, (255, 255, 255), (scaled_hit_x, scaled_hit_y), 10, 2)
    
    # Draw a larger crosshair at the hit point
    size = 15
    thickness = 2
    pygame.draw.line(killcam_surface, (255, 255, 255), 
                    (scaled_hit_x - size, scaled_hit_y), 
                    (scaled_hit_x + size, scaled_hit_y), thickness)
    pygame.draw.line(killcam_surface, (255, 255, 255), 
                    (scaled_hit_x, scaled_hit_y - size), 
                    (scaled_hit_x, scaled_hit_y + size), thickness)
    
    # Draw a "HIT" text above the hit point with a dark background
    font = pygame.font.SysFont(None, 28, bold=True)  # Larger, bold font
    hit_text = font.render("HIT", True, (0, 255, 0))
    text_rect = hit_text.get_rect(center=(scaled_hit_x, scaled_hit_y - 25))
    
    # Draw a dark background behind the text
    padding = 5
    bg_rect = pygame.Rect(
        text_rect.left - padding,
        text_rect.top - padding,
        text_rect.width + padding * 2,
        text_rect.height + padding * 2
    )
    pygame.draw.rect(killcam_surface, (0, 0, 0), bg_rect)
    pygame.draw.rect(killcam_surface, (0, 255, 0), bg_rect, 1)  # Green border around text background
    
    killcam_surface.blit(hit_text, text_rect)
    
    # Draw the kill cam box in the top right corner
    box_x = GAME_WIDTH - KILLCAM_SIZE - 20
    box_y = 20
    
    # Draw a border around the kill cam
    pygame.draw.rect(screen, (0, 255, 0), (box_x - 2, box_y - 2, KILLCAM_SIZE + 4, KILLCAM_SIZE + 4), 2)
    
    # Draw the kill cam surface
    screen.blit(killcam_surface, (box_x, box_y))

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
            # Calculate position to center the target image
            target_x = delayed_pos["x"] - target_image.get_width() // 2
            target_y = delayed_pos["y"] - target_image.get_height() // 2
            screen.blit(target_image, (target_x, target_y))
        
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
        
        # Draw kill cam
        draw_killcam()
        
        # Draw sidebar
        draw_sidebar()
        
        pygame.display.flip()
        clock.tick(60)
    
    ws.close()
    pygame.quit()

if __name__ == "__main__":
    game_loop()