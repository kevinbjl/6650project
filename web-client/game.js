// Constants
const TARGET_RADIUS = 1.0;
const ARENA_SIZE = 50;
const WALL_HEIGHT = 10;
const BUILDING_SIZE = 8;
const VEHICLE_SIZE = 3;
const SIDEBAR_WIDTH = 200; // Width of the sidebar
const POSITION_HISTORY_SIZE = 100; // Store last 100 positions

// Three.js setup
const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(
  75,
  (window.innerWidth - SIDEBAR_WIDTH) / window.innerHeight,
  0.1,
  1000
);
const renderer = new THREE.WebGLRenderer({ antialias: true });
renderer.setSize(window.innerWidth - SIDEBAR_WIDTH, window.innerHeight);
renderer.shadowMap.enabled = true;
renderer.shadowMap.type = THREE.PCFSoftShadowMap;
document.body.insertBefore(renderer.domElement, document.body.firstChild);

// Add OrbitControls but disable most features
const controls = new THREE.OrbitControls(camera, renderer.domElement);
controls.enableDamping = false;
controls.enableZoom = false;
controls.enablePan = false;
controls.enableRotate = false;
controls.minDistance = 20;
controls.maxDistance = 20;
controls.maxPolarAngle = Math.PI / 2;

// Add skybox
const skyboxGeometry = new THREE.BoxGeometry(1000, 1000, 1000);
const skyboxMaterial = new THREE.MeshBasicMaterial({
  color: 0x87ceeb, // Sky blue
  side: THREE.BackSide,
});
const skybox = new THREE.Mesh(skyboxGeometry, skyboxMaterial);
scene.add(skybox);

// Lighting
const ambientLight = new THREE.AmbientLight(0x404040, 0.5);
scene.add(ambientLight);

const directionalLight = new THREE.DirectionalLight(0xffffff, 1);
directionalLight.position.set(1, 1, 1);
directionalLight.castShadow = true;
directionalLight.shadow.mapSize.width = 2048;
directionalLight.shadow.mapSize.height = 2048;
scene.add(directionalLight);

// Camera position
camera.position.set(0, 15, 20);
controls.target.set(0, 0, 0);
controls.update();

// Create arena
function createArena() {
  // Ground
  const groundGeometry = new THREE.PlaneGeometry(ARENA_SIZE, ARENA_SIZE);
  const groundMaterial = new THREE.MeshStandardMaterial({
    color: 0x2e8b57, // Sea green
    roughness: 0.8,
    metalness: 0.2,
  });
  const ground = new THREE.Mesh(groundGeometry, groundMaterial);
  ground.rotation.x = -Math.PI / 2;
  ground.receiveShadow = true;
  scene.add(ground);

  // Walls
  const wallMaterial = new THREE.MeshStandardMaterial({
    color: 0x8b4513, // Saddle brown
    roughness: 0.7,
    metalness: 0.1,
  });
  const wallGeometry = new THREE.BoxGeometry(ARENA_SIZE, WALL_HEIGHT, 1);

  // North wall
  const northWall = new THREE.Mesh(wallGeometry, wallMaterial);
  northWall.position.set(0, WALL_HEIGHT / 2, -ARENA_SIZE / 2);
  northWall.castShadow = true;
  northWall.receiveShadow = true;
  scene.add(northWall);

  // South wall
  const southWall = new THREE.Mesh(wallGeometry, wallMaterial);
  southWall.position.set(0, WALL_HEIGHT / 2, ARENA_SIZE / 2);
  southWall.castShadow = true;
  southWall.receiveShadow = true;
  scene.add(southWall);

  // East wall
  const eastWall = new THREE.Mesh(wallGeometry, wallMaterial);
  eastWall.rotation.y = Math.PI / 2;
  eastWall.position.set(ARENA_SIZE / 2, WALL_HEIGHT / 2, 0);
  eastWall.castShadow = true;
  eastWall.receiveShadow = true;
  scene.add(eastWall);

  // West wall
  const westWall = new THREE.Mesh(wallGeometry, wallMaterial);
  westWall.rotation.y = Math.PI / 2;
  westWall.position.set(-ARENA_SIZE / 2, WALL_HEIGHT / 2, 0);
  westWall.castShadow = true;
  westWall.receiveShadow = true;
  scene.add(westWall);

  // Buildings
  const buildingMaterial = new THREE.MeshStandardMaterial({
    color: 0x808080, // Gray
    roughness: 0.6,
    metalness: 0.3,
  });
  const buildingGeometry = new THREE.BoxGeometry(
    BUILDING_SIZE,
    BUILDING_SIZE,
    BUILDING_SIZE
  );

  // Add some buildings
  const buildings = [
    { x: -15, z: -15 },
    { x: 15, z: -15 },
    { x: -15, z: 15 },
    { x: 15, z: 15 },
  ];

  buildings.forEach((pos) => {
    const building = new THREE.Mesh(buildingGeometry, buildingMaterial);
    building.position.set(pos.x, BUILDING_SIZE / 2, pos.z);
    building.castShadow = true;
    building.receiveShadow = true;
    scene.add(building);
  });

  // Vehicles
  const vehicleMaterial = new THREE.MeshStandardMaterial({
    color: 0x444444,
    roughness: 0.5,
    metalness: 0.5,
  });
  const vehicleGeometry = new THREE.BoxGeometry(
    VEHICLE_SIZE,
    VEHICLE_SIZE / 2,
    VEHICLE_SIZE * 2
  );

  // Add some vehicles
  const vehicles = [
    { x: -10, z: 0, rotation: 0 },
    { x: 10, z: 0, rotation: Math.PI },
    { x: 0, z: -10, rotation: Math.PI / 2 },
    { x: 0, z: 10, rotation: -Math.PI / 2 },
  ];

  vehicles.forEach((pos) => {
    const vehicle = new THREE.Mesh(vehicleGeometry, vehicleMaterial);
    vehicle.position.set(pos.x, VEHICLE_SIZE / 4, pos.z);
    vehicle.rotation.y = pos.rotation;
    vehicle.castShadow = true;
    vehicle.receiveShadow = true;
    scene.add(vehicle);
  });
}

// Create target
function createTarget() {
  const geometry = new THREE.SphereGeometry(TARGET_RADIUS, 32, 32);
  const material = new THREE.MeshStandardMaterial({
    color: 0xff0000,
    roughness: 0.3,
    metalness: 0.7,
  });
  const target = new THREE.Mesh(geometry, material);
  target.castShadow = true;
  target.receiveShadow = true;
  return target;
}

// WebSocket setup
let ws;
let target;
let positionHistory = [];
let lastHitResult = null;
let hitMarkerTimeout = null;
let serverOffset = 0;
let measuredLatency = 0;
let simulatedLatency = 0;
let compensationEnabled = true;
let clientStartTime = Date.now();

function connectWebSocket() {
  console.log("Attempting to connect to WebSocket...");
  const wsUrl = "ws://";
  const host = "ec2-54-242-91-6.compute-1.amazonaws.com";
  // const host = window.location.hostname; // For localhost
  const port = "8080";
  ws = new WebSocket(`${wsUrl}${host}:${port}/ws`);

  ws.onopen = () => {
    console.log("WebSocket connection established");
    sendSync();
  };

  ws.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      console.log("WebSocket message received:", data);

      if (data.type === "position") {
        const posData = {
          x: data.position.x,
          y: data.position.y,
          z: data.position.z,
          timestamp: Date.now() - clientStartTime,
          serverTime: data.position.serverTime,
        };

        // Add the position to history with simulated latency
        setTimeout(() => {
          updateTargetPosition(posData);
        }, simulatedLatency);
      } else if (data.type === "hit_result") {
        console.log("Hit result received:", data);
        lastHitResult = data;
        showHitMarker(data.hit);
      } else if (data.type === "sync_response") {
        console.log("Sync response received:", data);
        const t0 = data.clientTime;
        const t1 = data.serverRecvTime;
        const t2 = data.serverSendTime;
        const t3 = Date.now() - clientStartTime;

        const rtt = t3 - t0;
        measuredLatency = Math.floor(rtt / 2);
        serverOffset = t1 - (t0 + measuredLatency);

        updateStats();
      }
    } catch (error) {
      console.error("Error processing WebSocket message:", error);
      console.error("Raw message:", event.data);
    }
  };

  ws.onerror = (error) => {
    console.error("WebSocket error:", error);
    console.error("Error details:", error.message);
  };

  ws.onclose = (event) => {
    console.log("WebSocket connection closed:", event.code, event.reason);
    console.log("Attempting to reconnect in 1 second...");
    setTimeout(connectWebSocket, 1000);
  };
}

function sendSync() {
  const syncData = {
    type: "sync",
    timestamp: Date.now() - clientStartTime,
  };
  ws.send(JSON.stringify(syncData));
}

function sendShoot(event) {
  // Only handle clicks in the game area (excluding sidebar)
  if (event.clientX > window.innerWidth - SIDEBAR_WIDTH) {
    return;
  }

  // Get mouse position in normalized device coordinates
  const mouse = new THREE.Vector2();
  mouse.x = (event.clientX / (window.innerWidth - SIDEBAR_WIDTH)) * 2 - 1;
  mouse.y = -(event.clientY / window.innerHeight) * 2 + 1;

  // Create raycaster
  const raycaster = new THREE.Raycaster();
  raycaster.setFromCamera(mouse, camera);

  // Get the point where the ray intersects with the target plane (y=1.0)
  const targetPlane = new THREE.Plane(new THREE.Vector3(0, 1, 0), -1.0);
  const intersection = new THREE.Vector3();
  raycaster.ray.intersectPlane(targetPlane, intersection);

  // Calculate the time when the shot was fired
  const currentTime = Date.now() - clientStartTime;
  const shotTime = currentTime - simulatedLatency;

  // Debug logging
  console.log("Shooting at time:", {
    currentTime,
    shotTime,
    simulatedLatency,
    compensationEnabled,
    serverOffset,
  });

  // Always send the shoot command with the intersection point
  const shootData = {
    type: "shoot",
    timestamp: shotTime, // Send the time when the shot was actually fired
    x: intersection.x,
    y: 1.0,
    z: intersection.z,
    offset: compensationEnabled ? serverOffset : 0,
    compensation_enabled: compensationEnabled,
  };
  ws.send(JSON.stringify(shootData));
}

function updateTargetPosition(posData) {
  if (target) {
    // Add position to history
    positionHistory.push({
      x: posData.x,
      y: posData.y,
      z: posData.z,
      timestamp: posData.timestamp,
      serverTime: posData.serverTime,
    });

    // Keep only the last POSITION_HISTORY_SIZE positions
    if (positionHistory.length > POSITION_HISTORY_SIZE) {
      positionHistory.shift();
    }

    // Update target position
    target.position.set(posData.x, posData.y, posData.z);

    // Debug logging
    console.log("Position update:", {
      newPos: posData,
      historySize: positionHistory.length,
      firstTime: positionHistory[0]?.timestamp,
      lastTime: positionHistory[positionHistory.length - 1]?.timestamp,
    });
  } else {
    console.error("Target not initialized");
  }
}

function showHitMarker(hit) {
  const hitMarker = document.getElementById("hit-marker");
  if (hit) {
    hitMarker.textContent = "HIT";
    hitMarker.style.color = "green";
  } else {
    hitMarker.textContent = "MISS";
    hitMarker.style.color = "red";
  }
  hitMarker.style.opacity = 1;

  if (hitMarkerTimeout) {
    clearTimeout(hitMarkerTimeout);
  }

  hitMarkerTimeout = setTimeout(() => {
    hitMarker.style.opacity = 0;
  }, 500);
}

function updateStats() {
  document.getElementById("measured-latency").textContent = measuredLatency;
  document.getElementById("server-offset").textContent = serverOffset;
}

// Event listeners
document.addEventListener("click", (event) => {
  if (
    event.target.id !== "latency-slider" &&
    event.target.id !== "compensation-checkbox"
  ) {
    // TODO: send a large amount of shoot requests with just one click
    sendShoot(event);
  }
});

document.getElementById("latency-slider").addEventListener("input", (event) => {
  simulatedLatency = parseInt(event.target.value);
  document.getElementById("latency-value").textContent = simulatedLatency;
  console.log("Simulated latency set to:", simulatedLatency, "ms");
});

document
  .getElementById("compensation-checkbox")
  .addEventListener("change", (event) => {
    compensationEnabled = event.target.checked;
    console.log(
      "Lag compensation",
      compensationEnabled ? "enabled" : "disabled"
    );
  });

// Handle window resize
window.addEventListener("resize", () => {
  camera.aspect = (window.innerWidth - SIDEBAR_WIDTH) / window.innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(window.innerWidth - SIDEBAR_WIDTH, window.innerHeight);
});

// Animation loop
function animate() {
  requestAnimationFrame(animate);
  controls.update(); // Update controls
  renderer.render(scene, camera);
}

// Initialize
createArena();
target = createTarget();
scene.add(target);
connectWebSocket();
animate();

// Update the renderer size to account for sidebar
function updateRendererSize() {
  const width = window.innerWidth - SIDEBAR_WIDTH;
  const height = window.innerHeight;
  renderer.setSize(width, height);
  camera.aspect = width / height;
  camera.updateProjectionMatrix();
}

// Initialize renderer with correct size
updateRendererSize();

// Update window resize handler
window.addEventListener("resize", updateRendererSize);
