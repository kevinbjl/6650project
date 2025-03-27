import pygame
import websocket
import json
import threading
import time

# Constants
WIDTH, HEIGHT = 800, 600
TARGET_RADIUS = 5
CROSSHAIR_SIZE = 15
MUZZLE_FLASH_TIME = 100  # Flash duration in milliseconds
HIT_MARKER_TIME = 500   # Hit marker display duration in milliseconds

# Initialize Pygame
pygame.init()
screen = pygame.display.set_mode((WIDTH, HEIGHT))
pygame.display.set_caption("FPS Shooter Game")
clock = pygame.time.Clock()

# Load Assets
flash_image = pygame.image.load("./assets/640px-Muzzle_flash_VFX.png")  # Muzzle flash
background = pygame.image.load("./assets/Screenshot-Doom.jpeg")  # Replace with your background
flash_image = pygame.transform.scale(flash_image, (50, 50))
background = pygame.transform.scale(background, (WIDTH, HEIGHT))

# WebSocket
client_start_time = int(time.time() * 1000)
target_position = None  # Single position instead of list
last_hit_result = None
hit_marker_start_time = 0

def on_error(ws, error):
    print(f"WebSocket Error: {error}")

def on_message(ws, message):
    global target_position, last_hit_result, hit_marker_start_time
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
    except Exception as e:
        print(f"Error processing message: {e}")

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
    global target_position, last_hit_result
    
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
    
    running = True
    muzzle_flash = False
    flash_start_time = 0
    
    while running:
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
        
        pygame.display.flip()
        clock.tick(60)
    
    ws.close()
    pygame.quit()

if __name__ == "__main__":
    game_loop()