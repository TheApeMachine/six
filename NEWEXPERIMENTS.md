```
import { useState, useCallback, useRef, useEffect } from "react";

const N = 257;

const rotX = Array.from({length: N}, (_, p) => (p + 1) % N);
const rotY = Array.from({length: N}, (_, p) => (3 * p) % N);
const rotZ = Array.from({length: N}, (_, p) => (3 * p + 1) % N);

function applyPerm(bits, perm) {
  const out = new Uint8Array(N);
  for (let i = 0; i < N; i++) out[perm[i]] = bits[i];
  return out;
}
function composeAffine(f, g) {
  return { a: (g.a * f.a) % N, b: (g.a * f.b + g.b) % N };
}
const IDENT = { a: 1, b: 0 };
const AFF_X = { a: 1, b: 1 };
const AFF_Y = { a: 3, b: 0 };
const AFF_Z = { a: 3, b: 1 };

function affineStr({a, b}) {
  if (a === 1 && b === 0) return "p";
  if (a === 1) return `p+${b}`;
  if (b === 0) return `${a}p`;
  return `${a}p+${b}`;
}
function stateKey({a, b}) { return `${a}:${b}`; }
function stateIndex({a, b}) { return (a - 1) * N + b; }

function charToOp(c) {
  const r = c.charCodeAt(0) % 3;
  return r === 0 ? "X" : r === 1 ? "Y" : "Z";
}
function computeState(str) {
  let aff = IDENT;
  for (const c of str) {
    const d = charToOp(c) === "X" ? AFF_X : charToOp(c) === "Y" ? AFF_Y : AFF_Z;
    aff = composeAffine(aff, d);
  }
  return aff;
}
function computeStateSteps(str) {
  let aff = IDENT;
  const steps = [{ aff: {...aff}, ch: null, op: null }];
  for (const c of str) {
    const op = charToOp(c);
    aff = composeAffine(aff, op==="X"?AFF_X:op==="Y"?AFF_Y:AFF_Z);
    steps.push({ aff: {...aff}, ch: c, op });
  }
  return steps;
}

const CORPUS = [
  "The cat sat on the mat",
  "The cat sat on the hat",
  "The cat saw the rat",
  "The dog sat on the rug",
  "The dog ran to the cat",
  "The bird sang in the tree",
  "The fish swam in the lake",
  "The sun set on the hill",
];

function buildHoloStore(corpus) {
  const store = new Map();
  for (const sentence of corpus) {
    let aff = IDENT;
    for (let i = 0; i <= sentence.length; i++) {
      const key = stateKey(aff);
      if (!store.has(key)) {
        store.set(key, { readout: sentence.slice(i), sentence, prefixLen: i, state: {...aff} });
      }
      if (i < sentence.length) {
        const op = charToOp(sentence[i]);
        aff = composeAffine(aff, op==="X"?AFF_X:op==="Y"?AFF_Y:AFF_Z);
      }
    }
  }
  return store;
}

const HOLO_STORE = buildHoloStore(CORPUS);

export default function App() {
  const [tab, setTab] = useState("holo");
  const [query, setQuery] = useState("");
  const [result, setResult] = useState(null);
  const [animSteps, setAnimSteps] = useState([]);
  const [ingestStep, setIngestStep] = useState(-1);
  const [ingesting, setIngesting] = useState(false);
  const [manualBits, setManualBits] = useState(new Uint8Array(N));
  const [manualAff, setManualAff] = useState(IDENT);
  const [manualHistory, setManualHistory] = useState([]);
  const [demoResults, setDemoResults] = useState(null);
  const rafRef = useRef(null);

  const mf = { fontFamily: '"JetBrains Mono","Fira Code","Courier New",monospace' };

  const doRecall = useCallback((q) => {
    if (!q) { setResult(null); return; }
    const qState = computeState(q);
    const key = stateKey(qState);
    setResult({ qState, key, hit: HOLO_STORE.get(key) || null });
  }, []);

  const handleQuery = useCallback(e => {
    const v = e.target.value;
    setQuery(v);
    doRecall(v);
  }, [doRecall]);

  const runIngest = useCallback(() => {
    if (ingesting) return;
    setIngesting(true);
    const steps = computeStateSteps(CORPUS[0]);
    setAnimSteps(steps);
    setIngestStep(0);
    let i = 0;
    const tick = () => {
      i++;
      setIngestStep(i);
      if (i < steps.length) rafRef.current = setTimeout(tick, 150);
      else setTimeout(() => setIngesting(false), 500);
    };
    rafRef.current = setTimeout(tick, 150);
  }, [ingesting]);

  useEffect(() => () => clearTimeout(rafRef.current), []);

  const applyOp = useCallback((op) => {
    const perm = op==="X"?rotX:op==="Y"?rotY:rotZ;
    const d    = op==="X"?AFF_X:op==="Y"?AFF_Y:AFF_Z;
    setManualBits(prev => {
      if (prev.every(b=>b===0)) {
        const b = new Uint8Array(N); b[84]=1; b[104]=1; b[101]=1;
        return applyPerm(b, perm);
      }
      return applyPerm(prev, perm);
    });
    setManualAff(prev => {
      if (prev.a===1&&prev.b===0&&manualHistory.length===0) return d;
      return composeAffine(prev, d);
    });
    setManualHistory(prev => [...prev.slice(-22), op]);
  }, [manualHistory]);

  const runNonComm = useCallback(() => {
    const pairs = [["X","Y"],["Y","X"],["X","Z"],["Z","X"],["Y","Z"],["Z","Y"]];
    setDemoResults(pairs.map(ops => {
      let a = IDENT;
      for (const op of ops) a = composeAffine(a, op==="X"?AFF_X:op==="Y"?AFF_Y:AFF_Z);
      return { ops, a };
    }));
  }, []);

  const RING_N = 64, RING_R = 92;
  const querySteps = query ? computeStateSteps(query) : [];
  const currentIngestStep = animSteps[Math.min(ingestStep, animSteps.length-1)];

  const Tab = (id, label) => (
    <button onClick={() => setTab(id)} style={{
      background:"transparent", border:"none",
      borderBottom:`2px solid ${tab===id?"#00ffcc":"transparent"}`,
      color: tab===id?"#00ffcc":"#3a6070",
      padding:"10px 16px", cursor:"pointer", fontSize:9, letterSpacing:2, ...mf,
    }}>{label}</button>
  );

  const opColor = op => op==="X"?"#4488ff":op==="Y"?"#aa44ff":"#44ff88";

  return (
    <div style={{minHeight:"100vh", background:"#070d14", color:"#c8e8f0", ...mf}}>
      <div style={{maxWidth:900, margin:"0 auto", padding:"20px 20px"}}>

        <div style={{marginBottom:18}}>
          <div style={{fontSize:9, letterSpacing:4, color:"#00ffcc", opacity:0.6, marginBottom:4}}>
            GF(257) AFFINE ROTATIONS
          </div>
          <div style={{fontSize:19, fontWeight:"bold", color:"#fff", marginBottom:4}}>
            257 bits · 65,792 rotation states · O(1) holographic recall
          </div>
          <div style={{fontSize:10, color:"#2a4a5a", lineHeight:1.8}}>
            The rotation state IS the address. Ingest stores every position's suffix at its rotation coordinate.
            Recall derives the query state once — no scan, no search, one lookup.
          </div>
        </div>

        <div style={{borderBottom:"1px solid #1a3040", marginBottom:18, display:"flex", gap:0}}>
          {Tab("holo",    "① HOLOGRAPHIC RECALL")}
          {Tab("ingest",  "② INGESTION")}
          {Tab("noncomm", "③ NON-COMMUTATIVITY")}
          {Tab("states",  "④ STATE SPACE")}
        </div>

        {/* ── TAB 1: HOLOGRAPHIC RECALL ─────────────────────────────────── */}
        {tab==="holo" && (
          <div>
            <div style={{fontSize:10, color:"#3a6070", marginBottom:14, lineHeight:1.8}}>
              The store holds <strong style={{color:"#c8e8f0"}}>{HOLO_STORE.size}</strong> rotation-state → readout
              entries from {CORPUS.length} sentences. Type any prefix — one pass computes the rotation state,
              one map lookup returns the stored suffix. No scan ever happens.
            </div>

            <div style={{display:"flex", gap:8, marginBottom:16}}>
              <input value={query} onChange={handleQuery}
                placeholder='Try: "The cat"  "The dog ran"  "The bird"'
                style={{flex:1, background:"#0d1a24",
                  border:`1px solid ${result?.hit?"#00ffcc44":result?"#ff444444":"#1a3040"}`,
                  color:"#c8e8f0", padding:"10px 14px", fontSize:13, ...mf, outline:"none"}}
              />
              <button onClick={()=>{setQuery("");setResult(null);}}
                style={{background:"#0d1a24",border:"1px solid #1a3040",color:"#3a6070",
                  padding:"10px 14px",cursor:"pointer",fontSize:9,...mf}}>CLEAR</button>
            </div>

            {/* Step-by-step state trace */}
            {query.length>0 && (
              <div style={{background:"#060e18",border:"1px solid #1a3040",padding:14,marginBottom:14}}>
                <div style={{fontSize:9,letterSpacing:2,color:"#3a6070",marginBottom:10}}>
                  ONE PASS · EACH CHAR APPLIES ONE AFFINE MAP · FINAL STATE = ADDRESS
                </div>
                <div style={{display:"flex",flexWrap:"wrap",gap:3,marginBottom:10}}>
                  {querySteps.map((step,i) => (
                    <div key={i} style={{
                      background: i===querySteps.length-1?"#001a30":"#0a1520",
                      border:`1px solid ${i===querySteps.length-1?"#00ffcc33":"#1a3040"}`,
                      padding:"6px 8px",minWidth:58,
                    }}>
                      <div style={{fontSize:10,marginBottom:2}}>
                        {i===0
                          ? <span style={{color:"#1a4050"}}>START</span>
                          : <><span style={{color:opColor(step.op),fontWeight:"bold"}}>'{step.ch}'</span>
                             <span style={{color:"#1a3040",fontSize:8}}>·{step.op}</span></>
                        }
                      </div>
                      <div style={{fontSize:9,color:i===querySteps.length-1?"#00ffcc":"#1a5040"}}>
                        {affineStr(step.aff)}
                      </div>
                    </div>
                  ))}
                </div>
                <div style={{fontSize:10,display:"flex",gap:16,flexWrap:"wrap"}}>
                  <span style={{color:"#3a6070"}}>→ final state:</span>
                  <span style={{color:"#00ffcc",fontWeight:"bold"}}>f(p) = {affineStr(result?.qState||IDENT)} (mod 257)</span>
                  <span style={{color:"#3a6070"}}>→ map key:</span>
                  <span style={{color:"#ff8844"}}>"{result?.key}"</span>
                  <span style={{color:"#3a6070"}}>→ 1 lookup</span>
                </div>
              </div>
            )}

            {/* Result */}
            {result && (
              <div style={{
                background:result.hit?"#001a10":"#160008",
                border:`1px solid ${result.hit?"#00aa4455":"#aa002233"}`,
                padding:18,marginBottom:16,
              }}>
                {result.hit ? (
                  <>
                    <div style={{fontSize:9,letterSpacing:2,color:"#00aa44",marginBottom:12}}>
                      ◉ O(1) HIT — HOLOGRAPHIC READOUT RETURNED
                    </div>
                    <div style={{display:"grid",gridTemplateColumns:"1fr 1fr",gap:20}}>
                      <div>
                        <div style={{fontSize:9,color:"#3a6070",marginBottom:5}}>QUERY</div>
                        <div style={{fontSize:15,color:"#888"}}>{query}</div>
                        <div style={{fontSize:9,color:"#1a4050",marginTop:6}}>
                          rotation state {stateIndex(result.qState).toLocaleString()} of 65,792
                        </div>
                      </div>
                      <div>
                        <div style={{fontSize:9,color:"#3a6070",marginBottom:5}}>READOUT (stored suffix)</div>
                        <div style={{fontSize:15}}>
                          <span style={{color:"#333"}}>{result.hit.sentence.slice(0,result.hit.prefixLen)}</span>
                          <span style={{color:"#00ffcc",fontWeight:"bold"}}>{result.hit.readout}</span>
                        </div>
                        <div style={{fontSize:9,color:"#1a4050",marginTop:6}}>
                          from "{result.hit.sentence}" · position {result.hit.prefixLen}
                        </div>
                      </div>
                    </div>
                  </>
                ) : (
                  <>
                    <div style={{fontSize:9,letterSpacing:2,color:"#aa0022",marginBottom:6}}>
                      ✗ MISS — state "{result.key}" not in store
                    </div>
                    <div style={{fontSize:10,color:"#3a6070"}}>
                      This prefix traces a path no ingested sequence has taken. The geometry has no memory here.
                    </div>
                  </>
                )}
              </div>
            )}

            {!query && (
              <div style={{padding:"24px 0",textAlign:"center",color:"#1a3040",fontSize:11}}>
                type any prefix — or click a sentence below
              </div>
            )}

            <div style={{background:"#0a1520",border:"1px solid #1a3040",padding:14}}>
              <div style={{fontSize:9,letterSpacing:2,color:"#3a6070",marginBottom:8}}>CORPUS (click to query first two words)</div>
              <div style={{display:"flex",flexWrap:"wrap",gap:6}}>
                {CORPUS.map((s,i) => (
                  <button key={i} onClick={() => {
                    const pfx = s.split(" ").slice(0,2).join(" ");
                    setQuery(pfx); doRecall(pfx);
                  }} style={{background:"#060e18",border:"1px solid #1a3040",color:"#3a6070",
                    padding:"5px 10px",cursor:"pointer",fontSize:10,...mf}}>
                    {s}
                  </button>
                ))}
              </div>
            </div>
          </div>
        )}

        {/* ── TAB 2: INGESTION ──────────────────────────────────────────── */}
        {tab==="ingest" && (
          <div>
            <div style={{fontSize:10,color:"#3a6070",marginBottom:14,lineHeight:1.8}}>
              Each character advances the rotation state. At every position the system writes:
              <span style={{color:"#00ffcc"}}> state → suffix</span>. No external index.
              The rotation state is the address. The suffix is the holographic readout stored there.
            </div>

            <div style={{display:"flex",gap:10,marginBottom:16,alignItems:"center"}}>
              <button onClick={runIngest} disabled={ingesting} style={{
                background:"#001428",border:"1px solid #00ffcc88",color:"#00ffcc",
                padding:"9px 18px",cursor:ingesting?"not-allowed":"pointer",
                fontSize:10,letterSpacing:2,opacity:ingesting?0.5:1,...mf,
              }}>
                {ingesting?"INGESTING...":"▶ ANIMATE INGESTION"}
              </button>
              <span style={{fontSize:10,color:"#1a3040"}}>"{CORPUS[0]}"</span>
            </div>

            {animSteps.length>0 && ingestStep>=0 && (
              <>
                <div style={{fontSize:9,letterSpacing:2,color:"#3a6070",marginBottom:8}}>
                  CHAR · OP · NEW STATE · READOUT STORED
                </div>
                <div style={{display:"flex",flexWrap:"wrap",gap:3,marginBottom:14}}>
                  {animSteps.slice(0,Math.min(ingestStep+1,animSteps.length)).map((step,i) => {
                    const isLast = i===Math.min(ingestStep,animSteps.length-1);
                    return (
                      <div key={i} style={{
                        background:isLast?"#001a30":"#060e18",
                        border:`1px solid ${isLast?"#00ffcc44":"#1a3040"}`,
                        padding:"7px 9px",minWidth:62,transition:"all 0.15s",
                      }}>
                        <div style={{fontSize:10,marginBottom:2}}>
                          {i===0
                            ? <span style={{color:"#1a4050"}}>START</span>
                            : <><span style={{color:opColor(step.op),fontWeight:"bold"}}>'{step.ch}'</span>
                               <span style={{color:"#1a3040",fontSize:8}}> {step.op}</span></>
                          }
                        </div>
                        <div style={{fontSize:9,color:isLast?"#00ffcc":"#1a5040"}}>
                          {affineStr(step.aff)}
                        </div>
                      </div>
                    );
                  })}
                </div>

                {currentIngestStep && ingestStep>0 && (
                  <div style={{background:"#001a10",border:"1px solid #00aa4444",padding:16,marginBottom:14}}>
                    <div style={{fontSize:9,letterSpacing:2,color:"#00aa44",marginBottom:10}}>
                      STORE WRITE
                    </div>
                    <div style={{display:"grid",gridTemplateColumns:"auto 30px 1fr",gap:14,alignItems:"center"}}>
                      <div>
                        <div style={{fontSize:8,color:"#3a6070",marginBottom:4}}>KEY</div>
                        <div style={{fontSize:14,color:"#00ffcc",fontWeight:"bold"}}>
                          f(p) = {affineStr(currentIngestStep.aff)}
                        </div>
                        <div style={{fontSize:9,color:"#1a4050",marginTop:2}}>
                          state {stateIndex(currentIngestStep.aff).toLocaleString()}
                        </div>
                      </div>
                      <div style={{fontSize:20,color:"#1a4050",textAlign:"center"}}>→</div>
                      <div>
                        <div style={{fontSize:8,color:"#3a6070",marginBottom:4}}>VALUE (readout)</div>
                        <div style={{fontSize:14}}>
                          <span style={{color:"#333"}}>{CORPUS[0].slice(0,Math.max(0,ingestStep-1))}</span>
                          <span style={{color:"#00ffcc",fontWeight:"bold"}}>{CORPUS[0].slice(Math.max(0,ingestStep-1))}</span>
                        </div>
                        <div style={{fontSize:9,color:"#1a4050",marginTop:2}}>
                          {CORPUS[0].length-Math.max(0,ingestStep-1)} bytes
                        </div>
                      </div>
                    </div>
                  </div>
                )}
              </>
            )}
            {animSteps.length===0 && (
              <div style={{padding:"40px 0",textAlign:"center",color:"#1a3040",fontSize:11}}>
                press ▶ to animate
              </div>
            )}

            <div style={{display:"grid",gridTemplateColumns:"1fr 1fr 1fr",gap:10,marginTop:8}}>
              {[
                {label:"store entries",val:HOLO_STORE.size,color:"#00ffcc"},
                {label:"sentences",val:CORPUS.length,color:"#4488ff"},
                {label:"recall cost",val:"O(1)",color:"#44ff88"},
              ].map(({label,val,color})=>(
                <div key={label} style={{background:"#0a1520",border:"1px solid #1a3040",
                  padding:"12px 14px",textAlign:"center"}}>
                  <div style={{fontSize:24,fontWeight:"bold",color,marginBottom:3}}>{val}</div>
                  <div style={{fontSize:9,color:"#3a6070"}}>{label}</div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* ── TAB 3: NON-COMMUTATIVITY ──────────────────────────────────── */}
        {tab==="noncomm" && (
          <div>
            <div style={{fontSize:10,color:"#3a6070",marginBottom:14,lineHeight:1.8}}>
              Affine maps over GF(257) do not commute. X then Y ≠ Y then X.
              This means word order is physically encoded — two sentences with the same words
              in different order land at different rotation states and thus different addresses.
            </div>

            <div style={{display:"grid",gridTemplateColumns:"1fr 1fr",gap:16,marginBottom:16}}>
              {/* Ring */}
              <div style={{background:"#0a1520",border:"1px solid #1a3040",padding:16}}>
                <div style={{fontSize:9,letterSpacing:2,color:"#3a6070",marginBottom:10}}>MANUAL PAD</div>
                <svg width="100%" viewBox="-115 -115 230 230" style={{display:"block",marginBottom:10}}>
                  {Array.from({length:RING_N},(_,i)=>{
                    const a=(i/RING_N)*2*Math.PI-Math.PI/2;
                    const x=Math.cos(a)*RING_R,y=Math.sin(a)*RING_R;
                    const set=manualBits[i]===1;
                    return <circle key={i} cx={x} cy={y} r={set?5:2.5}
                      fill={set?"#00ffcc":"#0d2030"}
                      stroke={set?"#00aa88":"#1a3040"} strokeWidth={0.5}/>;
                  })}
                  <text textAnchor="middle" y={-8} fontSize={11} fill="#00ffcc">{affineStr(manualAff)}</text>
                  <text textAnchor="middle" y={10} fontSize={8} fill="#3a6070">
                    state {stateIndex(manualAff).toLocaleString()}
                  </text>
                </svg>
                <div style={{display:"flex",gap:6,marginBottom:8}}>
                  {["X","Y","Z"].map(op=>(
                    <button key={op} onClick={()=>applyOp(op)} style={{
                      flex:1,background:`${opColor(op)}11`,border:`1px solid ${opColor(op)}44`,
                      color:opColor(op),padding:"8px 4px",cursor:"pointer",fontSize:13,
                      fontWeight:"bold",...mf,
                    }}>
                      {op}
                      <div style={{fontSize:8,opacity:0.6,fontWeight:"normal"}}>
                        {op==="X"?"p+1":op==="Y"?"3p":"3p+1"}
                      </div>
                    </button>
                  ))}
                </div>
                <div style={{display:"flex",gap:3,flexWrap:"wrap",minHeight:22,marginBottom:6}}>
                  {manualHistory.length===0
                    ? <span style={{color:"#1a3040",fontSize:10}}>press a rotation</span>
                    : manualHistory.map((op,i)=>(
                      <span key={i} style={{
                        background:op==="X"?"#001428":op==="Y"?"#140028":"#001a10",
                        color:opColor(op),border:`1px solid ${opColor(op)}44`,
                        fontSize:10,padding:"1px 6px",fontWeight:"bold",
                      }}>{op}</span>
                    ))
                  }
                </div>
                <button onClick={()=>{setManualBits(new Uint8Array(N));setManualAff(IDENT);setManualHistory([]);}}
                  style={{background:"transparent",border:"1px solid #1a3040",color:"#3a6070",
                    padding:"5px",cursor:"pointer",fontSize:9,...mf,width:"100%"}}>RESET</button>
              </div>

              {/* Proof table */}
              <div style={{background:"#0a1520",border:"1px solid #1a3040",padding:16}}>
                <div style={{display:"flex",justifyContent:"space-between",alignItems:"center",marginBottom:12}}>
                  <div style={{fontSize:9,letterSpacing:2,color:"#3a6070"}}>ALGEBRAIC PROOF</div>
                  <button onClick={runNonComm} style={{
                    background:"#001428",border:"1px solid #00ffcc55",color:"#00ffcc",
                    padding:"6px 12px",cursor:"pointer",fontSize:9,...mf,
                  }}>RUN</button>
                </div>
                {demoResults ? (
                  <div style={{display:"flex",flexDirection:"column",gap:6}}>
                    {/* Group pairs */}
                    {[[0,1],[2,3],[4,5]].map(([ia,ib])=>{
                      const ra=demoResults[ia], rb=demoResults[ib];
                      const same = ra.a.a===rb.a.a && ra.a.b===rb.a.b;
                      return (
                        <div key={ia} style={{
                          background:"#060e18",
                          border:`1px solid ${same?"#660000":"#004422"}`,
                          padding:"10px 12px",
                        }}>
                          <div style={{display:"grid",gridTemplateColumns:"1fr 20px 1fr",gap:8,alignItems:"center"}}>
                            <div>
                              <div style={{fontSize:8,color:"#3a6070",marginBottom:2}}>
                                {ra.ops.map((o,i)=><span key={i} style={{color:opColor(o)}}>{o}{i<ra.ops.length-1?" → ":""}</span>)}
                              </div>
                              <div style={{fontSize:13,color:"#4488ff"}}>{affineStr(ra.a)}</div>
                              <div style={{fontSize:8,color:"#1a3040"}}>#{stateIndex(ra.a).toLocaleString()}</div>
                            </div>
                            <div style={{textAlign:"center",fontSize:16,fontWeight:"bold",
                              color:same?"#ff4444":"#00ff88"}}>{same?"=":"≠"}</div>
                            <div>
                              <div style={{fontSize:8,color:"#3a6070",marginBottom:2}}>
                                {rb.ops.map((o,i)=><span key={i} style={{color:opColor(o)}}>{o}{i<rb.ops.length-1?" → ":""}</span>)}
                              </div>
                              <div style={{fontSize:13,color:"#ff6622"}}>{affineStr(rb.a)}</div>
                              <div style={{fontSize:8,color:"#1a3040"}}>#{stateIndex(rb.a).toLocaleString()}</div>
                            </div>
                          </div>
                        </div>
                      );
                    })}
                    <div style={{fontSize:9,color:"#1a3040",marginTop:4,lineHeight:1.8}}>
                      Every reversed pair lands at a different state. Order is physically encoded.
                    </div>
                  </div>
                ) : (
                  <div style={{padding:"40px 0",textAlign:"center",color:"#1a3040",fontSize:10}}>
                    press RUN to see X→Y vs Y→X etc.
                  </div>
                )}
              </div>
            </div>

            {/* Sentence example */}
            <div style={{background:"#0a1520",border:"1px solid #1a3040",padding:16}}>
              <div style={{fontSize:9,letterSpacing:2,color:"#3a6070",marginBottom:10}}>
                CONCRETE SENTENCE EXAMPLE
              </div>
              {["The cat sat","sat cat The"].map(s => {
                const st = computeState(s);
                return (
                  <div key={s} style={{display:"flex",gap:16,alignItems:"center",marginBottom:8}}>
                    <span style={{fontSize:13,color:"#c8e8f0",minWidth:150}}>"{s}"</span>
                    <span style={{fontSize:10,color:"#3a6070"}}>→</span>
                    <span style={{fontSize:12,color:"#00ffcc"}}>f(p) = {affineStr(st)}</span>
                    <span style={{fontSize:9,color:"#1a4050"}}>state #{stateIndex(st).toLocaleString()}</span>
                  </div>
                );
              })}
              <div style={{fontSize:9,color:"#1a3040",marginTop:6,lineHeight:1.8}}>
                Same words, different order → different rotation states → different addresses in the store.
                No explicit sequence tracking needed. The non-commutativity of GF(257) affine maps does it automatically.
              </div>
            </div>
          </div>
        )}

        {/* ── TAB 4: STATE SPACE ────────────────────────────────────────── */}
        {tab==="states" && (
          <div>
            <div style={{fontSize:10,color:"#3a6070",marginBottom:14,lineHeight:1.8}}>
              The affine group Aff(GF(257)) has exactly 256 × 257 = 65,792 elements.
              Each is a distinct bijective permutation of the 257 positions.
              The same 257 bits at two different rotation states = two completely different memories.
            </div>

            <div style={{display:"grid",gridTemplateColumns:"1fr 1fr 1fr",gap:12,marginBottom:16}}>
              {[
                {label:"multiplier a",range:"1 → 256",count:"256",note:"non-zero → bijection guaranteed",color:"#4488ff"},
                {label:"offset b",range:"0 → 256",count:"257",note:"full range of cube positions",color:"#ff6622"},
                {label:"total states",range:"256 × 257",count:"65,792",note:"unique permutations of 257 positions",color:"#00ffcc"},
              ].map(({label,range,count,note,color})=>(
                <div key={label} style={{background:"#0a1520",border:"1px solid #1a3040",
                  padding:16,textAlign:"center"}}>
                  <div style={{fontSize:9,color:"#3a6070",letterSpacing:1,marginBottom:6}}>{label}</div>
                  <div style={{fontSize:9,color:"#1a4050",marginBottom:4}}>{range}</div>
                  <div style={{fontSize:30,fontWeight:"bold",color,marginBottom:6}}>{count}</div>
                  <div style={{fontSize:9,color:"#1a3040",lineHeight:1.6}}>{note}</div>
                </div>
              ))}
            </div>

            <div style={{background:"#0a1520",border:"1px solid #1a3040",padding:16,marginBottom:14}}>
              <div style={{fontSize:9,letterSpacing:2,color:"#3a6070",marginBottom:12}}>
                ROTATION GROUP COMPARISON
              </div>
              {[
                {name:"SO(3) cubic (old 3³)",states:24,desc:"24 rigid 90° rotations on 3×3×3 grid",color:"#2a3a4a"},
                {name:"A₅ icosahedral (iteration 5)",states:60,desc:"60 even permutations of chiral icosahedral group",color:"#3a4a6a"},
                {name:"Aff(GF(257)) · this proposal",states:65792,desc:"65,792 affine maps over prime field — 3 orders of magnitude richer",color:"#00ffcc"},
              ].map(({name,states,desc,color})=>(
                <div key={name} style={{marginBottom:12}}>
                  <div style={{display:"flex",justifyContent:"space-between",fontSize:10,marginBottom:4}}>
                    <span style={{color}}>{name}</span>
                    <span style={{color,fontWeight:"bold"}}>{states.toLocaleString()}</span>
                  </div>
                  <div style={{width:"100%",height:5,background:"#060e18",borderRadius:3,overflow:"hidden"}}>
                    <div style={{width:`${Math.max(states/65792*100,0.4)}%`,height:"100%",
                      background:color,borderRadius:3}}/>
                  </div>
                  <div style={{fontSize:9,color:"#1a3040",marginTop:2}}>{desc}</div>
                </div>
              ))}
            </div>

            <div style={{background:"#001a10",border:"1px solid #00aa4433",padding:16}}>
              <div style={{fontSize:9,letterSpacing:2,color:"#00aa44",marginBottom:10}}>THE POINT</div>
              <div style={{fontSize:12,color:"#c8e8f0",lineHeight:2.1}}>
                A 257-bit chord: 2²⁵⁷ possible bit patterns.<br/>
                Each storable under any of 65,792 rotation states.<br/>
                <span style={{color:"#3a6070",fontSize:10}}>
                  "The cat sat" traces a specific path through GF(257) to rotation state {stateIndex(computeState("The cat sat")).toLocaleString()}.
                  A different sequence that happens to activate the same bits
                  but via different character order arrives at a different state entirely.
                  The geometry separates them automatically. No explicit sequence tag. No position encoding.
                  The non-commutativity of multiplication in GF(257) does the work.
                </span>
              </div>
            </div>
          </div>
        )}

      </div>
    </div>
  );
}
```