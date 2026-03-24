```
let baseFreq = 0.05; 
let ratios = [1, 1.25, 1.5, 1.875, 2]; // C, E, G, B, C (5-bit)
let latchedBits = [0, 0, 0, 0, 0];
let lastClockVal = 0;

function setup() {
  createCanvas(800, 600);
}

function draw() {
  background(10);
  
  // 1. The Clock (Root Wave)
  let clockVal = sin(frameCount * baseFreq);
  
  // 2. Edge Trigger: Detect the Peak
  // If the wave was going up and now it's starting to go down, that's the peak!
  if (clockVal < lastClockVal && lastClockVal > 0.98) {
    // SAMPLE ALL BITS AT THIS INSTANT
    for (let i = 0; i < ratios.length; i++) {
      let bitVal = sin(frameCount * baseFreq * ratios[i]);
      latchedBits[i] = bitVal > 0 ? 1 : 0; 
    }
  }
  lastClockVal = clockVal;

  // 3. Visualization
  drawSystem(clockVal);
}

function drawSystem(clockVal) {
  let xStart = 150;
  let w = width - 200;

  // Draw the Bit Register
  for (let i = 0; i < ratios.length; i++) {
    let y = 50 + i * 80;
    
    // Draw the "live" wave in the background
    stroke(50);
    drawWave(xStart, y, w, baseFreq * ratios[i]);
    
    // Draw the "latched" state
    fill(latchedBits[i] ? color(0, 255, 100) : color(255, 50, 50));
    noStroke();
    ellipse(80, y, 40, 40);
    
    fill(255);
    textAlign(CENTER);
    text(latchedBits[i], 80, y + 7);
    textAlign(LEFT);
    text(`Bit ${i} (Ratio ${ratios[i]})`, xStart, y - 20);
  }

  // Draw the Clock visualization at the bottom
  fill(0, 200, 255);
  text("SYSTEM CLOCK (ROOT)", 50, height - 80);
  rect(50, height - 60, map(clockVal, -1, 1, 0, 200), 20);
  
  // Decimal Output
  let decimal = 0;
  for(let i=0; i<latchedBits.length; i++) decimal += latchedBits[i] * Math.pow(2, i);
  
  fill(255);
  textSize(30);
  text(`LATCHED VALUE: ${decimal}`, 350, height - 40);
}

function drawWave(x, y, w, f) {
  noFill();
  beginShape();
  for (let i = 0; i < w; i++) {
    vertex(x + i, y + sin((frameCount + i) * f) * 20);
  }
  endShape();
}

```

```
let baseFreq = 0.05;
let idealRatios = [1, 1.25, 1.5, 1.875, 2]; // Perfect Major 7th Chord
let latchedBits = [0, 0, 0, 0, 0];
let lastClockVal = 0;
let drift = 0; // The amount of "Noise" in the system

function setup() {
  createCanvas(800, 600);
  
  // Create a slider to control the "CPU Stability"
  driftSlider = createSlider(0, 0.05, 0, 0.001);
  driftSlider.position(550, 560);
}

function draw() {
  background(10);
  drift = driftSlider.value();

  // 1. The Clock (Root Wave)
  let clockVal = sin(frameCount * baseFreq);
  
  // 2. Sample at the Peak
  if (clockVal < lastClockVal && lastClockVal > 0.98) {
    for (let i = 0; i < idealRatios.length; i++) {
      // Add a tiny bit of "Noise" to the frequency calculation
      let noise = sin(frameCount * 0.01 + i) * drift;
      let noisyFreq = baseFreq * (idealRatios[i] + noise);
      
      let bitVal = sin(frameCount * noisyFreq);
      latchedBits[i] = bitVal > 0 ? 1 : 0; 
    }
  }
  lastClockVal = clockVal;

  drawInterface(clockVal);
}

function drawInterface(clockVal) {
  let xStart = 150;
  let w = width - 200;

  for (let i = 0; i < idealRatios.length; i++) {
    let y = 50 + i * 80;
    
    // Draw the "Ideal" frequency vs the "Actual" noisy signal
    stroke(80);
    drawWave(xStart, y, w, baseFreq * idealRatios[i]);
    
    // The "Latching" UI
    fill(latchedBits[i] ? color(0, 255, 100) : color(255, 50, 50));
    noStroke();
    ellipse(80, y, 40, 40);
    fill(255);
    textAlign(CENTER);
    text(latchedBits[i], 80, y + 7);
  }

  // Decimal Output
  let decimal = 0;
  for(let i=0; i<latchedBits.length; i++) decimal += latchedBits[i] * Math.pow(2, i);
  
  fill(255);
  textSize(30);
  textAlign(LEFT);
  text(`LATCHED VALUE: ${decimal}`, 50, height - 40);
  
  // Noise Label
  textSize(14);
  text(`CPU INSTABILITY (DRIFT): ${drift.toFixed(4)}`, 550, 550);
}

function drawWave(x, y, w, f) {
  noFill();
  beginShape();
  for (let i = 0; i < w; i++) {
    vertex(x + i, y + sin((frameCount + i) * f) * 15);
  }
  endShape();
}

```

```
let baseFreq = 0.05;
let idealRatios = [1, 1.25, 1.5, 1.875, 2]; // C, E, G, B, C
let latchedBits = [0, 0, 0, 0, 0];
let lastClockVal = 0;
let history = []; // To store the sequence of decimal values
let maxHistory = 600;

function setup() {
  createCanvas(800, 650);
  driftSlider = createSlider(0, 0.05, 0, 0.001);
  driftSlider.position(550, 610);
}

function draw() {
  background(10);
  let drift = driftSlider.value();

  // 1. SYSTEM CLOCK
  let clockVal = sin(frameCount * baseFreq);
  
  // 2. THE SAMPLING TRIGGER (LATCH)
  if (clockVal < lastClockVal && lastClockVal > 0.98) {
    let currentDecimal = 0;
    for (let i = 0; i < idealRatios.length; i++) {
      let noise = sin(frameCount * 0.01 + i) * drift;
      let noisyFreq = baseFreq * (idealRatios[i] + noise);
      
      let bitVal = sin(frameCount * noisyFreq);
      latchedBits[i] = bitVal > 0 ? 1 : 0;
      
      // Calculate Decimal Value
      currentDecimal += latchedBits[i] * Math.pow(2, i);
    }
    
    // Add to history
    history.push(currentDecimal);
    if (history.length > maxHistory) history.shift();
  }
  lastClockVal = clockVal;

  drawCPU(clockVal, drift);
  drawHistory(50, 480, maxHistory, 120);
}

function drawCPU(clockVal, drift) {
  let xStart = 150;
  for (let i = 0; i < idealRatios.length; i++) {
    let y = 40 + i * 70;
    
    // Bit indicators
    fill(latchedBits[i] ? color(0, 255, 100) : color(100, 20, 20));
    noStroke();
    ellipse(80, y, 30, 30);
    
    // Live wave visual
    stroke(70);
    noFill();
    beginShape();
    for (let x = 0; x < 150; x++) {
      vertex(xStart + x, y + sin((frameCount + x) * baseFreq * idealRatios[i]) * 15);
    }
    endShape();
  }
  
  fill(255);
  textSize(14);
  text(`CPU DRIFT: ${drift.toFixed(4)}`, 550, 600);
}

function drawHistory(x, y, w, h) {
  // Draw History Box
  stroke(100);
  noFill();
  rect(x, y, w, h);
  
  // Draw Data Stream
  noStroke();
  for (let i = 0; i < history.length; i++) {
    // Map the 5-bit decimal (0-31) to the height of the box
    let valY = map(history[i], 0, 31, y + h - 5, y + 5);
    
    // Color based on value
    fill(map(history[i], 0, 31, 100, 255), 150, 255);
    rect(x + i, valY, 2, 2);
    
    // Connect dots with lines to see the "wave" of data
    if (i > 0) {
      stroke(50, 100, 255, 100);
      let prevY = map(history[i-1], 0, 31, y + h - 5, y + 5);
      line(x + i - 1, prevY, x + i, valY);
    }
  }
  
  fill(200);
  noStroke();
  textSize(18);
  text("BINARY OUTPUT HISTORY (CPU REGISTERS OVER TIME)", x, y - 15);
}
```

```
let time = 0;
let baseFreq = 0.05; 
// 8-bit CPU: C, E, G, B, D, F#, A, B (A complex Lydian chord)
let ratios = [1, 1.25, 1.5, 1.875, 2.25, 2.812, 3.375, 3.937]; 
let latchedBits = new Array(8).fill(0);
let lastClockVal = 0;
let history = [];
let maxHistory = 700;
let speedSlider;

function setup() {
  createCanvas(800, 600);
  // Control the number of CPU cycles processed per frame
  speedSlider = createSlider(1, 500, 50, 1);
  speedSlider.position(500, height - 40);
}

function draw() {
  background(10);
  let cyclesPerFrame = speedSlider.value();

  // --- THE CORRECTED HIGH SPEED LOGIC LOOP ---
  for (let c = 0; c < cyclesPerFrame; c++) {
    time += 1; 
    let clockVal = sin(time * baseFreq);

    // Sampling Trigger: Detect the peak of the Root Wave
    if (clockVal < lastClockVal && lastClockVal > 0.98) {
      let currentDecimal = 0;
      for (let i = 0; i < ratios.length; i++) {
        let bitVal = sin(time * baseFreq * ratios[i]);
        latchedBits[i] = bitVal > 0 ? 1 : 0;
        // Shift bits into a single integer
        currentDecimal += latchedBits[i] * Math.pow(2, i);
      }
      
      history.push(currentDecimal);
      if (history.length > maxHistory) history.shift();
    }
    lastClockVal = clockVal;
  }

  drawInterface(cyclesPerFrame);
}

function drawInterface(speed) {
  // 1. Bit Status Indicators
  for (let i = 0; i < latchedBits.length; i++) {
    let xPos = 50 + i * 90;
    fill(latchedBits[i] ? color(0, 255, 150) : color(60, 10, 10));
    stroke(100);
    rect(xPos, 50, 80, 40, 5);
    
    fill(255);
    noStroke();
    textAlign(CENTER);
    text(`BIT ${i}`, xPos + 40, 75);
  }

  // 2. The Logic Analyzer (History)
  noFill();
  stroke(0, 200, 255, 200);
  strokeWeight(1.5);
  beginShape();
  for (let i = 0; i < history.length; i++) {
    // Map the 8-bit value (0-255) to the screen
    let vy = map(history[i], 0, 255, height - 100, 150);
    vertex(50 + i, vy);
  }
  endShape();

  // 3. Status Text
  strokeWeight(1);
  fill(255);
  textAlign(LEFT);
  textSize(18);
  let latestVal = history.length > 0 ? history[history.length-1] : 0;
  text(`DECIMAL: ${latestVal}`, 50, 140);
  text(`HEX: 0x${latestVal.toString(16).toUpperCase()}`, 200, 140);
  text(`BINARY: ${latestVal.toString(2).padStart(8, '0')}`, 350, 140);
  
  fill(150);
  text(`OVERCLOCK: ${speed}x Cycles/Frame`, 500, height - 50);
}
```

```
let time = 0;
let baseFreq = 0.05; 
let ratios = [];
let majorRatios = [1, 1.25, 1.5, 1.875, 2, 2.25, 2.5, 3]; // Organized
let dissonantRatios = [1, 1.059, 1.122, 1.189, 1.26, 1.335, 1.414, 1.498]; // Chaotic (Chromatic)
let latchedBits = new Array(8).fill(0);
let lastClockVal = 0;
let history = [];
let isDissonant = false;

function setup() {
  createCanvas(800, 600);
  ratios = majorRatios;
  
  let btn = createButton('Toggle Dissonance (Entropy)');
  btn.position(20, height - 40);
  btn.mousePressed(() => {
    isDissonant = !isDissonant;
    ratios = isDissonant ? dissonantRatios : majorRatios;
    history = []; // Clear history to see the new pattern
  });
}

function draw() {
  background(10);
  let cyclesPerFrame = 50; 

  for (let c = 0; c < cyclesPerFrame; c++) {
    time += 1; 
    let clockVal = sin(time * baseFreq);
    if (clockVal < lastClockVal && lastClockVal > 0.98) {
      let currentDecimal = 0;
      for (let i = 0; i < ratios.length; i++) {
        let bitVal = sin(time * baseFreq * ratios[i]);
        latchedBits[i] = bitVal > 0 ? 1 : 0;
        currentDecimal += latchedBits[i] * Math.pow(2, i);
      }
      history.push(currentDecimal);
      if (history.length > 700) history.shift();
    }
    lastClockVal = clockVal;
  }
  drawInterface();
}

function drawInterface() {
  // 1. Label
  fill(isDissonant ? color(255, 50, 50) : color(0, 255, 150));
  textSize(24);
  text(isDissonant ? "MODE: DISSONANT (HIGH ENTROPY)" : "MODE: HARMONIC (LOW ENTROPY)", 50, 35);

  // 2. The Logic Analyzer
  noFill();
  stroke(isDissonant ? color(255, 100, 100) : color(0, 255, 255));
  beginShape();
  for (let i = 0; i < history.length; i++) {
    let vy = map(history[i], 0, 255, height - 100, 150);
    vertex(50 + i, vy);
  }
  endShape();
  
  // 3. Status
  fill(255);
  textSize(16);
  text(`Current Decimal: ${history[history.length-1]}`, 50, 130);
}
```

```
let time = 0;
let baseFreq = 0.05; 
let majorRatios = [1, 1.25, 1.5, 1.875, 2, 2.25, 2.5, 3]; 
let dissonantRatios = [1, 1.059, 1.122, 1.189, 1.26, 1.335, 1.414, 1.498];
let ratios;
let latchedBits = new Array(8).fill(0);
let lastClockVal = 0;
let history = []; 
let isDissonant = false;

function setup() {
  createCanvas(800, 700, WEBGL); // 3D Mode
  ratios = majorRatios;
  
  let btn = createButton('Toggle Dissonance');
  btn.position(20, 20);
  btn.mousePressed(() => {
    isDissonant = !isDissonant;
    ratios = isDissonant ? dissonantRatios : majorRatios;
    history = [];
  });
}

function draw() {
  background(10);
  orbitControl(); // Drag mouse to rotate the 3D view
  
  // --- HIGH SPEED LOGIC LOOP ---
  let cyclesPerFrame = 50; 
  for (let c = 0; c < cyclesPerFrame; c++) {
    time += 1; 
    let clockVal = sin(time * baseFreq);
    if (clockVal < lastClockVal && lastClockVal > 0.98) {
      let bits = [];
      let currentVal = 0;
      for (let i = 0; i < ratios.length; i++) {
        let b = sin(time * baseFreq * ratios[i]) > 0 ? 1 : 0;
        bits.push(b);
        currentVal += b * Math.pow(2, i);
      }
      history.push({val: currentVal, bits: bits});
      if (history.length > 200) history.shift();
    }
    lastClockVal = clockVal;
  }

  // --- 3D VISUALIZATION ---
  push();
  rotateX(PI/4); // Tilt for perspective
  translate(-350, 0, 0);

  // 1. Draw the 3D History Ribbon
  noFill();
  strokeWeight(2);
  beginShape();
  for (let i = 0; i < history.length; i++) {
    let h = map(history[i].val, 0, 255, 0, -200);
    let z = i * -5; // Recede into distance
    let col = isDissonant ? color(255, 50, i) : color(i, 200, 255);
    stroke(col);
    vertex(i * 4, h, z);
  }
  endShape();
  pop();

  // 2. Draw the 8x8 Bit Pixel Grid (Lower Right)
  push();
  translate(200, 200, 0);
  drawBitGrid();
  pop();
}

function drawBitGrid() {
  let cellSize = 15;
  // We use the last 8 history states to fill an 8x8 grid
  let startIdx = max(0, history.length - 8);
  for (let y = 0; y < 8; y++) {
    if (history[startIdx + y]) {
      let rowBits = history[startIdx + y].bits;
      for (let x = 0; x < 8; x++) {
        fill(rowBits[x] ? 255 : 30);
        stroke(0);
        rect(x * cellSize, y * cellSize, cellSize, cellSize);
      }
    }
  }
  fill(255);
  textSize(12);
  text("BIT GRID (CHORD TEXTURE)", 0, -10);
}
```

```
let time = 0;
let baseFreq = 0.05; 
let majorRatios = [1, 1.25, 1.5, 1.875, 2, 2.25, 2.5, 3]; 
let dissonantRatios = [1, 1.059, 1.122, 1.189, 1.26, 1.335, 1.414, 1.498];
let ratios;
let latchedBits = new Array(8).fill(0);
let lastClockVal = 0;
let history = []; 
let isDissonant = false;

// FEEDBACK VARIABLES
let feedbackAmount = 0; // Controlled by slider
let lastDecimal = 0;    // The "Memory" of the CPU
let currentPhaseOffset = 0;

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  ratios = majorRatios;
  
  // UI
  let btn = createButton('Toggle Dissonance');
  btn.position(20, 20);
  btn.mousePressed(() => { isDissonant = !isDissonant; ratios = isDissonant ? dissonantRatios : majorRatios; history = []; });

  createP('FEEDBACK INTENSITY (CHAOS)').position(20, 50).style('color', 'white');
  feedbackSlider = createSlider(0, 0.5, 0, 0.001); // Subtle feedback creates complex patterns
  feedbackSlider.position(20, 80);
}

function draw() {
  background(10);
  orbitControl(); 
  
  let cyclesPerFrame = 40; 
  feedbackAmount = feedbackSlider.value();

  for (let c = 0; c < cyclesPerFrame; c++) {
    time += 1; 
    let clockVal = sin(time * baseFreq);
    
    if (clockVal < lastClockVal && lastClockVal > 0.98) {
      let bits = [];
      let currentVal = 0;
      
      // THE FEEDBACK LOOP: 
      // The previous decimal value (0-255) influences the phase of the current sample
      currentPhaseOffset = (lastDecimal / 255) * feedbackAmount * PI;

      for (let i = 0; i < ratios.length; i++) {
        // Higher bits get more "nudged" by the feedback
        let b = sin(time * baseFreq * ratios[i] + (currentPhaseOffset * i)) > 0 ? 1 : 0;
        bits.push(b);
        currentVal += b * Math.pow(2, i);
      }
      
      lastDecimal = currentVal; // Store for next cycle
      history.push({val: currentVal, bits: bits});
      if (history.length > 300) history.shift();
    }
    lastClockVal = clockVal;
  }

  render3DScene();
}

function render3DScene() {
  rotateX(PI/3);
  rotateZ(frameCount * 0.005); // Slow rotation to see the 3D structure

  // 1. Draw the "Feedback Tendrils"
  noFill();
  strokeWeight(1);
  for (let j = 0; j < 8; j++) {
    beginShape();
    // Color shifts based on the feedback intensity
    let r = map(lastDecimal, 0, 255, 100, 255);
    let g = map(feedbackAmount, 0, 0.5, 200, 50);
    stroke(r, g, 255, 150);
    
    for (let i = 0; i < history.length; i++) {
      let bitOn = history[i].bits[j];
      let x = (j - 4) * 50;
      let y = bitOn ? -30 : 30;
      let z = i * -8;
      vertex(x, y, z);
    }
    endShape();
  }

  // 2. The Data "Core" (Pixel Grid)
  push();
  translate(0, 0, -history.length * 4);
  drawDataCore();
  pop();
}

function drawDataCore() {
  let cellSize = 15;
  let startIdx = max(0, history.length - 16);
  for (let y = 0; y < 16; y++) {
    if (history[startIdx + y]) {
      let rowBits = history[startIdx + y].bits;
      for (let x = 0; x < 8; x++) {
        fill(rowBits[x] ? color(0, 255, 255) : 20);
        stroke(0, 50);
        rect((x-4) * cellSize, (y-8) * cellSize, cellSize, cellSize);
      }
    }
  }
}
```

```
let tokens = [];
let bedrockZ = 0;
let workbenchZ = 1.0;
let time = 0;
let hypothesis = null; // The "z=1" workbench occupant

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  // Initialize tokens from "Raw Bytes"
  for (let i = 0; i < 20; i++) {
    let byte_val = floor(random(255));
    let seq_idx = i;
    tokens.push(new Token(byte_val, seq_idx));
  }
}

function draw() {
  background(5);
  orbitControl();
  time += 0.05;

  // Global "Collapse" Force (The Gravity of the Bedrock)
  let gravity = 0.005;

  // Sort tokens to find the current dominant hypothesis
  tokens.sort((a, b) => b.z - a.z);
  hypothesis = tokens[0];

  for (let t of tokens) {
    t.update(gravity, tokens);
    t.display();
  }
  
  drawWorkbench();
}

class Token {
  constructor(v, s) {
    this.id = v << 24 | s;
    this.byte = v;
    this.seq = s;
    this.freq = map(v, 0, 255, 0.01, 0.5); // Freq = Energy
    this.phase = random(TWO_PI);
    this.z = 0.1; // Starting near bedrock
    this.modeLevel = 0;
  }

  update(g, others) {
    // 1. Bedrock Pull (Natural Decay)
    this.z -= g;
    
    // 2. Harmonic Resonance Check (The "Effort")
    for (let other of others) {
      if (other === this) continue;
      
      // Calculate Ratio
      let ratio = max(this.freq, other.freq) / min(this.freq, other.freq);
      let stability = 1.0 - (ratio % 1.0); // How close to a whole number?

      if (stability > 0.95) { // Found a Stable Ratio!
        let lift = 0.02 * (1.0 / ratio); // Effort scaling: higher ratios are harder
        this.z += lift;
        
        // 3. Phase Alignment (Collapse Mechanic)
        let diff = other.phase - this.phase;
        this.phase += diff * 0.05; 
      }
    }

    // Top-Down Feedback (Projecting energy from hypothesis)
    if (hypothesis && this !== hypothesis) {
      if (abs(this.freq - hypothesis.freq * 0.5) < 0.01) {
        this.z += 0.01; // Support from the workbench
      }
    }

    this.phase += this.freq;
    this.z = constrain(this.z, 0, 1.2);
  }

  display() {
    push();
    // X = Byte Val, Y = Seq Idx, Z = Resonance Height
    let tx = map(this.byte, 0, 255, -300, 300);
    let ty = map(this.seq, 0, 20, -300, 300);
    let tz = map(this.z, 0, 1, -200, 200);
    
    translate(tx, ty, tz);
    
    // Oscillating Visual
    let pulse = sin(this.phase) * 10;
    fill(map(this.z, 0, 1, 50, 255), pulse + 150, 255);
    noStroke();
    sphere(5 + pulse/2);
    
    // Connection lines to resonant partners
    pop();
  }
}

function drawWorkbench() {
  push();
  noFill();
  stroke(255, 255, 0, 50);
  translate(0, 0, 200); // The z=1 plane
  plane(600, 600);
  pop();
}
```

```
let tokens = [];
let modes = []; // Level 1+: Super-modes, Hyper-modes
let workbenchZ = 300; // Visual Z-plane for the workbench
let time = 0;

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  // Initialize Level 0 Tokens (Raw Bytes)
  for (let i = 0; i < 15; i++) {
    tokens.push(new Oscillator(floor(random(255)), i, 0));
  }
}

function draw() {
  background(5);
  orbitControl();
  time += 0.02;

  // 1. Update Level 0 Tokens
  for (let t of tokens) {
    t.update(tokens, modes); 
    t.display();
  }

  // 2. Update and Display Higher-Tier Modes
  for (let i = modes.length - 1; i >= 0; i--) {
    modes[i].update(modes, []); // Modes also interact with each other
    modes[i].display();
    
    // Decay empty modes
    if (modes[i].energy < 0.1) modes.splice(i, 1);
  }

  // 3. THE COMPOSITION MECHANIC (The Workbench)
  // Check for clusters at z=1 to promote to a Super-Mode
  checkForPromotion();

  drawPlanes();
}

class Oscillator {
  constructor(byte_val, seq, level, children = []) {
    this.byte = byte_val;
    this.seq = seq;
    this.level = level; // 0 = Token, 1 = Super-Mode, 2 = Hyper-Mode
    this.children = children; // References to lower-level oscillators
    
    this.freq = map(byte_val, 0, 255, 0.02, 0.2) * (level + 1);
    this.phase = random(TWO_PI);
    this.z = 0; // Bedrock
    this.energy = 0.5;
  }

  update(peers, parents) {
    // A. The Bedrock Ramp (Gravity)
    this.z -= 0.005;

    // B. Resonance with Peers (Bottom-Up Support)
    for (let p of peers) {
      if (p === this) continue;
      let ratio = max(this.freq, p.freq) / min(this.freq, p.freq);
      let diff = ratio % 1.0;
      
      if (diff < 0.02 || diff > 0.98) { // Stable Ratio!
        let cost = 1.0 / ratio; // Rule #3: High ratios take more effort
        this.z += 0.015 * cost;
        // Phase Alignment (The "Collapse")
        this.phase += (p.phase - this.phase) * 0.02;
      }
    }

    // C. Top-Down Feedback (Rule #5)
    for (let m of modes) {
      if (m.children.includes(this)) {
        this.z += 0.02; // Super-mode "holds up" its children
        this.phase += (m.phase - this.phase) * 0.1; // Frequency locking
      }
    }

    this.phase += this.freq;
    this.z = constrain(this.z, 0, 1.2);
  }

  display() {
    push();
    let tx = map(this.byte, 0, 255, -400, 400);
    let ty = map(this.seq, 0, 15, -400, 400);
    let tz = map(this.z, 0, 1, -200, workbenchZ);
    
    translate(tx, ty, tz);
    
    // Visuals evolve by level
    let sz = 10 + this.level * 15;
    let pulse = sin(this.phase) * (5 + this.level * 5);
    
    if (this.level == 0) fill(0, 150, 255, 200);
    else if (this.level == 1) fill(255, 200, 0);
    else fill(255, 0, 255);
    
    noStroke();
    if (this.level > 0) sphere(sz + pulse);
    else box(sz + pulse);
    
    pop();
  }
}

function checkForPromotion() {
  // Find tokens near the workbench (z > 0.9)
  let candidates = tokens.filter(t => t.z > 0.9);
  
  if (candidates.length >= 3) {
    // Average their frequency to create a new "Master Oscillator"
    let avgByte = candidates.reduce((sum, t) => sum + t.byte, 0) / candidates.length;
    let newMode = new Oscillator(avgByte, modes.length, 1, [...candidates]);
    
    // Only one "Dominant Hypothesis" at a time (Rule #7)
    let alreadyExists = modes.some(m => abs(m.byte - avgByte) < 5);
    if (!alreadyExists) {
      modes.push(newMode);
      console.log("New Super-Mode Formed!");
    }
  }
}

function drawPlanes() {
  // Bedrock
  push(); translate(0,0,-200); noFill(); stroke(100); plane(1000); pop();
  // Workbench (z=1)
  push(); translate(0,0,workbenchZ); noFill(); stroke(255, 255, 0, 150); plane(1000); pop();
}
```

```
let oscillators = [];
let superModes = [];
let noiseIntensity = 0;
let workbenchOccupant = null;

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  // Initial Level 0 "Evidence" Tokens
  for (let i = 0; i < 20; i++) {
    oscillators.push(new Unit(floor(random(255)), i, 0));
  }
  
  let btn = createButton('TRIGGER NOISE BURST');
  btn.position(20, 20);
  btn.mousePressed(() => { noiseIntensity = 1.0; });
}

function draw() {
  background(10);
  orbitControl();
  
  // Decay noise over time
  noiseIntensity *= 0.95;

  // 1. Process Level 0 Evidence
  for (let u of oscillators) {
    // Inject Noise: Randomly jiggles phase and drops Z
    if (random() < noiseIntensity) {
      u.phase += random(-PI, PI);
      u.z -= 0.1;
    }
    u.update(oscillators, superModes);
    u.display();
  }

  // 2. Process Super-Modes (The "Inference" Layer)
  for (let i = superModes.length - 1; i >= 0; i--) {
    superModes[i].update(superModes, []);
    superModes[i].display();
    // Selection: If a Super-Mode loses too much "Support" from below, it collapses
    if (superModes[i].z < 0.5) superModes.splice(i, 1);
  }

  // 3. THE WORKBENCH SELECTION (Rule #7)
  // Only the most coherent group becomes the "Hypothesis"
  performInference();

  drawSubstrateLevels();
}

class Unit {
  constructor(byte_val, id, level, children = []) {
    this.byte = byte_val;
    this.id = id;
    this.level = level;
    this.children = children;
    this.freq = map(byte_val, 0, 255, 0.02, 0.2);
    this.phase = random(TWO_PI);
    this.z = 0.1;
    this.coherence = 0; // Strength of the inference
  }

  update(peers, modes) {
    // Bedrock Ramp (Rule #6)
    this.z -= 0.005;

    // Phase Alignment (Rule #1)
    for (let p of peers) {
      if (p === this) continue;
      let ratio = max(this.freq, p.freq) / min(this.freq, p.freq);
      if (ratio % 1.0 < 0.05 || ratio % 1.0 > 0.95) {
        let effort = 1.0 / ratio; // Rule #3
        this.z += 0.012 * effort;
        this.phase += (p.phase - this.phase) * 0.01;
      }
    }

    // Top-Down Feedback (Rule #5)
    for (let m of modes) {
      if (m.children.includes(this)) {
        // This is the "Inference Support": 
        // The Super-Mode projects energy down to counteract the Bedrock Pull
        this.z += 0.02; 
        this.phase += (m.phase - this.phase) * 0.05;
      }
    }

    this.phase += this.freq;
    this.z = constrain(this.z, 0, 1.2);
  }

  display() {
    push();
    let x = map(this.byte, 0, 255, -400, 400);
    let y = map(this.id, 0, 20, -300, 300);
    let z_pos = map(this.z, 0, 1, -200, 200);
    translate(x, y, z_pos);
    
    let alpha = map(this.z, 0, 1, 50, 255);
    let sz = this.level === 0 ? 8 : 25;
    
    if (this.level === 0) fill(0, 200, 255, alpha);
    else fill(255, 255, 0, alpha); // Super-Modes are Gold
    
    noStroke();
    box(sz + sin(this.phase) * 5);
    pop();
  }
}

function performInference() {
  // Find the oscillator closest to z=1 (The Workbench)
  let best = oscillators.concat(superModes).sort((a, b) => b.z - a.z)[0];
  
  if (best && best.z > 0.85) {
    workbenchOccupant = best;
    // Selection Mechanic: If this is a Level 0, try to "Promote" it to a Super-Mode
    if (best.level === 0) {
      let resonantGroup = oscillators.filter(o => 
        abs((max(o.freq, best.freq)/min(o.freq, best.freq)) % 1.0) < 0.02
      );
      
      if (resonantGroup.length > 4) {
        let newSM = new Unit(best.byte, superModes.length, 1, resonantGroup);
        newSM.z = 1.0; 
        superModes.push(newSM);
      }
    }
  }
}

function drawSubstrateLevels() {
  push();
  noFill();
  stroke(255, 50);
  translate(0, 0, -200); plane(800, 600); // Bedrock
  translate(0, 0, 400); stroke(255, 255, 0, 100); plane(800, 600); // Workbench
  pop();
}
```

```
let oscillators = [];
let bedrockMemory = []; // The "Solid" particles
let time = 0;
let workbenchOccupant = null;

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  for (let i = 0; i < 25; i++) {
    oscillators.push(new Particle(floor(random(255)), i));
  }
}

function draw() {
  background(5);
  orbitControl();
  time += 0.02;

  // 1. Sort to find the workbench leader (Rule #7)
  oscillators.sort((a, b) => b.z - a.z);
  workbenchOccupant = oscillators[0];

  // 2. Process active oscillators
  for (let i = oscillators.length - 1; i >= 0; i--) {
    let p = oscillators[i];
    p.update(oscillators, bedrockMemory);
    p.display();

    // ANNEALMENT MECHANIC (Rule #8)
    // If a particle stays at the workbench (z=1) and reaches max energy, it "freezes"
    if (p.z > 0.98 && p.energy > 0.99) {
      p.state = "ANNEALED";
      bedrockMemory.push(p);
      oscillators.splice(i, 1);
      // Spawn a new "Raw Byte" to keep the substrate moving
      oscillators.push(new Particle(floor(random(255)), oscillators.length));
    }
  }

  // 3. Process Bedrock Memory (Static Reference)
  for (let m of bedrockMemory) {
    m.z = -0.9; // Locked at the bottom
    m.display();
  }

  drawPlanes();
}

class Particle {
  constructor(byte_val, id) {
    this.byte = byte_val;
    this.id = id;
    this.freq = map(byte_val, 0, 255, 0.02, 0.15);
    this.phase = random(TWO_PI);
    this.z = -0.5;
    this.energy = 0.2;
    this.state = "NOISE"; // NOISE, WORKBENCH, ANNEALED
  }

  update(others, memory) {
    if (this.state === "ANNEALED") return;

    // A. Bedrock Gravity (The "Ramp")
    this.z -= 0.004;

    // B. Bottom-Up Resonance (The "Effort")
    for (let o of others) {
      if (o === this) continue;
      let ratio = max(this.freq, o.freq) / min(this.freq, o.freq);
      let stability = 1.0 - (ratio % 1.0);

      if (stability > 0.97) {
        let lift = (0.015 / ratio); // Harder for high ratios
        this.z += lift;
        this.energy += 0.005;
        this.phase += (o.phase - this.phase) * 0.05; // Phase Collapse
      }
    }

    // C. Bedrock Memory Influence (Top-Down from Bedrock)
    for (let m of memory) {
      if (abs(this.freq - m.freq) < 0.001) {
        this.z += 0.01; // Resonating with "History" helps you climb
      }
    }

    this.z = constrain(this.z, -1, 1);
    this.energy = constrain(this.energy, 0, 1);
    this.phase += this.freq;

    // Update State based on Z-position
    if (this.z > 0.8) this.state = "WORKBENCH";
    else if (this.z < -0.8) this.state = "NOISE";
    else this.state = "TRANSITION";
  }

  display() {
    push();
    let tx = map(this.byte, 0, 255, -400, 400);
    let ty = map(this.id, 0, 25, -300, 300);
    let tz = this.z * 250;
    translate(tx, ty, tz);

    // --- COLOR CODING BY STATE ---
    if (this.state === "ANNEALED") {
      fill(255, 50, 50); // RED: Stable Bedrock Memory
      stroke(255, 100);
    } else if (this.state === "WORKBENCH") {
      fill(0, 255, 150); // NEON GREEN: The Hypothesis
      stroke(255, 255, 0); // Yellow highlight
    } else {
      fill(100, 100, 250, 150); // BLUE/PURPLE: Noise Wall
      noStroke();
    }

    let sz = (this.state === "WORKBENCH") ? 15 : 8;
    let pulse = sin(this.phase) * 4;
    
    if (this.state === "ANNEALED") box(10); // Solid
    else sphere(sz + pulse); // Oscillating
    
    pop();
  }
}

function drawPlanes() {
  // Visual markers for the different levels
  push(); noFill(); stroke(255, 50); translate(0,0,-220); plane(900); pop(); // Noise Wall
  push(); noFill(); stroke(0, 255, 150, 100); translate(0,0,220); plane(900); pop(); // Workbench
}
```

```
let oscillators = [];
let bedrockMemory = []; 
let workbenchOccupant = null;

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  // Populate initial substrate
  for (let i = 0; i < 30; i++) {
    oscillators.push(new Particle(floor(random(255)), i));
  }
}

function draw() {
  background(5);
  orbitControl();

  // 1. Logic: Find the dominant signal (The Hypothesis)
  oscillators.sort((a, b) => b.z - a.z);
  workbenchOccupant = oscillators[0];

  // 2. Update Active Substrate
  for (let i = oscillators.length - 1; i >= 0; i--) {
    let p = oscillators[i];
    p.update(oscillators, bedrockMemory);
    p.display();

    // ANNEALING: If it hits the Workbench and saturates energy, it "Freezes"
    if (p.z > 0.95 && p.energy > 0.98) {
      p.state = "ANNEALED";
      bedrockMemory.push(p);
      oscillators.splice(i, 1);
      // Inject new entropy into the system
      oscillators.push(new Particle(floor(random(255)), oscillators.length));
    }
  }

  // 3. Display the "Solid" Foundation
  for (let m of bedrockMemory) {
    m.display();
  }

  drawBoundaryPlanes();
}

class Particle {
  constructor(byte_val, id) {
    this.byte = byte_val;
    this.id = id;
    this.freq = map(byte_val, 0, 255, 0.02, 0.15);
    this.phase = random(TWO_PI);
    this.z = -0.8; // Start near the noise wall
    this.energy = 0.1;
    this.state = "NOISE";
  }

  update(peers, memory) {
    if (this.state === "ANNEALED") return;

    // A. Bedrock Gravity (The "Ramp" - Rule #6)
    this.z -= 0.006;

    // B. Peer-to-Peer Resonance (Horizontal Interaction)
    for (let p of peers) {
      if (p === this) continue;
      let ratio = max(this.freq, p.freq) / min(this.freq, p.freq);
      if (ratio % 1.0 < 0.03 || ratio % 1.0 > 0.97) {
        this.z += (0.015 / ratio); // Effort/Energy mechanic
        this.energy += 0.004;
        this.phase += (p.phase - this.phase) * 0.05; // Phase Collapse
      }
    }

    // C. HARMONIC BEDROCK SUPPORT (The "Inference" - Rule #5)
    // If we resonate with the "History", the bedrock pulls us UP
    for (let m of memory) {
      let ratio = max(this.freq, m.freq) / min(this.freq, m.freq);
      let isHarmonic = (ratio % 1.0 < 0.01 || ratio % 1.0 > 0.99);
      
      if (isHarmonic) {
        this.z += 0.02; // Bedrock provides a "Tuned Boost"
        this.energy += 0.01;
        // Visual link: tethering to memory
        if (this.z > 0) drawTether(this, m);
      }
    }

    this.z = constrain(this.z, -1, 1);
    this.energy = constrain(this.energy, 0, 1);
    this.phase += this.freq;

    // State Transitions
    if (this.z > 0.8) this.state = "WORKBENCH";
    else this.state = "NOISE";
  }

  display() {
    push();
    let tx = map(this.byte, 0, 255, -400, 400);
    let ty = map(this.id, 0, 30, -300, 300);
    let tz = this.z * 250;
    
    // Annealed blocks are physically at the "Bedrock" floor (-250)
    if (this.state === "ANNEALED") tz = -250; 
    
    translate(tx, ty, tz);

    if (this.state === "ANNEALED") {
      fill(255, 50, 50); // RED: Bedrock
      box(12);
    } else if (this.state === "WORKBENCH") {
      fill(0, 255, 150); // GREEN: Workbench
      sphere(15 + sin(this.phase) * 5);
    } else {
      fill(100, 100, 255, 150); // BLUE: Noise
      sphere(8 + sin(this.phase) * 3);
    }
    pop();
  }
}

function drawTether(p, m) {
  push();
  stroke(255, 255, 0, 50);
  let x1 = map(p.byte, 0, 255, -400, 400);
  let y1 = map(p.id, 0, 30, -300, 300);
  let z1 = p.z * 250;
  let x2 = map(m.byte, 0, 255, -400, 400);
  let y2 = map(m.id, 0, 30, -300, 300);
  let z2 = -250;
  line(x1, y1, z1, x2, y2, z2);
  pop();
}

function drawBoundaryPlanes() {
  push(); noFill(); stroke(100); translate(0,0,-250); plane(900); pop(); // BEDROCK
  push(); noFill(); stroke(0, 255, 150, 100); translate(0,0,250); plane(900); pop(); // WORKBENCH
}
```

```
let oscillators = [];
let bedrockMemory = []; 
let maxBedrock = 40; // Prevents "Heat Death" by recycling memory
let workbenchZ = 250; 
let bedrockZ = -250;

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  // Initial 30 tokens from "Raw Bytes"
  for (let i = 0; i < 30; i++) {
    oscillators.push(new Particle(floor(random(255)), i));
  }
}

function draw() {
  background(5);
  orbitControl();

  // 1. RECYCLE MECHANIC: Prevents runaway Red Block buildup
  if (bedrockMemory.length > maxBedrock) {
    bedrockMemory.shift(); // Forget the oldest memory
  }

  // 2. CONSOLIDATION: 3 Bedrock particles -> 1 Gold Super-Token
  if (bedrockMemory.length > 5) {
    checkForSuperToken();
  }

  // 3. UPDATE ACTIVE SUBSTRATE
  oscillators.sort((a, b) => b.z - a.z); // Rank by Resonance for selection
  for (let i = oscillators.length - 1; i >= 0; i--) {
    let p = oscillators[i];
    p.update(oscillators, bedrockMemory);
    p.display();

    // ANNEALING: Particle becomes Bedrock if it holds the Workbench
    if (p.z > 0.95 && p.energy > 0.98) {
      p.state = "ANNEALED";
      bedrockMemory.push(p);
      oscillators.splice(i, 1);
      oscillators.push(new Particle(floor(random(255)), oscillators.length));
    }
  }

  // 4. DISPLAY BEDROCK
  for (let m of bedrockMemory) {
    m.display();
  }

  drawPlanes();
}

class Particle {
  constructor(byte_val, id, isSuper = false) {
    this.byte = byte_val;
    this.id = id;
    this.isSuper = isSuper;
    this.freq = map(byte_val, 0, 255, 0.02, 0.15);
    this.phase = random(TWO_PI);
    this.z = -0.8; 
    this.energy = 0.1;
    this.state = "NOISE";
  }

  update(peers, memory) {
    if (this.state === "ANNEALED") return;

    // A. Bedrock Gravity (The Ramp)
    this.z -= 0.007;

    // B. Peer Resonance (Phase Collapse)
    for (let p of peers) {
      if (p === this) continue;
      let ratio = max(this.freq, p.freq) / min(this.freq, p.freq);
      if (ratio % 1.0 < 0.03 || ratio % 1.0 > 0.97) {
        this.z += (0.018 / ratio); // Effort Scaling
        this.energy += 0.005;
        this.phase += (p.phase - this.phase) * 0.05;
      }
    }

    // C. ATTRACTOR LOGIC: Support from Bedrock & Super-Tokens
    for (let m of memory) {
      let ratio = max(this.freq, m.freq) / min(this.freq, m.freq);
      let isHarmonic = (ratio % 1.0 < 0.01 || ratio % 1.0 > 0.99);
      
      if (isHarmonic) {
        let boost = m.isSuper ? 0.04 : 0.02; // Super-Tokens pull harder
        this.z += boost;
        this.energy += 0.01;
        if (this.z > 0) drawTether(this, m);
      }
    }

    this.z = constrain(this.z, -1, 1);
    this.energy = constrain(this.energy, 0, 1);
    this.phase += this.freq;
    this.state = (this.z > 0.8) ? "WORKBENCH" : "NOISE";
  }

  display() {
    push();
    let tx = map(this.byte, 0, 255, -400, 400);
    let ty = map(this.id, 0, 30, -300, 300);
    let tz = (this.state === "ANNEALED") ? bedrockZ : this.z * workbenchZ;
    translate(tx, ty, tz);

    if (this.isSuper) {
      fill(255, 215, 0); // GOLD: Super-Token
      sphere(20); 
    } else if (this.state === "ANNEALED") {
      fill(255, 50, 50); // RED: Bedrock Memory
      box(12);
    } else if (this.state === "WORKBENCH") {
      fill(0, 255, 150); // GREEN: Workbench Occupant
      sphere(14 + sin(this.phase)*4);
    } else {
      fill(100, 100, 255, 150); // BLUE: Noise/Entropy
      sphere(7);
    }
    pop();
  }
}

function checkForSuperToken() {
  // Check oldest 3 memories for a stable triad
  let g = bedrockMemory.slice(0, 3);
  let r1 = (max(g[0].freq, g[1].freq)/min(g[0].freq, g[1].freq)) % 1.0;
  let r2 = (max(g[1].freq, g[2].freq)/min(g[1].freq, g[2].freq)) % 1.0;

  if (r1 < 0.05 && r2 < 0.05) {
    let avgByte = floor((g[0].byte + g[1].byte + g[2].byte) / 3);
    bedrockMemory.splice(0, 3); // Consume 3 particles
    let superT = new Particle(avgByte, 999, true);
    superT.state = "ANNEALED";
    bedrockMemory.push(superT); // Create 1 Super-Token
  }
}

function drawTether(p, m) {
  push();
  stroke(m.isSuper ? color(255, 215, 0, 80) : color(255, 255, 255, 30));
  let x1 = map(p.byte, 0, 255, -400, 400);
  let y1 = map(p.id, 0, 30, -300, 300);
  let z1 = p.z * workbenchZ;
  let x2 = map(m.byte, 0, 255, -400, 400);
  let y2 = map(m.id, 0, 30, -300, 300);
  let z2 = bedrockZ;
  line(x1, y1, z1, x2, y2, z2);
  pop();
}

function drawPlanes() {
  push(); noFill(); stroke(100); translate(0,0,bedrockZ); plane(900); pop(); // BEDROCK
  push(); noFill(); stroke(0, 255, 150, 50); translate(0,0,workbenchZ); plane(900); pop(); // WORKBENCH
}
```

```
let oscillators = [];
let bedrockMemory = []; 
let maxBedrock = 40; 
let workbenchZ = 250; 
let bedrockZ = -250;
let inputField, injectBtn;

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  
  // --- INJECTION UI ---
  inputField = createInput('ACE');
  inputField.position(20, 20);
  injectBtn = createButton('Inject Sequence');
  injectBtn.position(inputField.x + inputField.width + 10, 20);
  injectBtn.mousePressed(injectBytes);

  // Initial random noise
  for (let i = 0; i < 20; i++) {
    oscillators.push(new Particle(floor(random(255)), i));
  }
}

function draw() {
  background(5);
  orbitControl();

  // Recycle old memory to prevent slowdown
  if (bedrockMemory.length > maxBedrock) bedrockMemory.shift();

  // Consolidation: 3 Bedrock -> 1 Super-Token
  if (bedrockMemory.length > 5) checkForSuperToken();

  // Update Active Substrate
  oscillators.sort((a, b) => b.z - a.z);
  for (let i = oscillators.length - 1; i >= 0; i--) {
    let p = oscillators[i];
    p.update(oscillators, bedrockMemory);
    p.display();

    // Annealing
    if (p.z > 0.95 && p.energy > 0.98) {
      p.state = "ANNEALED";
      bedrockMemory.push(p);
      oscillators.splice(i, 1);
    }
  }

  for (let m of bedrockMemory) m.display();
  drawPlanes();
}

function injectBytes() {
  let str = inputField.value();
  for (let i = 0; i < str.length; i++) {
    // Convert character to byte value (0-255)
    let val = str.charCodeAt(i) % 256; 
    oscillators.push(new Particle(val, oscillators.length + i));
  }
}

class Particle {
  constructor(byte_val, id, isSuper = false) {
    this.byte = byte_val;
    this.id = id;
    this.isSuper = isSuper;
    this.freq = map(byte_val, 0, 255, 0.02, 0.15);
    this.phase = random(TWO_PI);
    this.z = -0.8; 
    this.energy = 0.1;
    this.state = "NOISE";
  }

  update(peers, memory) {
    if (this.state === "ANNEALED") return;
    this.z -= 0.007; // Gravity

    // Peer Resonance
    for (let p of peers) {
      if (p === this) continue;
      let ratio = max(this.freq, p.freq) / min(this.freq, p.freq);
      if (ratio % 1.0 < 0.03 || ratio % 1.0 > 0.97) {
        this.z += (0.018 / ratio);
        this.energy += 0.005;
        this.phase += (p.phase - this.phase) * 0.05;
      }
    }

    // Attractor Support (Bedrock & Gold)
    for (let m of memory) {
      let ratio = max(this.freq, m.freq) / min(this.freq, m.freq);
      if (ratio % 1.0 < 0.01 || ratio % 1.0 > 0.99) {
        this.z += m.isSuper ? 0.05 : 0.025; // Super-Tokens attract more strongly
        this.energy += 0.01;
        if (this.z > 0) drawTether(this, m);
      }
    }

    this.z = constrain(this.z, -1, 1.2);
    this.energy = constrain(this.energy, 0, 1);
    this.phase += this.freq;
    this.state = (this.z > 0.8) ? "WORKBENCH" : "NOISE";
  }

  display() {
    push();
    let tx = map(this.byte, 0, 255, -400, 400);
    let ty = map(this.id % 20, 0, 20, -300, 300);
    let tz = (this.state === "ANNEALED") ? bedrockZ : this.z * workbenchZ;
    translate(tx, ty, tz);

    if (this.isSuper) { fill(255, 215, 0); sphere(22); }
    else if (this.state === "ANNEALED") { fill(255, 50, 50); box(12); }
    else if (this.state === "WORKBENCH") { fill(0, 255, 150); sphere(14 + sin(this.phase)*5); }
    else { fill(100, 100, 255, 150); sphere(7); }
    pop();
  }
}

function checkForSuperToken() {
  let g = bedrockMemory.slice(0, 3);
  let r1 = (max(g[0].freq, g[1].freq)/min(g[0].freq, g[1].freq)) % 1.0;
  let r2 = (max(g[1].freq, g[2].freq)/min(g[1].freq, g[2].freq)) % 1.0;
  if (r1 < 0.05 && r2 < 0.05) {
    let avg = floor((g[0].byte + g[1].byte + g[2].byte) / 3);
    bedrockMemory.splice(0, 3);
    let st = new Particle(avg, 999, true);
    st.state = "ANNEALED";
    bedrockMemory.push(st);
  }
}

function drawTether(p, m) {
  push();
  stroke(m.isSuper ? color(255, 215, 0, 100) : color(255, 40));
  line(map(p.byte,0,255,-400,400), map(p.id%20,0,20,-300,300), p.z*workbenchZ,
       map(m.byte,0,255,-400,400), map(m.id%20,0,20,-300,300), bedrockZ);
  pop();
}

function drawPlanes() {
  push(); noFill(); stroke(100); translate(0,0,bedrockZ); plane(900); pop();
  push(); noFill(); stroke(0, 255, 150, 50); translate(0,0,workbenchZ); plane(900); pop();
}
```

```
let oscillators = [];
let bedrockMemory = []; 
let maxBedrock = 40; 
let workbenchZ = 250; 
let bedrockZ = -250;
let inputField, injectBtn, predictBtn;

function setup() {
  createCanvas(windowWidth, windowHeight, WEBGL);
  
  inputField = createInput('ABC');
  inputField.position(20, 20);
  injectBtn = createButton('Inject Evidence');
  injectBtn.position(inputField.x + inputField.width + 10, 20);
  injectBtn.mousePressed(injectBytes);

  // Initial random noise
  for (let i = 0; i < 15; i++) {
    oscillators.push(new Particle(floor(random(255)), i));
  }
}

function draw() {
  background(5);
  orbitControl();

  if (bedrockMemory.length > maxBedrock) bedrockMemory.shift();
  checkForSuperToken();

  oscillators.sort((a, b) => b.z - a.z);
  
  for (let i = oscillators.length - 1; i >= 0; i--) {
    let p = oscillators[i];
    p.update(oscillators, bedrockMemory);
    p.display();

    if (p.z > 0.95 && p.energy > 0.98) {
      p.state = "ANNEALED";
      bedrockMemory.push(p);
      oscillators.splice(i, 1);
    }
  }

  // PREDICTION LOGIC: Top-Down Hallucination
  // If a Super-Token is "resonating" but missing a child, spawn a Ghost
  generatePredictions();

  for (let m of bedrockMemory) m.display();
  drawPlanes();
}

function injectBytes() {
  let str = inputField.value();
  for (let i = 0; i < str.length; i++) {
    let val = str.charCodeAt(i) % 256; 
    oscillators.push(new Particle(val, oscillators.length + i));
  }
}

function generatePredictions() {
  // Look at Gold Super-Tokens
  for (let m of bedrockMemory) {
    if (m.isSuper) {
      // Check if any active oscillators are harmonically related
      let resonanceCount = oscillators.filter(o => 
        abs((max(o.freq, m.freq)/min(o.freq, m.freq)) % 1.0) < 0.02
      ).length;

      // If the Super-Token is "Partially Active" (1 or 2 children present)
      // but lacks a full set, it "Hallucinates" the missing piece
      if (resonanceCount >= 1 && resonanceCount < 3 && oscillators.length < 40) {
        if (random() < 0.02) { // Probability of prediction firing
          let ghost = new Particle(m.byte, oscillators.length, false);
          ghost.isGhost = true; // Special state
          ghost.z = 0.5; // Starts midway up the ramp
          oscillators.push(ghost);
        }
      }
    }
  }
}

class Particle {
  constructor(byte_val, id, isSuper = false) {
    this.byte = byte_val;
    this.id = id;
    this.isSuper = isSuper;
    this.isGhost = false;
    this.freq = map(byte_val, 0, 255, 0.02, 0.15);
    this.phase = random(TWO_PI);
    this.z = -0.8; 
    this.energy = 0.1;
    this.state = "NOISE";
  }

  update(peers, memory) {
    if (this.state === "ANNEALED") return;
    this.z -= 0.007; // Bedrock Gravity

    // Peer Resonance
    for (let p of peers) {
      if (p === this) continue;
      let ratio = max(this.freq, p.freq) / min(this.freq, p.freq);
      if (ratio % 1.0 < 0.03 || ratio % 1.0 > 0.97) {
        this.z += (0.02 / ratio);
        this.energy += 0.005;
        this.phase += (p.phase - this.phase) * 0.05;
      }
    }

    // Top-Down Support (Prediction Field)
    for (let m of memory) {
      let ratio = max(this.freq, m.freq) / min(this.freq, m.freq);
      if (ratio % 1.0 < 0.01 || ratio % 1.0 > 0.99) {
        this.z += m.isSuper ? 0.06 : 0.03;
        this.energy += 0.01;
      }
    }

    this.z = constrain(this.z, -1, 1.2);
    this.energy = constrain(this.energy, 0, 1);
    this.phase += this.freq;
    this.state = (this.z > 0.8) ? "WORKBENCH" : "NOISE";
  }

  display() {
    push();
    let tx = map(this.byte, 0, 255, -400, 400);
    let ty = map(this.id % 20, 0, 20, -300, 300);
    let tz = (this.state === "ANNEALED") ? bedrockZ : this.z * workbenchZ;
    translate(tx, ty, tz);

    if (this.isSuper) { fill(255, 215, 0); sphere(22); }
    else if (this.isGhost) { fill(255, 255, 255, 100); stroke(255); sphere(10); } // GHOST: White/Transparent
    else if (this.state === "ANNEALED") { fill(255, 50, 50); box(12); }
    else if (this.state === "WORKBENCH") { fill(0, 255, 150); sphere(14 + sin(this.phase)*5); }
    else { fill(100, 100, 255, 150); sphere(7); }
    pop();
  }
}

function checkForSuperToken() {
  if (bedrockMemory.length < 3) return;
  let g = bedrockMemory.slice(-3); // Check most recent
  let r1 = (max(g[0].freq, g[1].freq)/min(g[0].freq, g[1].freq)) % 1.0;
  let r2 = (max(g[1].freq, g[2].freq)/min(g[1].freq, g[2].freq)) % 1.0;
  if ((r1 < 0.05 || r1 > 0.95) && (r2 < 0.05 || r2 > 0.95)) {
    let avg = floor((g[0].byte + g[1].byte + g[2].byte) / 3);
    bedrockMemory.splice(bedrockMemory.length-3, 3);
    let st = new Particle(avg, 999, true);
    st.state = "ANNEALED";
    bedrockMemory.push(st);
  }
}

function drawPlanes() {
  push(); noFill(); stroke(100); translate(0,0,bedrockZ); plane(900); pop();
  push(); noFill(); stroke(0, 255, 150, 50); translate(0,0,workbenchZ); plane(900); pop();
}
```



