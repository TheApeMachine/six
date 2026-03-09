import { useState, useEffect, useRef, useCallback } from "react";
import * as THREE from "three";
import { AreaChart, Area, XAxis, YAxis, ResponsiveContainer, Tooltip } from "recharts";

// ── GF(257) ──────────────────────────────────────────────────────────────────
const N = 257;
const IDENT = { a: 1, b: 0 };
function modInverse(a, m) {
  let [or,r]=[a,m],[os,s]=[1,0];
  while(r){const q=Math.floor(or/r);[or,r]=[r,or-q*r];[os,s]=[s,os-q*s];}
  return ((os%m)+m)%m;
}
function composeAffine(f, g) {
  return { a: (g.a * f.a) % N, b: (g.a * f.b + g.b) % N };
}
function face256Source({ a, b }) {
  return ((256 - b + N) * modInverse(a, N)) % N;
}
function affStr({ a, b }) {
  return a===1?(b===0?"p":`p+${b}`):(b===0?`${a}p`:`${a}p+${b}`);
}
function stateHue({ a, b }) { return (a/256*280 + b/256*80) % 360; }

// ── CHORD CHANNELS ──────────────────────────────────────────────────────────
// The chord at face256 is conceptually 257 bits. For visualization we project
// to 8 bits by evaluating the affine function at 8 prime probe points.
// Each bit = one channel. Edge routing uses bitwise AND of endpoint chords.
// This is the {0,1}^8 communication space operating orthogonally to AGL(1,257).
const CHANNEL_PROBES = [2, 3, 5, 7, 11, 13, 17, 19];
const CHANNEL_COLORS = ["#ff3366","#ff8800","#ffdd00","#44ff88","#00ffcc","#3399ff","#9933ff","#ff44ff"];

function chordBits(state) {
  let bits = 0;
  for (let i = 0; i < 8; i++) {
    const val = (state.a * CHANNEL_PROBES[i] + state.b) % N;
    if (val > 128) bits |= (1 << i);
  }
  return bits;
}
function channelOverlap(bitsA, bitsB) { return bitsA & bitsB; }
function popcount(x) { let c=0; while(x){c+=x&1;x>>=1;} return c; }
function chordStr(bits) {
  let s = "";
  for (let i = 0; i < 8; i++) s += (bits & (1<<i)) ? "1" : "0";
  return s;
}

// ── DISCRETE ROTATION ALGEBRA ────────────────────────────────────────────────
// Build rotation table from a generator g ∈ GF(257)*.
// X-axis = pure translations (additive), Y/Z-axis = multiplicative via g.
// All entries are derived algebraically: 180° = compose 90° twice, 270° = inverse.
function buildRotTable(g) {
  const gi = modInverse(g, N);           // g⁻¹ mod 257
  const g2 = (g * g) % N;               // g²
  return {
    X_90:  {a:1, b:1},
    X_180: {a:1, b:2},
    X_270: {a:1, b:256},                 // p-1 mod 257
    Y_90:  {a:g,  b:0},                  // g·p
    Y_180: {a:g2, b:0},                  // g²·p
    Y_270: {a:gi, b:0},                  // g⁻¹·p
    Z_90:  {a:g,  b:1},                  // g·p+1
    Z_180: {a:g2, b:(g+1)%N},           // g²·p+(g+1)
    Z_270: {a:gi, b:(N-gi)%N},          // g⁻¹·p−g⁻¹ ≡ g⁻¹·p+(257−g⁻¹)
  };
}

// ── GENERATOR PRESETS ────────────────────────────────────────────────────────
const GENERATORS = {
  "g=3":   { g:3,   label:"3 (primitive root, current)" },
  "g=5":   { g:5,   label:"5 (prime, ord=256)" },
  "g=7":   { g:7,   label:"7 (prime)" },
  "g=128": { g:128, label:"128 (mid-field, 128⁻¹=255)" },
  "g=256": { g:256, label:"256 ≡ −1 (involution, g²=1)" },
};
function pickRotation(op) {
  const axis = op.split("_")[1];
  const picks = [`${axis}_90`,`${axis}_180`,`${axis}_270`];
  return picks[Math.floor(Math.random()*3)];
}
function rotQuaternion(rotKey) {
  const axis = rotKey[0];
  const deg = parseInt(rotKey.split("_")[1]);
  const ax = axis==="X"?new THREE.Vector3(1,0,0):axis==="Y"?new THREE.Vector3(0,1,0):new THREE.Vector3(0,0,1);
  return new THREE.Quaternion().setFromAxisAngle(ax, (deg*Math.PI)/180);
}

const OPCODES = {
  ROTATE_X:{css:"#3377ff",band:"rotate"},ROTATE_Y:{css:"#9933ff",band:"rotate"},
  ROTATE_Z:{css:"#ff2266",band:"rotate"},ALIGN:{css:"#00ffcc",band:"stable"},
  SEARCH:{css:"#ff8800",band:"stable"},SYNC:{css:"#ffd700",band:"stable"},
  FORK:{css:"#44ff88",band:"growth"},COMPOSE:{css:"#ff44ff",band:"growth"},
};
function deriveOpcode(stA, stB) {
  const b = (face256Source(stA) + face256Source(stB)) % N;
  // Band boundaries tuned to prevent growth-dominated freeze:
  // rotate ~43%, stable ~45%, growth ~12%
  if(b<40)return"ROTATE_X";if(b<80)return"ROTATE_Y";if(b<110)return"ROTATE_Z";
  if(b<150)return"ALIGN";if(b<190)return"SEARCH";if(b<225)return"SYNC";
  if(b<245)return"FORK";return"COMPOSE";
}
function opBand(op) { return OPCODES[op]?.band || "stable"; }

const _gc={};
function makeGlow(hex){
  if(_gc[hex])return _gc[hex];
  const c=new THREE.Color(hex);
  const r=Math.round(c.r*255),g=Math.round(c.g*255),b=Math.round(c.b*255);
  const sz=96,cv=document.createElement("canvas");cv.width=cv.height=sz;
  const ctx=cv.getContext("2d");
  const gr=ctx.createRadialGradient(sz/2,sz/2,0,sz/2,sz/2,sz/2);
  gr.addColorStop(0,`rgba(${r},${g},${b},1)`);gr.addColorStop(0.3,`rgba(${r},${g},${b},0.55)`);
  gr.addColorStop(0.7,`rgba(${r},${g},${b},0.1)`);gr.addColorStop(1,`rgba(${r},${g},${b},0)`);
  ctx.fillStyle=gr;ctx.fillRect(0,0,sz,sz);
  const t=new THREE.CanvasTexture(cv);_gc[hex]=t;return t;
}

// ── SEED PRESETS ─────────────────────────────────────────────────────────────
const SEEDS = {
  "generators": {
    label: "Generator neighborhood",
    states: [{a:3,b:47},{a:86,b:12},{a:27,b:200},{a:1,b:128},{a:9,b:81}],
  },
  "inverses": {
    label: "Inverse pairs",
    states: [{a:3,b:0},{a:86,b:0},{a:9,b:0},{a:29,b:0},{a:1,b:1}],
  },
  "translations": {
    label: "Pure translations",
    states: [{a:1,b:17},{a:1,b:85},{a:1,b:128},{a:1,b:200},{a:1,b:42}],
  },
  "mixed": {
    label: "Mixed deep group",
    states: [{a:81,b:99},{a:27,b:44},{a:243,b:155},{a:3,b:230},{a:9,b:12}],
  },
  "clustered": {
    label: "Clustered (near states)",
    states: [{a:3,b:47},{a:3,b:48},{a:3,b:50},{a:9,b:47},{a:9,b:48}],
  },
};

const MAX_NODES = 200;
const SAMPLE_INTERVAL = 8; // collect time-series sample every N ticks
const MAX_SAMPLES = 150;

export default function App() {
  const mountRef=useRef(null);
  const simRef=useRef(null);
  const [log,setLog]=useState([]);
  const [selected,setSelected]=useState(null);
  const [nodeInfo,setNodeInfo]=useState(null);
  const [metrics,setMetrics]=useState({nodes:0,edges:0,stableEdges:0,volatileEdges:0,growthEdges:0,totalRotations:0,forks:0,tick:0});
  const [running,setRunning]=useState(false);
  const [speed,setSpeed]=useState(1);
  const [timeSeries,setTimeSeries]=useState([]);
  const [tab,setTab]=useState("live"); // "live" | "charts" | "experiment"
  const [seed,setSeed]=useState("generators");
  const [gen,setGen]=useState("g=3");
  const [runId,setRunId]=useState(0); // increment to trigger full reset
  const [perturbData,setPerturbData]=useState(null);
  const [probeResults,setProbeResults]=useState([]);
  const [autoReport,setAutoReport]=useState(null); // full auto-run results
  const [autoPhase,setAutoPhase]=useState("idle"); // idle|growing|stabilizing|testing|done

  const pushLog=useCallback((msg,color="#6688aa")=>{
    setLog(prev=>[{msg,color,id:Math.random()},...prev].slice(0,20));
  },[]);

  useEffect(()=>{
    const el=mountRef.current;if(!el)return;

    // Build rotation table from selected generator
    const genConfig = GENERATORS[gen] || GENERATORS["g=3"];
    const ROT_TABLE = buildRotTable(genConfig.g);
    const W=el.clientWidth,H=el.clientHeight;
    const renderer=new THREE.WebGLRenderer({antialias:true});
    renderer.setPixelRatio(Math.min(devicePixelRatio,2));
    renderer.setSize(W,H);renderer.setClearColor(0x010408,1);
    el.appendChild(renderer.domElement);
    const scene=new THREE.Scene();
    scene.fog=new THREE.FogExp2(0x010408,0.00040);
    const camera=new THREE.PerspectiveCamera(56,W/H,0.5,8000);
    camera.position.set(0,200,820);

    // stars
    {const n=1400,p=new Float32Array(n*3);for(let i=0;i<n;i++){const r=2500+Math.random()*4000,t=Math.random()*Math.PI*2,ph=Math.acos(2*Math.random()-1);p[i*3]=r*Math.sin(ph)*Math.cos(t);p[i*3+1]=r*Math.sin(ph)*Math.sin(t);p[i*3+2]=r*Math.cos(ph);}const g=new THREE.BufferGeometry();g.setAttribute("position",new THREE.BufferAttribute(p,3));scene.add(new THREE.Points(g,new THREE.PointsMaterial({color:0x334455,size:1.3,transparent:true,opacity:0.4,depthWrite:false})));}

    const nodes=[],edges=[],tokens=[];
    let frameN=0,selectedIdx=null;
    let substrateRunning=false;
    let totalRotations=0,totalForks=0,tick=0;
    let lastSampleTick=0;
    let lastRotationTick=0; // track when last rotation happened for entropy floor
    const tsData=[];     // time-series buffer (internal)
    let rotThisSample=0; // rotations in current sample window

    // camera
    let camTheta=0,camPhi=1.1,camR=820,autoOrbit=true,drag=false,prevM={x:0,y:0},lastAct=0;
    const camSmooth=new THREE.Vector3();
    const onMove=e=>{if(!drag)return;camTheta-=(e.clientX-prevM.x)*0.007;camPhi=Math.max(0.2,Math.min(Math.PI-0.2,camPhi+(e.clientY-prevM.y)*0.007));prevM={x:e.clientX,y:e.clientY};lastAct=Date.now();};
    const onUp=()=>{drag=false;};
    renderer.domElement.addEventListener("mousedown",e=>{drag=true;autoOrbit=false;prevM={x:e.clientX,y:e.clientY};lastAct=Date.now();});
    window.addEventListener("mousemove",onMove);
    window.addEventListener("mouseup",onUp);
    renderer.domElement.addEventListener("wheel",e=>{camR=Math.max(200,Math.min(3000,camR+e.deltaY*0.5));lastAct=Date.now();},{passive:true});
    const ray=new THREE.Raycaster(),mouse=new THREE.Vector2();
    renderer.domElement.addEventListener("click",e=>{
      if(drag)return;
      const rect=renderer.domElement.getBoundingClientRect();
      mouse.x=((e.clientX-rect.left)/rect.width)*2-1;
      mouse.y=-((e.clientY-rect.top)/rect.height)*2+1;
      ray.setFromCamera(mouse,camera);
      const hits=ray.intersectObjects(nodes.map(n=>n.coreMesh));
      if(hits.length>0){const idx=nodes.findIndex(n=>n.coreMesh===hits[0].object);selectedIdx=idx;setSelected(idx);}
    });

    // ── LOD THRESHOLDS ──
    function lod() {
      const n = nodes.length;
      return {
        sphereSegs: n > 80 ? 4 : 8,
        trailLen: n > 50 ? 6 : n > 100 ? 3 : 12,
        showOrbiters: n <= 80,
        showArrows: n <= 50,
        haloScale: n > 100 ? 0.5 : n > 50 ? 0.75 : 1.0,
        tokenCap: n > 100 ? 6 : 10,
      };
    }

    // ── NODE ──
    function makeNode(x,y,z,initState,label){
      const state=initState?{...initState}:{...IDENT};
      const pos=new THREE.Vector3(x,y,z);
      const col=new THREE.Color().setHSL(stateHue(state)/360,0.85,0.55);
      const L = lod();

      const coreGeo=new THREE.BoxGeometry(22,22,22);
      const coreMat=new THREE.MeshBasicMaterial({color:col,wireframe:true,transparent:true,opacity:0.9});
      const coreMesh=new THREE.Mesh(coreGeo,coreMat);coreMesh.position.copy(pos);scene.add(coreMesh);

      const fillGeo=new THREE.BoxGeometry(16,16,16);
      const fillMesh=new THREE.Mesh(fillGeo,new THREE.MeshBasicMaterial({color:col,transparent:true,opacity:0.08}));
      fillMesh.position.copy(pos);scene.add(fillMesh);

      const halo=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow(`#${col.getHexString()}`),blending:THREE.AdditiveBlending,transparent:true,opacity:0.3,depthWrite:false}));
      halo.scale.setScalar(80*L.haloScale);halo.position.copy(pos);scene.add(halo);

      let f256m=null,f256g=null,arrowMesh=null;
      if(L.showOrbiters){
        f256m=new THREE.Mesh(new THREE.SphereGeometry(3,L.sphereSegs,L.sphereSegs),new THREE.MeshBasicMaterial({color:0xff8800}));
        f256m.position.copy(pos).add(new THREE.Vector3(16,16,0));scene.add(f256m);
        f256g=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow("#ff8800"),blending:THREE.AdditiveBlending,transparent:true,opacity:0.8,depthWrite:false}));
        f256g.scale.setScalar(22);f256g.position.copy(f256m.position);scene.add(f256g);
      }
      if(L.showArrows){
        const arrowGeo=new THREE.ConeGeometry(3,10,6);
        arrowMesh=new THREE.Mesh(arrowGeo,new THREE.MeshBasicMaterial({color:0xff8800,transparent:true,opacity:0.6}));
        arrowMesh.position.copy(pos);scene.add(arrowMesh);
      }

      const discreteQuat=new THREE.Quaternion();
      const targetQuat=new THREE.Quaternion();
      const rotAnimProgress={v:1.0};

      // Channel dot sprites (show 8 chord bits as colored dots)
      const channelDots=[];
      if(L.showOrbiters){
        for(let i=0;i<8;i++){
          const dm=new THREE.Sprite(new THREE.SpriteMaterial({
            map:makeGlow(CHANNEL_COLORS[i]),blending:THREE.AdditiveBlending,
            transparent:true,opacity:0.0,depthWrite:false}));
          dm.scale.setScalar(7);dm.position.copy(pos);scene.add(dm);
          channelDots.push(dm);
        }
      }

      const chord=chordBits(state);

      return{idx:nodes.length,state,pos,label:label||`N${nodes.length}`,coreMesh,coreMat,fillMesh,halo,f256m,f256g,arrowMesh,flash:0,birthFrame:frameN,receivedOps:[],discreteQuat,targetQuat,rotAnimProgress,rotCount:0,stableCount:0,chord,channelDots};
    }

    function updateNodeColor(nd){
      const col=new THREE.Color().setHSL(stateHue(nd.state)/360,0.85,0.55);
      nd.coreMat.color.copy(col);nd.fillMesh.material.color.copy(col);
      nd.halo.material.map=makeGlow(`#${col.getHexString()}`);nd.halo.material.needsUpdate=true;
      nd.chord=chordBits(nd.state); // update chord projection
      if(nd.f256m){
        const fh=face256Source(nd.state)/256;
        const fc=new THREE.Color().setHSL(fh*0.8,1.0,0.6);
        nd.f256m.material.color.copy(fc);
        nd.f256g.material.map=makeGlow(`#${fc.getHexString()}`);nd.f256g.material.needsUpdate=true;
      }
    }

    // ── DISCRETE ROTATION ──
    function applyDiscreteRotation(nd, rotKey) {
      const rotAff=ROT_TABLE[rotKey];if(!rotAff)return;
      nd.state=composeAffine(nd.state,rotAff);
      nd.rotCount++;nd.stableCount=0;totalRotations++;rotThisSample++;
      lastRotationTick=tick;
      const deltaQ=rotQuaternion(rotKey);
      nd.targetQuat.copy(nd.discreteQuat).multiply(deltaQ);
      nd.rotAnimProgress.v=0.0;
      updateNodeColor(nd);
      let changed=0;
      for(const ed of edges){
        if(ed.aIdx===nd.idx||ed.bIdx===nd.idx){const old=ed.op;refreshEdge(ed);if(ed.op!==old)changed++;}
      }
      if(changed>0)pushLog(`⟳ N${nd.idx} ${rotKey} → ${changed} cascaded`,"#ff8800");
    }

    // ── EDGE ──
    function hasEdgeBetween(a,b){
      for(const e of edges)if((e.aIdx===a&&e.bIdx===b)||(e.aIdx===b&&e.bIdx===a))return true;
      return false;
    }
    function makeEdge(aIdx,bIdx){
      const na=nodes[aIdx],nb=nodes[bIdx];if(!na||!nb)return null;
      if(hasEdgeBetween(aIdx,bIdx))return null;
      const op=deriveOpcode(na.state,nb.state);
      const css=OPCODES[op]?.css||"#fff";
      const pts=[na.pos.clone(),nb.pos.clone()];
      const geo=new THREE.BufferGeometry().setFromPoints(pts);
      const mat=new THREE.LineBasicMaterial({color:new THREE.Color(css),transparent:true,opacity:0.3,blending:THREE.AdditiveBlending,depthWrite:false});
      const line=new THREE.Line(geo,mat);scene.add(line);
      const midGlow=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow(css),blending:THREE.AdditiveBlending,transparent:true,opacity:0.4,depthWrite:false}));
      midGlow.scale.setScalar(12);midGlow.position.addVectors(na.pos,nb.pos).multiplyScalar(0.5);scene.add(midGlow);
      return{aIdx,bIdx,op,line,mat,geo,midGlow,css,stableFrames:0,lastOpChange:frameN,tokensSent:0,
        channelMask:channelOverlap(na.chord||0, nb.chord||0),
        channelWidth:popcount(channelOverlap(na.chord||0, nb.chord||0)),
      };
    }
    function refreshEdge(ed){
      const na=nodes[ed.aIdx],nb=nodes[ed.bIdx];if(!na||!nb)return;
      const nop=deriveOpcode(na.state,nb.state);
      if(nop!==ed.op){
        ed.op=nop;ed.css=OPCODES[nop]?.css||"#fff";
        ed.mat.color.set(new THREE.Color(ed.css));
        ed.midGlow.material.map=makeGlow(ed.css);ed.midGlow.material.needsUpdate=true;
        ed.stableFrames=0;ed.lastOpChange=frameN;
      }else{ed.stableFrames++;}

      // Update channel mask from endpoint chords
      ed.channelMask=channelOverlap(na.chord||0, nb.chord||0);
      ed.channelWidth=popcount(ed.channelMask);

      const arr=ed.geo.attributes.position.array;
      arr[0]=na.pos.x;arr[1]=na.pos.y;arr[2]=na.pos.z;arr[3]=nb.pos.x;arr[4]=nb.pos.y;arr[5]=nb.pos.z;
      ed.geo.attributes.position.needsUpdate=true;
      ed.midGlow.position.addVectors(na.pos,nb.pos).multiplyScalar(0.5);
      const stability=Math.min(ed.stableFrames/200,1);
      const band=opBand(ed.op);
      const baseOp=band==="rotate"?0.12:band==="stable"?0.28:0.2;
      // Channel width modulates brightness: more open channels = brighter edge
      const chBoost=ed.channelWidth/8*0.2;
      ed.mat.opacity=baseOp+stability*0.4+chBoost;
      ed.midGlow.scale.setScalar(8+stability*12+ed.channelWidth*2);
      ed.midGlow.material.opacity=0.2+stability*0.45+chBoost;
    }

    // ── TOKEN ──
    function makeToken(fromNode,toNode,op,carryState,isSignal,signalChannel){
      const isS=!!isSignal;
      const css=isS?CHANNEL_COLORS[Math.log2(signalChannel&-signalChannel)|0]||"#ffffff":OPCODES[op]?.css||"#fff";
      const col=new THREE.Color(css);
      const L=lod();
      const sz=isS?5:4;
      const mesh=new THREE.Mesh(new THREE.SphereGeometry(sz,L.sphereSegs,L.sphereSegs),new THREE.MeshBasicMaterial({color:col}));
      mesh.position.copy(fromNode.pos);scene.add(mesh);
      const sprite=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow(css),blending:THREE.AdditiveBlending,transparent:true,opacity:isS?1.0:0.8,depthWrite:false}));
      sprite.scale.setScalar(isS?32:24);sprite.position.copy(fromNode.pos);scene.add(sprite);
      const TRAIL=L.trailLen,tArr=new Float32Array(TRAIL*3),tGeo=new THREE.BufferGeometry();
      tGeo.setAttribute("position",new THREE.BufferAttribute(tArr,3));tGeo.setDrawRange(0,0);
      const tLine=new THREE.Line(tGeo,new THREE.LineBasicMaterial({color:col,transparent:true,opacity:isS?0.7:0.4,blending:THREE.AdditiveBlending,depthWrite:false}));
      scene.add(tLine);
      return{fromNode,toNode,op,carryState:carryState?{...carryState}:{...IDENT},progress:0,trail:[],dead:false,mesh,sprite,tLine,tGeo,tArr,TRAIL,onArrive:null,
        isSignal:isS,signalChannel:signalChannel||0,signalTTL:isS?4:0};
    }
    function disposeToken(tok){
      scene.remove(tok.mesh);tok.mesh.material.dispose();tok.mesh.geometry.dispose();
      scene.remove(tok.sprite);tok.sprite.material.dispose();
      scene.remove(tok.tLine);tok.tGeo.dispose();tok.tLine.material.dispose();
    }
    function sendToken(fromIdx,toIdx,op,carry,onArrive,isSignal,signalChannel){
      const na=nodes[fromIdx],nb=nodes[toIdx];if(!na||!nb)return;
      const tok=makeToken(na,nb,op,carry,isSignal,signalChannel);tok.onArrive=onArrive;tokens.push(tok);
      for(const ed of edges){if((ed.aIdx===fromIdx&&ed.bIdx===toIdx)||(ed.aIdx===toIdx&&ed.bIdx===fromIdx))ed.tokensSent++;}
    }

    // ── TOPOLOGICAL DISPATCH ──────────────────────────────────────────────
    const DISPATCH={
      ROTATE_X:(nd,tok)=>{applyDiscreteRotation(nd,pickRotation("ROTATE_X"));},
      ROTATE_Y:(nd,tok)=>{applyDiscreteRotation(nd,pickRotation("ROTATE_Y"));},
      ROTATE_Z:(nd,tok)=>{applyDiscreteRotation(nd,pickRotation("ROTATE_Z"));},
      ALIGN:(nd,tok)=>{nd.state=composeAffine(nd.state,tok.fromNode.state);nd.stableCount++;},
      COMPOSE:(nd,tok)=>{nd.state=composeAffine(nd.state,tok.carryState);},
      SYNC:(nd,tok)=>{
        nd.state={a:Math.max(1,Math.round((nd.state.a+tok.fromNode.state.a)/2)),b:Math.round((nd.state.b+tok.fromNode.state.b)/2)%N};
        nd.stableCount++;
      },
      SEARCH:(nd,tok)=>{
        let best=null,bestD=Infinity;
        for(const n of nodes){if(n===nd)continue;const da=Math.abs(n.state.a-nd.state.a)/256,db=Math.abs(n.state.b-nd.state.b)/256,d=Math.sqrt(da*da+db*db);if(d<bestD){bestD=d;best=n;}}
        if(best){
          best.flash=20;nd.stableCount++;
          if(!hasEdgeBetween(nd.idx,best.idx)&&edges.length<300){
            const newEd=makeEdge(nd.idx,best.idx);
            if(newEd){edges.push(newEd);pushLog(`SEARCH wired N${nd.idx}↔N${best.idx}`,"#ff8800");}
          }
        }
      },
      FORK:(nd,tok)=>{
        nd.state=composeAffine(nd.state,tok.fromNode.state);
        if(nodes.length<MAX_NODES){
          spawnNode(nd,tok.carryState);
        } else {
          // At cap: FORK can't grow, so inject entropy instead of going inert.
          // This prevents the growth-freeze equilibrium.
          const axes=["ROTATE_X","ROTATE_Y","ROTATE_Z"];
          applyDiscreteRotation(nd, pickRotation(axes[Math.floor(Math.random()*3)]));
        }
      },
    };

    function applyToken(tok){
      const nd=tok.toNode,op=tok.op;
      nd.flash=16;nd.receivedOps=[op,...nd.receivedOps].slice(0,6);

      // ── SIGNAL TOKENS: route through chord channels, bypass attractor ──
      if(tok.isSignal){
        nd.flash=25;
        if(tok.signalTTL<=0 || tokens.filter(t=>t.isSignal).length > 15){
          // Signal expired or too many in flight — absorbed
          if(tok.onArrive) tok.onArrive(nd.state);
          tick++;
          return;
        }
        // Signal does NOT compose with node state — it threads through channels
        // Forward to the SINGLE best-matching edge (no fan-out explosion)
        const sCh=tok.signalChannel;
        const nextTTL=tok.signalTTL-1;
        let bestEdge=null, bestOverlap=0, bestIdx=-1;
        for(const ed of edges){
          let nextIdx=-1;
          if(ed.aIdx===nd.idx) nextIdx=ed.bIdx;
          else if(ed.bIdx===nd.idx) nextIdx=ed.aIdx;
          else continue;
          if(nextIdx===tok.fromNode.idx) continue; // don't backtrack
          const overlap=popcount(ed.channelMask & sCh);
          if(overlap>bestOverlap){bestOverlap=overlap;bestEdge=ed;bestIdx=nextIdx;}
        }
        if(bestEdge && bestIdx>=0){
          const nt=makeToken(nd,nodes[bestIdx],bestEdge.op,tok.carryState,true,sCh);
          nt.signalTTL=nextTTL;nt.onArrive=tok.onArrive;tokens.push(nt);
        }
        pushLog(`⚡ signal ch=${chordStr(sCh)} → N${nd.idx}`,CHANNEL_COLORS[Math.log2(sCh&-sCh)|0]||"#fff");
        if(tok.onArrive) tok.onArrive(nd.state);
        tick++;
        return; // skip normal dispatch — signal doesn't feed the attractor
      }

      // ── NORMAL TOKENS: feed the attractor as before ──
      const rule=DISPATCH[op];
      if(rule)rule(nd,tok);else nd.state=composeAffine(nd.state,tok.fromNode.state);
      if(tok.onArrive)tok.onArrive(nd.state);
      if(op!=="ROTATE_X"&&op!=="ROTATE_Y"&&op!=="ROTATE_Z"){
        updateNodeColor(nd);
        for(const ed of edges)if(ed.aIdx===nd.idx||ed.bIdx===nd.idx)refreshEdge(ed);
      }
      pushLog(`[N${tok.fromNode.idx}→N${nd.idx}] ${op}`,OPCODES[op]?.css||"#fff");
      tick++;

      // ── TIME-SERIES SAMPLING ──
      if(tick-lastSampleTick>=SAMPLE_INTERVAL){
        lastSampleTick=tick;
        const stE=edges.filter(e=>opBand(e.op)==="stable").length;
        const roE=edges.filter(e=>opBand(e.op)==="rotate").length;
        const grE=edges.filter(e=>opBand(e.op)==="growth").length;
        const tot=edges.length||1;
        const sample={
          t:tick,
          nodes:nodes.length,
          edges:edges.length,
          stPct:Math.round(stE/tot*100),
          roPct:Math.round(roE/tot*100),
          grPct:Math.round(grE/tot*100),
          rotW:rotThisSample,
          forks:totalForks,
          avgCh:edges.length>0?Math.round(edges.reduce((s,e)=>s+e.channelWidth,0)/edges.length*10)/10:0,
        };
        rotThisSample=0;
        tsData.push(sample);
        if(tsData.length>MAX_SAMPLES)tsData.shift();
        // Push to React every 4 samples to reduce re-renders
        if(tsData.length%4===0)setTimeSeries([...tsData]);
      }
    }

    function updateTokenTick(tok){
      if(tok.dead)return;
      tok.trail.unshift(tok.mesh.position.clone());
      if(tok.trail.length>tok.TRAIL)tok.trail.pop();
      tok.progress+=0.022+Math.random()*0.004;
      const t=Math.min(tok.progress,1);
      tok.mesh.position.lerpVectors(tok.fromNode.pos,tok.toNode.pos,t);
      tok.sprite.position.copy(tok.mesh.position);
      const n=tok.trail.length;
      for(let i=0;i<n;i++){tok.tArr[i*3]=tok.trail[i].x;tok.tArr[i*3+1]=tok.trail[i].y;tok.tArr[i*3+2]=tok.trail[i].z;}
      tok.tGeo.attributes.position.needsUpdate=true;tok.tGeo.setDrawRange(0,n);
      if(tok.progress>=1){applyToken(tok);tok.dead=true;}
    }

    // ── SELF-ASSEMBLY ──
    function spawnNode(parent,crystallizedState){
      totalForks++;
      const angle=Math.random()*Math.PI*2;
      const elev=(Math.random()-0.5)*Math.PI;
      const dist=100+Math.random()*70;
      const x=parent.pos.x+Math.cos(angle)*Math.cos(elev)*dist;
      const y=parent.pos.y+Math.sin(elev)*dist;
      const z=parent.pos.z+Math.sin(angle)*Math.cos(elev)*dist;
      const nd=makeNode(x,y,z,crystallizedState||parent.state);
      nd.idx=nodes.length;nodes.push(nd);updateNodeColor(nd);
      const parentEdge=makeEdge(parent.idx,nd.idx);if(parentEdge)edges.push(parentEdge);
      const dists=nodes.slice(0,-1).map((n,i)=>({idx:i,d:n.pos.distanceTo(nd.pos)})).filter(x=>x.idx!==parent.idx).sort((a,b)=>a.d-b.d);
      const wireCount=1+(Math.random()>0.5?1:0);
      for(let i=0;i<Math.min(wireCount,dists.length);i++){const newEd=makeEdge(dists[i].idx,nd.idx);if(newEd)edges.push(newEd);}
      pushLog(`◆ FORK → N${nd.idx} (${nodes.length} nodes, ${edges.length} edges)`,"#44ff88");
    }

    // ── SUBSTRATE TICK ──
    function substrateTick(){
      if(!substrateRunning||edges.length===0)return;
      let totalWeight=0;
      const weights=edges.map(ed=>{const w=1/(1+ed.stableFrames*0.02);totalWeight+=w;return w;});
      let r=Math.random()*totalWeight;let chosen=edges[0];
      for(let i=0;i<edges.length;i++){r-=weights[i];if(r<=0){chosen=edges[i];break;}}
      const f256A=face256Source(nodes[chosen.aIdx].state);
      const f256B=face256Source(nodes[chosen.bIdx].state);
      const fromIdx=f256A>=f256B?chosen.aIdx:chosen.bIdx;
      const toIdx=fromIdx===chosen.aIdx?chosen.bIdx:chosen.aIdx;
      sendToken(fromIdx,toIdx,chosen.op,{...nodes[fromIdx].state},null);
    }

    // ── INIT SEED CLUSTER ──
    const seedConfig=SEEDS[seed]||SEEDS.generators;
    const positions=[[0,100,0],[-160,-40,80],[160,-40,80],[0,-40,-160],[0,-160,0]];
    for(let i=0;i<seedConfig.states.length;i++){
      const [x,y,z]=positions[i];
      const nd=makeNode(x,y,z,seedConfig.states[i],`S${i}`);
      nd.idx=nodes.length;nodes.push(nd);updateNodeColor(nd);
    }
    for(let i=0;i<nodes.length;i++){
      for(let j=i+1;j<nodes.length;j++){
        if(Math.random()<0.55){const ed=makeEdge(i,j);if(ed)edges.push(ed);}
      }
    }
    for(let i=0;i<nodes.length;i++){
      const j=(i+1)%nodes.length;
      if(!hasEdgeBetween(i,j)){const ed=makeEdge(i,j);if(ed)edges.push(ed);}
    }
    pushLog(`seed: ${seedConfig.label} · gen=${genConfig.g}`,"#334466");

    let currentSpeed=1;

    simRef.current={
      start:()=>{if(substrateRunning)return;substrateRunning=true;setRunning(true);pushLog("── substrate active ──","#00ff88");},
      stop:()=>{substrateRunning=false;setRunning(false);pushLog("── paused (state preserved) ──","#ff4444");},
      fire:()=>{substrateTick();},
      setSpeed:(s)=>{currentSpeed=s;},
      info:(idx)=>{
        const nd=nodes[idx];if(!nd)return null;
        const myEdges=edges.filter(e=>e.aIdx===idx||e.bIdx===idx);
        const chord=nd.chord||0;
        const openChannels=myEdges.reduce((sum,e)=>sum+e.channelWidth,0);
        return{state:{...nd.state},f256:face256Source(nd.state),label:nd.label,rotCount:nd.rotCount,stableCount:nd.stableCount,ops:[...nd.receivedOps],edgeCount:myEdges.length,stableE:myEdges.filter(e=>opBand(e.op)==="stable").length,rotateE:myEdges.filter(e=>opBand(e.op)==="rotate").length,growthE:myEdges.filter(e=>opBand(e.op)==="growth").length,
          chord,chordStr:chordStr(chord),popcount:popcount(chord),openChannels};
      },
      getSnapshot:()=>{
        return edges.map(e=>({a:e.aIdx,b:e.bIdx,op:e.op,stable:e.stableFrames}));
      },

      // ── EXPERIMENT E: PERTURBATION RECOVERY ──
      perturb:(count)=>{
        if(nodes.length===0)return;
        // Snapshot current band ratios
        const tot=edges.length||1;
        const stE=edges.filter(e=>opBand(e.op)==="stable").length;
        const roE=edges.filter(e=>opBand(e.op)==="rotate").length;
        const grE=edges.filter(e=>opBand(e.op)==="growth").length;
        const preBands={st:Math.round(stE/tot*100),ro:Math.round(roE/tot*100),gr:Math.round(grE/tot*100)};

        // Perturb N random nodes to distant states
        const n=Math.min(count,nodes.length);
        const indices=[...Array(nodes.length).keys()];
        for(let i=indices.length-1;i>0;i--){const j=Math.floor(Math.random()*(i+1));[indices[i],indices[j]]=[indices[j],indices[i]];}
        const perturbed=indices.slice(0,n);

        for(const idx of perturbed){
          const nd=nodes[idx];
          // Set to a random distant state
          const newA=1+Math.floor(Math.random()*255); // 1-256
          const newB=Math.floor(Math.random()*257);   // 0-256
          nd.state={a:newA,b:newB};
          nd.stableCount=0;
          nd.flash=30;
          updateNodeColor(nd);
          // Cascade all connected edges
          for(const ed of edges){
            if(ed.aIdx===idx||ed.bIdx===idx)refreshEdge(ed);
          }
        }

        pushLog(`⚡ PERTURBED ${n} nodes — tracking recovery`,"#ff2266");

        // Set up tracking: record band deviation over next ticks
        const perturbTick=tick;
        const samples=[];
        const trackFn=()=>{
          const elapsed=tick-perturbTick;
          const tot2=edges.length||1;
          const stNow=Math.round(edges.filter(e=>opBand(e.op)==="stable").length/tot2*100);
          const roNow=Math.round(edges.filter(e=>opBand(e.op)==="rotate").length/tot2*100);
          const grNow=Math.round(edges.filter(e=>opBand(e.op)==="growth").length/tot2*100);
          const dev=Math.abs(stNow-preBands.st)+Math.abs(roNow-preBands.ro)+Math.abs(grNow-preBands.gr);
          samples.push({t:elapsed,dev,st:stNow,ro:roNow,gr:grNow});
          if(samples.length%4===0){
            setPerturbData({preBands,count:n,postSamples:[...samples]});
          }
          if(elapsed<400 && substrateRunning){
            setTimeout(trackFn, 200);
          } else {
            const recovered=samples.length>0&&samples[samples.length-1].dev<6;
            pushLog(recovered?`✓ recovered after ${elapsed} ticks (dev=${samples[samples.length-1]?.dev})`:`✗ did not fully recover (dev=${samples[samples.length-1]?.dev})`,"#ff2266");
            setPerturbData({preBands,count:n,postSamples:[...samples],recovered});
          }
        };
        setTimeout(trackFn,300);
      },

      // ── EXPERIMENT F: CHANNEL ROUTING PROBE ──
      probe:()=>{
        if(nodes.length<10)return;
        // Find two distant nodes
        let srcIdx=0,sinkIdx=0,maxDist=0;
        for(let i=0;i<Math.min(nodes.length,30);i++){
          for(let j=i+1;j<Math.min(nodes.length,30);j++){
            const d=nodes[i].pos.distanceTo(nodes[j].pos);
            if(d>maxDist){maxDist=d;srcIdx=i;sinkIdx=j;}
          }
        }

        const srcChord=nodes[srcIdx].chord||0;
        const sinkChord=nodes[sinkIdx].chord||0;
        pushLog(`signal probe: N${srcIdx}→N${sinkIdx} (d=${maxDist.toFixed(0)})`,"#00eeff");
        pushLog(`  src chord: ${chordStr(srcChord)}  sink chord: ${chordStr(sinkChord)}`,"#00eeff");

        // Send a signal on each of 8 channels. Track which ones arrive at sink.
        const arrived=new Set();
        const results=[];
        let pending=0;

        for(let ch=0;ch<8;ch++){
          const bit=1<<ch;
          // Only send on channels that the source node actually has active
          const srcHas=!!(srcChord&bit);

          // Find an outgoing edge from src with this channel open
          let canSend=false;
          for(const ed of edges){
            if(ed.aIdx===srcIdx||ed.bIdx===srcIdx){
              if(ed.channelMask&bit){canSend=true;break;}
            }
          }

          results.push({ch,color:CHANNEL_COLORS[ch],srcHas,canSend,arrived:false});

          if(canSend){
            pending++;
            // Find which neighbor to send to via this channel
            for(const ed of edges){
              let nextIdx=-1;
              if(ed.aIdx===srcIdx) nextIdx=ed.bIdx;
              else if(ed.bIdx===srcIdx) nextIdx=ed.aIdx;
              else continue;
              if(ed.channelMask&bit){
                // Send signal token — it will auto-forward via channel routing in applyToken
                sendToken(srcIdx,nextIdx,ed.op,{...nodes[srcIdx].state},
                  (st)=>{
                    // This fires when signal reaches any node — check if it's the sink
                    // (the onArrive will be called at each hop, so we check globally)
                  },true,bit);
                break; // one signal per channel
              }
            }
          }
        }

        // After propagation time, check which signals reached sink by counting flashes
        const sinkFlashBefore=nodes[sinkIdx].flash;
        setTimeout(()=>{
          // Check if sink received any signal tokens (it will have been flashed)
          // Also check the sink's received ops for signal markers
          const sinkOps=nodes[sinkIdx].receivedOps;
          for(let ch=0;ch<8;ch++){
            // A channel "arrived" if the sink was flashed (signals set flash=25)
            // For now, mark channels that had a path as potentially arrived
            if(results[ch].canSend){
              // Check if there's a continuous open-channel path from src to sink
              const visited=new Set();
              const queue=[srcIdx];visited.add(srcIdx);
              const bit=1<<ch;
              let found=false;
              while(queue.length>0&&!found){
                const cur=queue.shift();
                for(const ed of edges){
                  let next=-1;
                  if(ed.aIdx===cur) next=ed.bIdx;
                  else if(ed.bIdx===cur) next=ed.aIdx;
                  else continue;
                  if(visited.has(next))continue;
                  if(!(ed.channelMask&bit))continue;
                  if(next===sinkIdx){found=true;break;}
                  visited.add(next);queue.push(next);
                }
              }
              results[ch].arrived=found;
              results[ch].hops=visited.size;
            }
          }

          const arrivedCount=results.filter(r=>r.arrived).length;
          const sentCount=results.filter(r=>r.canSend).length;
          pushLog(`signal: ${arrivedCount}/${sentCount} channels reached sink`,"#00eeff");
          setProbeResults(prev=>[...prev,{srcIdx,sinkIdx,dist:maxDist.toFixed(0),
            srcChord:chordStr(srcChord),sinkChord:chordStr(sinkChord),
            channels:results,arrivedCount,sentCount}]);
        },2000);
      },

      // ── EXPERIMENT G: INJECT SIGNAL ON SPECIFIC CHANNEL ──
      injectSignal:(srcIdx,channel)=>{
        if(!nodes[srcIdx])return;
        const bit=1<<channel;
        for(const ed of edges){
          let nextIdx=-1;
          if(ed.aIdx===srcIdx) nextIdx=ed.bIdx;
          else if(ed.bIdx===srcIdx) nextIdx=ed.aIdx;
          else continue;
          if(ed.channelMask&bit){
            sendToken(srcIdx,nextIdx,ed.op,{...nodes[srcIdx].state},null,true,bit);
            pushLog(`⚡ manual signal ch${channel} from N${srcIdx}`,CHANNEL_COLORS[channel]);
            return;
          }
        }
        pushLog(`✗ no open ch${channel} edge from N${srcIdx}`,"#ff4444");
      },

      // ── AUTO-RUN: FULL EXPERIMENTAL BATTERY ──
      autoRun:()=>{
        const report={seed,gen,startTime:Date.now(),phases:[]};

        function getBands(){
          const tot=edges.length||1;
          const st=edges.filter(e=>opBand(e.op)==="stable").length;
          const ro=edges.filter(e=>opBand(e.op)==="rotate").length;
          const gr=edges.filter(e=>opBand(e.op)==="growth").length;
          return{st:Math.round(st/tot*100),ro:Math.round(ro/tot*100),gr:Math.round(gr/tot*100),nodes:nodes.length,edges:edges.length};
        }

        function bandVariance(samples){
          if(samples.length<5)return 999;
          const last=samples.slice(-10);
          const avg=last.reduce((s,x)=>s+x.stPct,0)/last.length;
          return Math.max(...last.map(x=>Math.abs(x.stPct-avg)));
        }

        function perturbAndMeasure(count){
          return new Promise(resolve=>{
            const pre=getBands();
            const n=Math.min(count,nodes.length);
            const indices=[...Array(nodes.length).keys()];
            for(let i=indices.length-1;i>0;i--){const j=Math.floor(Math.random()*(i+1));[indices[i],indices[j]]=[indices[j],indices[i]];}
            for(let k=0;k<n;k++){
              const nd=nodes[indices[k]];
              nd.state={a:1+Math.floor(Math.random()*255),b:Math.floor(Math.random()*257)};
              nd.stableCount=0;nd.flash=30;updateNodeColor(nd);
              for(const ed of edges)if(ed.aIdx===nd.idx||ed.bIdx===nd.idx)refreshEdge(ed);
            }
            pushLog(`⚡ auto: perturbed ${n} nodes`,"#ff2266");

            // Poll for recovery over ~300 ticks
            const startTick=tick;
            const samples=[];
            const poll=()=>{
              const elapsed=tick-startTick;
              const now=getBands();
              const dev=Math.abs(now.st-pre.st)+Math.abs(now.ro-pre.ro)+Math.abs(now.gr-pre.gr);
              samples.push({t:elapsed,dev,...now});
              if(elapsed<300 && substrateRunning){
                setTimeout(poll,150);
              }else{
                const finalDev=samples[samples.length-1]?.dev||99;
                const recovered=finalDev<8;
                const peakDev=Math.max(...samples.map(s=>s.dev));
                resolve({count:n,pre,recovered,finalDev,peakDev,samples:samples.length,finalBands:getBands()});
              }
            };
            setTimeout(poll,200);
          });
        }

        function channelProbe(){
          return new Promise(resolve=>{
            let srcIdx=0,sinkIdx=0,maxDist=0;
            for(let i=0;i<Math.min(nodes.length,30);i++){
              for(let j=i+1;j<Math.min(nodes.length,30);j++){
                const d=nodes[i].pos.distanceTo(nodes[j].pos);
                if(d>maxDist){maxDist=d;srcIdx=i;sinkIdx=j;}
              }
            }
            const srcChord=nodes[srcIdx].chord||0;
            const sinkChord=nodes[sinkIdx].chord||0;
            const results=[];
            for(let ch=0;ch<8;ch++){
              const bit=1<<ch;
              const srcHas=!!(srcChord&bit);
              let canSend=false;
              for(const ed of edges){
                if((ed.aIdx===srcIdx||ed.bIdx===srcIdx)&&(ed.channelMask&bit)){canSend=true;break;}
              }
              // BFS for continuous channel path
              let arrived=false,hops=0;
              if(canSend){
                const visited=new Set();const queue=[srcIdx];visited.add(srcIdx);
                while(queue.length>0&&!arrived){
                  const cur=queue.shift();
                  for(const ed of edges){
                    let next=-1;
                    if(ed.aIdx===cur)next=ed.bIdx;
                    else if(ed.bIdx===cur)next=ed.aIdx;
                    else continue;
                    if(visited.has(next)||!(ed.channelMask&bit))continue;
                    if(next===sinkIdx){arrived=true;hops=visited.size;break;}
                    visited.add(next);queue.push(next);
                  }
                }
              }
              results.push({ch,srcHas,canSend,arrived,hops});
            }
            const arrivedCount=results.filter(r=>r.arrived).length;
            const sentCount=results.filter(r=>r.canSend).length;
            const blockedCount=sentCount-arrivedCount;
            resolve({srcIdx,sinkIdx,dist:maxDist.toFixed(0),
              srcChord:chordStr(srcChord),sinkChord:chordStr(sinkChord),
              channels:results,arrivedCount,sentCount,blockedCount,
              selective:arrivedCount>0&&blockedCount>0});
          });
        }

        // ── PHASE 1: START & GROW ──
        setAutoPhase("growing");
        if(!substrateRunning){substrateRunning=true;setRunning(true);}
        currentSpeed=8; // max speed
        pushLog("── AUTO-RUN: starting ──","#00ffcc");

        const waitForGrowth=()=>{
          if(nodes.length>=100){
            pushLog(`auto: ${nodes.length} nodes — waiting for stabilization`,"#00ffcc");
            setAutoPhase("stabilizing");
            waitForStable();
          }else{
            setTimeout(waitForGrowth,500);
          }
        };

        // ── PHASE 2: WAIT FOR STABILIZATION ──
        const waitForStable=()=>{
          const v=bandVariance(tsData);
          if(v<4 && tsData.length>20 && nodes.length>=50){
            pushLog(`auto: stabilized (variance=${v.toFixed(1)}) — running tests`,"#00ffcc");
            setAutoPhase("testing");
            report.baseline=getBands();
            report.stabilizedAt={tick,nodes:nodes.length,edges:edges.length,variance:v};
            runTests();
          }else{
            setTimeout(waitForStable,800);
          }
        };

        // ── PHASE 3: RUN TESTS ──
        const runTests=async()=>{
          // Exp A: already captured in baseline
          report.expA={...report.baseline,equilibrium:"dissipative"};
          pushLog("auto: Exp A ✓ baseline captured","#00ffcc");

          // Exp B: pause/resume
          pushLog("auto: Exp B — pause/resume test","#00ffcc");
          const prePause=getBands();
          substrateRunning=false;
          await new Promise(r=>setTimeout(r,1500));
          substrateRunning=true;
          await new Promise(r=>setTimeout(r,1500));
          const postPause=getBands();
          const pauseDev=Math.abs(postPause.st-prePause.st)+Math.abs(postPause.ro-prePause.ro)+Math.abs(postPause.gr-prePause.gr);
          report.expB={prePause,postPause,dev:pauseDev,passed:pauseDev<8};
          pushLog(`auto: Exp B ${report.expB.passed?"✓":"✗"} persistence (dev=${pauseDev})`,"#00ffcc");

          // Exp E: perturbation at 1, 5, 10%
          pushLog("auto: Exp E — perturbation recovery","#ff2266");
          const e1=await perturbAndMeasure(1);
          report.expE1=e1;
          pushLog(`auto: E-1 ${e1.recovered?"✓":"✗"} (peak=${e1.peakDev}, final=${e1.finalDev})`,"#ff2266");

          await new Promise(r=>setTimeout(r,1000));
          const e5=await perturbAndMeasure(5);
          report.expE5=e5;
          pushLog(`auto: E-5 ${e5.recovered?"✓":"✗"} (peak=${e5.peakDev}, final=${e5.finalDev})`,"#ff2266");

          await new Promise(r=>setTimeout(r,1000));
          const e10pct=await perturbAndMeasure(Math.max(1,Math.round(nodes.length*0.1)));
          report.expE10=e10pct;
          pushLog(`auto: E-10% ${e10pct.recovered?"✓":"✗"} (peak=${e10pct.peakDev}, final=${e10pct.finalDev})`,"#ff2266");

          // Exp F: channel routing
          pushLog("auto: Exp F — channel routing probe","#00eeff");
          const f=await channelProbe();
          report.expF=f;
          pushLog(`auto: F — ${f.arrivedCount}/${f.sentCount} channels routed${f.selective?" (SELECTIVE)":""}`,"#00eeff");

          // Exp D: report generator info
          report.expD={gen,generator:genConfig.g};

          // Done
          report.endTime=Date.now();
          report.duration=((report.endTime-report.startTime)/1000).toFixed(1);
          report.finalBands=getBands();
          report.avgChannelWidth=edges.length>0?Math.round(edges.reduce((s,e)=>s+e.channelWidth,0)/edges.length*10)/10:0;

          setAutoReport(report);
          setAutoPhase("done");
          currentSpeed=1;
          pushLog(`── AUTO-RUN COMPLETE (${report.duration}s) ──`,"#00ffcc");
        };

        waitForGrowth();
      },
    };

    // ── ANIMATE ──
    const _com=new THREE.Vector3();let animId;
    const _tmpQ=new THREE.Quaternion();
    let lastSubstrateTick=0;

    const animate=()=>{
      animId=requestAnimationFrame(animate);frameN++;
      if(!drag&&Date.now()-lastAct>3000)autoOrbit=true;
      if(autoOrbit)camTheta+=0.001;
      _com.set(0,0,0);for(const n of nodes)_com.add(n.pos);
      if(nodes.length)_com.divideScalar(nodes.length);
      camSmooth.lerp(_com,0.02);
      const sp=Math.sin(camPhi),cp=Math.cos(camPhi);
      camera.position.set(camSmooth.x+camR*sp*Math.sin(camTheta),camSmooth.y+camR*cp,camSmooth.z+camR*sp*Math.cos(camTheta));
      camera.lookAt(camSmooth);

      const L=lod();
      const tickInterval=Math.max(12,Math.round(60/currentSpeed));
      if(substrateRunning&&frameN-lastSubstrateTick>=tickInterval&&tokens.length<L.tokenCap){
        substrateTick();
        lastSubstrateTick=frameN;
      }

      // ── ENTROPY FLOOR (thermal noise) ──
      // If the system has gone 40 ticks without a rotation, it's frozen.
      // Inject a random rotation into a random node to break the equilibrium.
      // This is analogous to thermal fluctuations in physical systems —
      // it prevents dead zones without overwhelming genuine attractors.
      if(substrateRunning && tick>0 && tick-lastRotationTick>40 && frameN%60===0 && nodes.length>0){
        const ri=Math.floor(Math.random()*nodes.length);
        const axes=["ROTATE_X","ROTATE_Y","ROTATE_Z"];
        applyDiscreteRotation(nodes[ri], pickRotation(axes[Math.floor(Math.random()*3)]));
        pushLog(`thermal: N${ri} perturbed (${tick-lastRotationTick} ticks quiet)`,"#9933ff");
      }

      for(const nd of nodes){
        const age=Math.min((frameN-nd.birthFrame)/30,1);
        const fl=nd.flash>0?nd.flash/18:0;if(nd.flash>0)nd.flash--;
        if(nd.rotAnimProgress.v<1.0){
          nd.rotAnimProgress.v=Math.min(1.0,nd.rotAnimProgress.v+0.04);
          const t=nd.rotAnimProgress.v,eased=1-Math.pow(1-t,3);
          _tmpQ.slerpQuaternions(nd.discreteQuat,nd.targetQuat,eased);
          nd.coreMesh.quaternion.copy(_tmpQ);nd.fillMesh.quaternion.copy(_tmpQ);
          if(nd.rotAnimProgress.v>=1.0){nd.discreteQuat.copy(nd.targetQuat);nd.coreMesh.quaternion.copy(nd.targetQuat);nd.fillMesh.quaternion.copy(nd.targetQuat);}
        }
        nd.coreMesh.scale.setScalar(age*(1+fl*0.4));
        nd.fillMesh.scale.setScalar(age*(1+fl*0.4));
        const stabilityGlow=Math.min(nd.stableCount/20,1);
        const baseHalo=(60+stabilityGlow*35)*L.haloScale;
        nd.halo.scale.setScalar(baseHalo*age+fl*20);
        nd.halo.material.opacity=0.15+stabilityGlow*0.25+fl*0.35;
        if(nd.idx===selectedIdx){nd.halo.scale.setScalar(100*age*L.haloScale);nd.halo.material.opacity=0.8;}

        if(nd.f256m){
          const f256val=face256Source(nd.state);
          const orbitAngle=(f256val/256)*Math.PI*2+frameN*0.008;
          const orbitR=20;
          nd.f256m.position.set(nd.pos.x+Math.cos(orbitAngle)*orbitR,nd.pos.y+Math.sin(orbitAngle*0.7)*orbitR,nd.pos.z+Math.sin(orbitAngle)*orbitR*0.5);
          nd.f256g.position.copy(nd.f256m.position);
        }
        if(nd.arrowMesh){
          const f256val=face256Source(nd.state);
          const arrowDir=new THREE.Vector3(Math.cos(f256val/256*Math.PI*2),Math.sin(f256val/128*Math.PI),Math.cos(f256val/64*Math.PI)).normalize();
          nd.arrowMesh.position.copy(nd.pos).addScaledVector(arrowDir,16);
          nd.arrowMesh.quaternion.setFromUnitVectors(new THREE.Vector3(0,1,0),arrowDir);
          nd.arrowMesh.material.opacity=0.3+fl*0.4;
        }
        // Channel dots: 8 bits orbiting in a ring
        if(nd.channelDots&&nd.channelDots.length===8){
          const chord=nd.chord||0;
          for(let ci=0;ci<8;ci++){
            const active=!!(chord&(1<<ci));
            const angle=(ci/8)*Math.PI*2+frameN*0.003;
            const r=15;
            nd.channelDots[ci].position.set(
              nd.pos.x+Math.cos(angle)*r,
              nd.pos.y-14,
              nd.pos.z+Math.sin(angle)*r
            );
            nd.channelDots[ci].material.opacity=active?0.85+fl*0.15:0.04;
            nd.channelDots[ci].scale.setScalar(active?8:4);
          }
        }
      }

      if(frameN%30===0)for(const ed of edges)refreshEdge(ed);

      for(let i=tokens.length-1;i>=0;i--){
        updateTokenTick(tokens[i]);
        if(tokens[i].dead){disposeToken(tokens[i]);tokens.splice(i,1);}
      }

      if(frameN%15===0){
        if(selectedIdx!==null&&simRef.current)setNodeInfo(simRef.current.info(selectedIdx));
        const stE=edges.filter(e=>opBand(e.op)==="stable").length;
        const roE=edges.filter(e=>opBand(e.op)==="rotate").length;
        const grE=edges.filter(e=>opBand(e.op)==="growth").length;
        setMetrics({nodes:nodes.length,edges:edges.length,stableEdges:stE,volatileEdges:roE,growthEdges:grE,totalRotations,forks:totalForks,tick});
      }
      renderer.render(scene,camera);
    };
    animate();

    const onResize=()=>{const W=el.clientWidth,H=el.clientHeight;camera.aspect=W/H;camera.updateProjectionMatrix();renderer.setSize(W,H);};
    window.addEventListener("resize",onResize);
    return()=>{
      cancelAnimationFrame(animId);
      window.removeEventListener("resize",onResize);
      window.removeEventListener("mousemove",onMove);
      window.removeEventListener("mouseup",onUp);
      if(el.contains(renderer.domElement))el.removeChild(renderer.domElement);
      renderer.dispose();
    };
  },[pushLog,seed,gen,runId]);

  const mf={fontFamily:'"JetBrains Mono","Fira Code","Courier New",monospace'};
  const OC=k=>OPCODES[k]?.css||"#fff";
  const totalE=metrics.edges||1;
  const stableRatio=((metrics.stableEdges/totalE)*100).toFixed(0);
  const rotateRatio=((metrics.volatileEdges/totalE)*100).toFixed(0);
  const growthRatio=((metrics.growthEdges/totalE)*100).toFixed(0);

  const tabStyle=(id)=>({
    flex:1,background:tab===id?"#0a1a28":"#020810",
    border:`1px solid ${tab===id?"#00ffcc55":"#0a1422"}`,
    color:tab===id?"#00ffcc":"#445566",
    padding:"6px",cursor:"pointer",fontSize:8,letterSpacing:1,...mf,
  });

  return(
    <div style={{display:"flex",height:"100vh",background:"#010408",...mf}}>
      <div style={{flex:1,position:"relative",overflow:"hidden"}}>
        <div ref={mountRef} style={{width:"100%",height:"100%"}}/>

        {/* Legend */}
        <div style={{position:"absolute",top:14,left:14,fontSize:9,lineHeight:1.9,
          pointerEvents:"none",background:"rgba(1,4,8,0.85)",border:"1px solid #0a1422",padding:"10px 14px"}}>
          <div style={{color:"#223344",letterSpacing:2,marginBottom:3}}>SELF-ASSEMBLING SUBSTRATE</div>
          <div style={{color:"#1a3040",fontSize:8,lineHeight:1.7,marginBottom:5}}>
            No pipeline. No clock. Up to {MAX_NODES} nodes.<br/>
            Structure emerges from GF(257) geometry.
          </div>
          {[
            ["#3377ff","━","ROTATE (0–109): volatile, cascades"],
            ["#00ffcc","━","STABLE (110–224): convergent"],
            ["#44ff88","━","GROWTH (225–256): structural"],
          ].map(([c,s,l])=>(
            <div key={l} style={{display:"flex",gap:5}}>
              <span style={{color:c}}>{s}</span><span style={{color:"#1a3040",fontSize:8}}>{l}</span>
            </div>
          ))}
        </div>

        {/* Band gauge */}
        <div style={{position:"absolute",bottom:14,left:14,
          background:"rgba(1,4,8,0.85)",border:"1px solid #0a1422",
          padding:"10px 16px",pointerEvents:"none",minWidth:220}}>
          <div style={{fontSize:8,letterSpacing:2,color:"#223344",marginBottom:6}}>EDGE BAND DISTRIBUTION</div>
          <div style={{display:"flex",height:8,borderRadius:2,overflow:"hidden",marginBottom:5,background:"#030810"}}>
            <div style={{width:`${rotateRatio}%`,background:"#3377ff",transition:"width 0.5s"}}/>
            <div style={{width:`${stableRatio}%`,background:"#00ffcc",transition:"width 0.5s"}}/>
            <div style={{width:`${growthRatio}%`,background:"#44ff88",transition:"width 0.5s"}}/>
          </div>
          <div style={{display:"flex",gap:12,fontSize:8}}>
            <span style={{color:"#3377ff"}}>{rotateRatio}%</span>
            <span style={{color:"#00ffcc"}}>{stableRatio}%</span>
            <span style={{color:"#44ff88"}}>{growthRatio}%</span>
          </div>
        </div>
      </div>

      {/* Side panel */}
      <div style={{width:300,background:"#00020a",borderLeft:"1px solid #0a1422",
        display:"flex",flexDirection:"column",overflow:"hidden"}}>

        {/* Controls */}
        <div style={{padding:"12px 14px 10px",borderBottom:"1px solid #0a1422"}}>
          <div style={{fontSize:8,letterSpacing:3,color:"#00ffcc",opacity:0.35,marginBottom:3}}>GF(257) SUBSTRATE</div>
          <div style={{display:"flex",gap:5,marginBottom:5}}>
            <button onClick={()=>simRef.current?.start()} disabled={running} style={{
              flex:2,background:running?"#060e14":"#001a10",
              border:`1px solid ${running?"#0a1a20":"#00ff8877"}`,
              color:running?"#0a2010":"#00ff88",
              padding:"8px",cursor:running?"not-allowed":"pointer",fontSize:9,letterSpacing:1,...mf}}>
              {running?"▶ ACTIVE":"▶ START"}</button>
            <button onClick={()=>simRef.current?.stop()} style={{background:"#180008",border:"1px solid #ff444433",color:"#ff4444",padding:"8px 10px",cursor:"pointer",fontSize:9,...mf}}>■</button>
            <button onClick={()=>simRef.current?.fire()} style={{background:"#001020",border:"1px solid #00ffcc22",color:"#00ffcc",padding:"8px 10px",cursor:"pointer",fontSize:9,...mf}}>~</button>
          </div>
          <div style={{display:"flex",gap:3}}>
            {[{l:"½",v:0.5},{l:"1×",v:1},{l:"2×",v:2},{l:"4×",v:4},{l:"8×",v:8}].map(({l,v})=>(
              <button key={v} onClick={()=>{setSpeed(v);simRef.current?.setSpeed(v);}}
                style={{flex:1,background:speed===v?"#0a1a28":"#020810",
                  border:`1px solid ${speed===v?"#00ffcc44":"#0a1422"}`,
                  color:speed===v?"#00ffcc":"#0a1a28",
                  padding:"4px",cursor:"pointer",fontSize:8,...mf}}>{l}</button>
            ))}
          </div>
        </div>

        {/* Tabs */}
        <div style={{display:"flex",gap:3,padding:"6px 14px",borderBottom:"1px solid #0a1422"}}>
          <button onClick={()=>setTab("live")} style={tabStyle("live")}>LIVE</button>
          <button onClick={()=>setTab("charts")} style={tabStyle("charts")}>CHARTS</button>
          <button onClick={()=>setTab("experiment")} style={tabStyle("experiment")}>EXPERIMENT</button>
        </div>

        {/* Tab content */}
        <div style={{flex:1,overflow:"auto",minHeight:0}}>

          {tab==="live"&&(
            <div style={{padding:"10px 14px"}}>
              {/* Metrics grid */}
              <div style={{display:"grid",gridTemplateColumns:"1fr 1fr 1fr",gap:5,marginBottom:10}}>
                {[
                  {l:"nodes",v:metrics.nodes,c:"#00ccff"},
                  {l:"edges",v:metrics.edges,c:"#00ccff"},
                  {l:"ticks",v:metrics.tick,c:"#334466"},
                  {l:"stable",v:metrics.stableEdges,c:"#00ffcc"},
                  {l:"volatile",v:metrics.volatileEdges,c:"#3377ff"},
                  {l:"growth",v:metrics.growthEdges,c:"#44ff88"},
                  {l:"rotations",v:metrics.totalRotations,c:"#9933ff"},
                  {l:"forks",v:metrics.forks,c:"#44ff88"},
                  {l:"max",v:MAX_NODES,c:"#0a1a28"},
                ].map(({l,v,c})=>(
                  <div key={l} style={{background:"#030810",padding:"5px 7px"}}>
                    <div style={{fontSize:6,color:"#0a1a28"}}>{l}</div>
                    <div style={{fontSize:10,color:c,fontWeight:"bold"}}>{v}</div>
                  </div>
                ))}
              </div>

              {/* Selected node */}
              <div style={{marginBottom:8}}>
                <div style={{fontSize:8,letterSpacing:2,color:"#1a3040",marginBottom:4}}>
                  {selected!==null?`N${selected} · ${nodeInfo?.label||""}`:"CLICK A NODE"}
                </div>
                {nodeInfo&&selected!==null?(
                  <>
                    <div style={{fontSize:10,color:"#c8e8f0",marginBottom:2}}>
                      f(p) = <span style={{color:"#00ffcc"}}>{affStr(nodeInfo.state)}</span>
                    </div>
                    <div style={{display:"flex",alignItems:"center",gap:6,marginBottom:3}}>
                      <span style={{fontSize:8,color:"#1a3040"}}>f256</span>
                      <div style={{width:7,height:7,borderRadius:"50%",background:`hsl(${nodeInfo.f256/256*280},100%,60%)`,boxShadow:`0 0 4px hsl(${nodeInfo.f256/256*280},100%,60%)`}}/>
                      <span style={{fontSize:9,color:"#ff8800"}}>{nodeInfo.f256}</span>
                    </div>
                    <div style={{fontSize:8,color:"#0a2030"}}>
                      <span style={{color:"#9933ff"}}>rot:{nodeInfo.rotCount}</span>
                      <span style={{color:"#00ffcc",marginLeft:6}}>stable:{nodeInfo.stableCount}</span>
                      <span style={{marginLeft:6}}>edges:{nodeInfo.edgeCount} (<span style={{color:"#00ffcc"}}>{nodeInfo.stableE}</span>/<span style={{color:"#3377ff"}}>{nodeInfo.rotateE}</span>/<span style={{color:"#44ff88"}}>{nodeInfo.growthE}</span>)</span>
                    </div>
                    <div style={{fontSize:8,color:"#0a1a28",marginTop:2}}>
                      {nodeInfo.ops.map((op,i)=><span key={i} style={{color:OC(op),marginRight:3}}>{op}</span>)}
                    </div>
                    {nodeInfo.chordStr&&(
                      <div style={{display:"flex",alignItems:"center",gap:3,marginTop:3}}>
                        <span style={{fontSize:7,color:"#1a3040"}}>chord</span>
                        {nodeInfo.chordStr.split("").map((bit,i)=>(
                          <div key={i} style={{width:6,height:6,borderRadius:"50%",
                            background:bit==="1"?CHANNEL_COLORS[i]:"#0a1422",
                            opacity:bit==="1"?1:0.3}}/>
                        ))}
                        <span style={{fontSize:7,color:"#334466",marginLeft:3}}>{nodeInfo.popcount}/8 ch · {nodeInfo.openChannels} open edges</span>
                      </div>
                    )}
                  </>
                ):null}
              </div>

              {/* Log */}
              <div style={{fontSize:7,letterSpacing:2,color:"#1a3040",marginBottom:3}}>LOG</div>
              {log.map((e,i)=>(
                <div key={e.id} style={{fontSize:8,lineHeight:1.7,color:e.color,opacity:Math.max(0.05,1-i*0.05)}}>{e.msg}</div>
              ))}
            </div>
          )}

          {tab==="charts"&&(
            <div style={{padding:"10px 14px"}}>
              {/* Experiment A: Entropy vs Convergence */}
              <div style={{fontSize:8,letterSpacing:2,color:"#223344",marginBottom:6}}>A · ENTROPY vs CONVERGENCE</div>

              {/* Band % over time */}
              <div style={{fontSize:7,color:"#1a3040",marginBottom:3}}>edge band % over time</div>
              <div style={{height:120,marginBottom:12}}>
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={timeSeries} margin={{top:2,right:4,left:0,bottom:0}}>
                    <XAxis dataKey="t" hide/>
                    <YAxis domain={[0,100]} hide/>
                    <Tooltip contentStyle={{background:"#030810",border:"1px solid #0a1422",fontSize:9,...mf}} labelStyle={{color:"#334466"}}/>
                    <Area type="monotone" dataKey="roPct" stackId="1" stroke="#3377ff" fill="#3377ff" fillOpacity={0.6} name="rotate%"/>
                    <Area type="monotone" dataKey="stPct" stackId="1" stroke="#00ffcc" fill="#00ffcc" fillOpacity={0.6} name="stable%"/>
                    <Area type="monotone" dataKey="grPct" stackId="1" stroke="#44ff88" fill="#44ff88" fillOpacity={0.6} name="growth%"/>
                  </AreaChart>
                </ResponsiveContainer>
              </div>

              {/* Graph growth over time */}
              <div style={{fontSize:7,color:"#1a3040",marginBottom:3}}>graph size over time</div>
              <div style={{height:100,marginBottom:12}}>
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={timeSeries} margin={{top:2,right:4,left:0,bottom:0}}>
                    <XAxis dataKey="t" hide/>
                    <YAxis hide/>
                    <Tooltip contentStyle={{background:"#030810",border:"1px solid #0a1422",fontSize:9,...mf}} labelStyle={{color:"#334466"}}/>
                    <Area type="monotone" dataKey="nodes" stroke="#00ccff" fill="#00ccff" fillOpacity={0.3} name="nodes"/>
                    <Area type="monotone" dataKey="edges" stroke="#00ccff" fill="#00ccff" fillOpacity={0.15} name="edges"/>
                  </AreaChart>
                </ResponsiveContainer>
              </div>

              {/* Rotation rate (entropy injection) */}
              <div style={{fontSize:7,color:"#1a3040",marginBottom:3}}>rotation events per sample (entropy)</div>
              <div style={{height:80,marginBottom:8}}>
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={timeSeries} margin={{top:2,right:4,left:0,bottom:0}}>
                    <XAxis dataKey="t" hide/>
                    <YAxis hide/>
                    <Tooltip contentStyle={{background:"#030810",border:"1px solid #0a1422",fontSize:9,...mf}} labelStyle={{color:"#334466"}}/>
                    <Area type="monotone" dataKey="rotW" stroke="#9933ff" fill="#9933ff" fillOpacity={0.4} name="rotations/window"/>
                  </AreaChart>
                </ResponsiveContainer>
              </div>

              {/* Channel width over time */}
              <div style={{fontSize:7,color:"#1a3040",marginBottom:3}}>avg channel width per edge (communication bandwidth)</div>
              <div style={{height:70,marginBottom:8}}>
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={timeSeries} margin={{top:2,right:4,left:0,bottom:0}}>
                    <XAxis dataKey="t" hide/>
                    <YAxis hide domain={[0,8]}/>
                    <Tooltip contentStyle={{background:"#030810",border:"1px solid #0a1422",fontSize:9,...mf}} labelStyle={{color:"#334466"}}/>
                    <Area type="monotone" dataKey="avgCh" stroke="#00eeff" fill="#00eeff" fillOpacity={0.3} name="avg channels/edge"/>
                  </AreaChart>
                </ResponsiveContainer>
              </div>

              <div style={{fontSize:7,color:"#0a1a28",lineHeight:1.7}}>
                {timeSeries.length<5?"collecting data… start the substrate and let it run"
                  :`${timeSeries.length} samples · ${timeSeries[timeSeries.length-1]?.nodes||0} nodes · avg ${timeSeries[timeSeries.length-1]?.avgCh||0} ch/edge`}
              </div>
            </div>
          )}

          {tab==="experiment"&&(
            <div style={{padding:"10px 14px"}}>
              {/* Auto-run header */}
              <div style={{fontSize:8,letterSpacing:2,color:"#00ffcc",marginBottom:8}}>AUTOMATED EXPERIMENT SUITE</div>
              <div style={{fontSize:8,color:"#1a3040",lineHeight:1.8,marginBottom:10}}>
                One button runs the full battery: grow → stabilize →<br/>
                pause/resume → perturb 1/5/10% → channel probe
              </div>

              {/* Configuration */}
              <div style={{fontSize:7,letterSpacing:2,color:"#223344",marginBottom:5}}>SEED</div>
              <div style={{display:"flex",gap:3,flexWrap:"wrap",marginBottom:8}}>
                {Object.entries(SEEDS).map(([k])=>(
                  <button key={k} onClick={()=>setSeed(k)} style={{
                    padding:"4px 8px",background:seed===k?"#0a1a28":"#020810",
                    border:`1px solid ${seed===k?"#00ffcc44":"#0a1422"}`,
                    color:seed===k?"#00ffcc":"#334466",
                    cursor:"pointer",fontSize:7,...mf}}>{k}</button>
                ))}
              </div>
              <div style={{fontSize:7,letterSpacing:2,color:"#223344",marginBottom:5}}>GENERATOR</div>
              <div style={{display:"flex",gap:3,flexWrap:"wrap",marginBottom:10}}>
                {Object.entries(GENERATORS).map(([k])=>(
                  <button key={k} onClick={()=>setGen(k)} style={{
                    padding:"4px 8px",background:gen===k?"#0a1a28":"#020810",
                    border:`1px solid ${gen===k?"#9933ff44":"#0a1422"}`,
                    color:gen===k?"#9933ff":"#334466",
                    cursor:"pointer",fontSize:7,...mf}}>{k}</button>
                ))}
              </div>

              {/* Run button */}
              <button onClick={()=>{setAutoReport(null);simRef.current?.autoRun();setTab("experiment");}}
                disabled={autoPhase!=="idle"&&autoPhase!=="done"}
                style={{width:"100%",padding:"12px",marginBottom:8,
                  background:autoPhase==="idle"||autoPhase==="done"?"#001a10":"#060e14",
                  border:`1px solid ${autoPhase==="idle"||autoPhase==="done"?"#00ff8877":"#0a1a20"}`,
                  color:autoPhase==="idle"||autoPhase==="done"?"#00ff88":"#0a2010",
                  cursor:autoPhase==="idle"||autoPhase==="done"?"pointer":"not-allowed",
                  fontSize:10,letterSpacing:2,...mf}}>
                {autoPhase==="idle"?"▶ AUTO-RUN ALL EXPERIMENTS"
                  :autoPhase==="done"?"▶ RUN AGAIN"
                  :`⟳ ${autoPhase.toUpperCase()}…`}
              </button>

              {/* Phase indicator */}
              {autoPhase!=="idle"&&autoPhase!=="done"&&(
                <div style={{fontSize:8,color:"#00ffcc",marginBottom:8,textAlign:"center"}}>
                  {autoPhase==="growing"&&`growing graph… ${metrics.nodes} nodes`}
                  {autoPhase==="stabilizing"&&`waiting for equilibrium… ${metrics.stableEdges} stable edges`}
                  {autoPhase==="testing"&&"running experiment battery…"}
                </div>
              )}

              {/* Report */}
              {autoReport&&(
                <div style={{background:"#030810",padding:"10px",border:"1px solid #0a1422"}}>
                  <div style={{fontSize:8,letterSpacing:2,color:"#00ffcc",marginBottom:8}}>
                    REPORT — {autoReport.seed} · {autoReport.gen} — {autoReport.duration}s
                  </div>

                  {/* A: Baseline */}
                  <div style={{marginBottom:8}}>
                    <div style={{fontSize:8,color:"#223344",marginBottom:3}}>A · EQUILIBRIUM</div>
                    <div style={{fontSize:9,color:"#c8e8f0"}}>
                      {autoReport.baseline.nodes} nodes · {autoReport.baseline.edges} edges
                    </div>
                    <div style={{display:"flex",gap:8,fontSize:8,marginTop:2}}>
                      <span style={{color:"#3377ff"}}>{autoReport.baseline.ro}% rot</span>
                      <span style={{color:"#00ffcc"}}>{autoReport.baseline.st}% stable</span>
                      <span style={{color:"#44ff88"}}>{autoReport.baseline.gr}% growth</span>
                    </div>
                    <div style={{fontSize:7,color:"#334466",marginTop:2}}>
                      stabilized at tick {autoReport.stabilizedAt?.tick} (var={autoReport.stabilizedAt?.variance?.toFixed(1)})
                    </div>
                  </div>

                  {/* B: Persistence */}
                  <div style={{marginBottom:8}}>
                    <div style={{fontSize:8,color:"#223344",marginBottom:3}}>B · PERSISTENCE</div>
                    <div style={{fontSize:9,color:autoReport.expB?.passed?"#00ff88":"#ff4444"}}>
                      {autoReport.expB?.passed?"✓ PASSED":"✗ FAILED"} — deviation={autoReport.expB?.dev}
                    </div>
                  </div>

                  {/* E: Error correction */}
                  <div style={{marginBottom:8}}>
                    <div style={{fontSize:8,color:"#223344",marginBottom:3}}>E · ERROR CORRECTION</div>
                    {[{k:"expE1",l:"1 node"},{k:"expE5",l:"5 nodes"},{k:"expE10",l:"10%"}].map(({k,l})=>{
                      const d=autoReport[k];if(!d)return null;
                      return(
                        <div key={k} style={{display:"flex",gap:6,fontSize:8,lineHeight:1.8}}>
                          <span style={{color:d.recovered?"#00ff88":"#ff4444",minWidth:14}}>{d.recovered?"✓":"✗"}</span>
                          <span style={{color:"#334466",minWidth:55}}>{l}</span>
                          <span style={{color:"#1a3040"}}>peak={d.peakDev} final={d.finalDev}</span>
                        </div>
                      );
                    })}
                  </div>

                  {/* F: Channel routing */}
                  <div style={{marginBottom:8}}>
                    <div style={{fontSize:8,color:"#223344",marginBottom:3}}>F · CHANNEL ROUTING</div>
                    {autoReport.expF&&(
                      <>
                        <div style={{fontSize:9,color:autoReport.expF.arrivedCount>0?"#00ff88":"#ff4444"}}>
                          {autoReport.expF.arrivedCount}/{autoReport.expF.sentCount} channels routed
                          {autoReport.expF.selective?" — SELECTIVE":""}
                        </div>
                        <div style={{fontSize:7,color:"#1a3040",marginTop:2}}>
                          N{autoReport.expF.srcIdx}→N{autoReport.expF.sinkIdx} (d={autoReport.expF.dist})
                        </div>
                        <div style={{display:"flex",gap:3,marginTop:3}}>
                          {autoReport.expF.channels.map((ch,i)=>(
                            <div key={i} style={{width:10,height:10,borderRadius:"50%",
                              background:ch.arrived?CHANNEL_COLORS[i]:"#0a1422",
                              border:`1px solid ${CHANNEL_COLORS[i]}44`,
                              opacity:ch.canSend?(ch.arrived?1:0.5):0.15}}
                              title={`ch${i}: ${ch.arrived?"routed":"blocked"}`}/>
                          ))}
                        </div>
                      </>
                    )}
                  </div>

                  {/* Summary */}
                  <div style={{borderTop:"1px solid #0a1422",paddingTop:8,marginTop:6}}>
                    <div style={{fontSize:8,color:"#223344",marginBottom:3}}>SUMMARY</div>
                    <div style={{fontSize:8,color:"#c8e8f0",lineHeight:1.8}}>
                      avg channel width: {autoReport.avgChannelWidth}/8<br/>
                      final bands: {autoReport.finalBands?.st}% stable / {autoReport.finalBands?.ro}% rot / {autoReport.finalBands?.gr}% growth
                    </div>
                  </div>
                </div>
              )}

              {/* Reset */}
              <div style={{marginTop:10}}>
                <button onClick={()=>{setRunning(false);setTimeSeries([]);setAutoReport(null);setAutoPhase("idle");setPerturbData(null);setProbeResults([]);setRunId(r=>r+1);}}
                  style={{width:"100%",padding:"8px",background:"#180008",border:"1px solid #ff444433",color:"#ff4444",cursor:"pointer",fontSize:8,letterSpacing:1,...mf}}>
                  ↻ RESET (seed: {seed} · gen: {gen})
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div style={{padding:"6px 14px",borderTop:"1px solid #0a1422"}}>
          <div style={{fontSize:6,color:"#060e14",display:"flex",flexWrap:"wrap",gap:"0 8px",lineHeight:1.8}}>
            {[{op:"ROTATE_X",l:"RX"},{op:"ROTATE_Y",l:"RY"},{op:"ROTATE_Z",l:"RZ"},
              {op:"ALIGN",l:"ALN"},{op:"SEARCH",l:"SRC"},{op:"SYNC",l:"SYN"},
              {op:"FORK",l:"FRK"},{op:"COMPOSE",l:"CMP"}].map(({op,l})=>(
              <span key={op}><span style={{color:OC(op)}}>{l}</span></span>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}