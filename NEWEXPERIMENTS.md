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

```
import { useState, useEffect, useRef, useCallback } from "react";
import * as THREE from "three";

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

// face256Source: which chord currently sits at position 256
// depends on the cube's discrete rotation state
function face256Source({ a, b }) {
  return ((256 - b + N) * modInverse(a, N)) % N;
}
function affStr({ a, b }) {
  return a===1?(b===0?"p":`p+${b}`):(b===0?`${a}p`:`${a}p+${b}`);
}
function stateHue({ a, b }) { return (a/256*280 + b/256*80) % 360; }

// ── DISCRETE ROTATION ALGEBRA ────────────────────────────────────────────────
// A cube rotation is 90°, 180°, or 270° around X, Y, or Z.
// Each maps to an affine transform over GF(257).
// 90° rotations use the generators; 180° = compose twice; 270° = compose thrice.
const ROT_TABLE = {
  X_90:  {a:1, b:1},    // p+1
  X_180: {a:1, b:2},    // p+2  (compose X_90 twice)
  X_270: {a:1, b:256},  // p+256 ≡ p-1 mod 257
  Y_90:  {a:3, b:0},    // 3p
  Y_180: {a:9, b:0},    // 9p   (compose Y_90 twice)
  Y_270: {a:86,b:0},    // 86p  (3^(-1) mod 257 = 86, i.e. 3^3 mod 257... actually 3*86=258≡1)
  Z_90:  {a:3, b:1},    // 3p+1
  Z_180: {a:9, b:4},    // compose Z_90 twice: 3(3p+1)+1 = 9p+4
  Z_270: {a:86,b:171},  // inverse of Z_90
};

// Pick a rotation based on the derived opcode flavor
function pickRotation(op) {
  if (op === "ROTATE_X") {
    const picks = ["X_90","X_180","X_270"];
    return picks[Math.floor(Math.random()*3)];
  }
  if (op === "ROTATE_Y") {
    const picks = ["Y_90","Y_180","Y_270"];
    return picks[Math.floor(Math.random()*3)];
  }
  if (op === "ROTATE_Z") {
    const picks = ["Z_90","Z_180","Z_270"];
    return picks[Math.floor(Math.random()*3)];
  }
  return null;
}

// Quaternion for a discrete rotation
function rotQuaternion(rotKey) {
  const axis = rotKey[0]; // X, Y, or Z
  const deg = parseInt(rotKey.split("_")[1]); // 90, 180, 270
  const rad = (deg * Math.PI) / 180;
  const ax = axis === "X" ? new THREE.Vector3(1,0,0) :
             axis === "Y" ? new THREE.Vector3(0,1,0) :
                            new THREE.Vector3(0,0,1);
  return new THREE.Quaternion().setFromAxisAngle(ax, rad);
}

const OPCODES = {
  ROTATE_X:{ css:"#3377ff", desc:"f(p)=p+k  (X axis)",  aff:null },
  ROTATE_Y:{ css:"#9933ff", desc:"f(p)=3^k·p (Y axis)", aff:null },
  ROTATE_Z:{ css:"#ff2266", desc:"f(p)=3^k·p+k (Z axis)",aff:null },
  ALIGN:   { css:"#00ffcc", desc:"compose sender",     aff:null },
  SEARCH:  { css:"#ff8800", desc:"nearest neighbor",   aff:null },
  SYNC:    { css:"#ffd700", desc:"midpoint",           aff:null },
  FORK:    { css:"#44ff88", desc:"spawn tool",         aff:{a:1,b:2} },
  COMPOSE: { css:"#ff44ff", desc:"wire pipelines",     aff:null },
  LOOP:    { css:"#ffaa00", desc:"iterate dataset",    aff:null },
  CALL:    { css:"#00eeff", desc:"enter subgraph",     aff:null },
  RETURN:  { css:"#ff6600", desc:"exit + pop stack",   aff:null },
  SYNTH:   { css:"#cc44ff", desc:"O(1) recall",        aff:null },
};

function deriveOpcode(stA, stB) {
  const b = (face256Source(stA) + face256Source(stB)) % N;
  if(b<32)  return "ROTATE_X";
  if(b<64)  return "ROTATE_Y";
  if(b<96)  return "ROTATE_Z";
  if(b<128) return "ALIGN";
  if(b<160) return "SEARCH";
  if(b<192) return "SYNC";
  if(b<220) return "FORK";
  return "COMPOSE";
}

const _gc={};
function makeGlow(hex){
  if(_gc[hex]) return _gc[hex];
  const c=new THREE.Color(hex);
  const r=Math.round(c.r*255),g=Math.round(c.g*255),b=Math.round(c.b*255);
  const sz=96,cv=document.createElement("canvas"); cv.width=cv.height=sz;
  const ctx=cv.getContext("2d");
  const gr=ctx.createRadialGradient(sz/2,sz/2,0,sz/2,sz/2,sz/2);
  gr.addColorStop(0,`rgba(${r},${g},${b},1)`);
  gr.addColorStop(0.3,`rgba(${r},${g},${b},0.55)`);
  gr.addColorStop(0.7,`rgba(${r},${g},${b},0.1)`);
  gr.addColorStop(1,`rgba(${r},${g},${b},0)`);
  ctx.fillStyle=gr; ctx.fillRect(0,0,sz,sz);
  const t=new THREE.CanvasTexture(cv); _gc[hex]=t; return t;
}

// Dataset: 8 affine states representing "observations"
const DATASET=[
  {a:3,  b:47 }, {a:9,  b:12 }, {a:27, b:81 }, {a:81, b:200},
  {a:3,  b:155}, {a:9,  b:230}, {a:27, b:44 }, {a:81, b:99 },
];
const DATASET_LABELS=[
  "Mary→bathroom","John→hallway","Daniel→hallway","Sandra→garden",
  "John→office","Sandra→bathroom","Mary→hallway","Daniel→office",
];

// Node roles
const ROLES={
  INPUT: { css:"#00ff88", glow:"#00ff88", geo:"box",  fixed:true  },
  OUTPUT:{ css:"#4488ff", glow:"#4488ff", geo:"box",  fixed:true  },
  PROC:  { css:"#00ccff", glow:"#00ccff", geo:"box",  fixed:false },
  TOOL:  { css:"#ff44ff", glow:"#ff44ff", geo:"octa", fixed:false },
};

export default function App() {
  const mountRef=useRef(null);
  const simRef=useRef(null);
  const [log,setLog]=useState([]);
  const [selected,setSelected]=useState(null);
  const [nodeInfo,setNodeInfo]=useState(null);
  const [progState,setProgState]=useState({iteration:0,phase:"idle",accumState:IDENT,stackDepth:0});
  const [toolCount,setToolCount]=useState(0);
  const [running,setRunning]=useState(false);
  const [rotLog,setRotLog]=useState([]);

  const pushLog=useCallback((msg,color="#6688aa")=>{
    setLog(prev=>[{msg,color,id:Math.random()},...prev].slice(0,20));
  },[]);
  const pushRotLog=useCallback((msg,color="#3377ff")=>{
    setRotLog(prev=>[{msg,color,id:Math.random()},...prev].slice(0,8));
  },[]);

  useEffect(()=>{
    const el=mountRef.current; if(!el) return;
    const W=el.clientWidth,H=el.clientHeight;
    const renderer=new THREE.WebGLRenderer({antialias:true});
    renderer.setPixelRatio(Math.min(devicePixelRatio,2));
    renderer.setSize(W,H); renderer.setClearColor(0x010408,1);
    el.appendChild(renderer.domElement);
    const scene=new THREE.Scene();
    scene.fog=new THREE.FogExp2(0x010408,0.00055);
    const camera=new THREE.PerspectiveCamera(56,W/H,0.5,6000);
    camera.position.set(0,200,820);

    // stars
    {const n=1200,p=new Float32Array(n*3);for(let i=0;i<n;i++){const r=2000+Math.random()*3000,t=Math.random()*Math.PI*2,ph=Math.acos(2*Math.random()-1);p[i*3]=r*Math.sin(ph)*Math.cos(t);p[i*3+1]=r*Math.sin(ph)*Math.sin(t);p[i*3+2]=r*Math.cos(ph);}const g=new THREE.BufferGeometry();g.setAttribute("position",new THREE.BufferAttribute(p,3));scene.add(new THREE.Points(g,new THREE.PointsMaterial({color:0x334455,size:1.3,transparent:true,opacity:0.4,depthWrite:false})));}

    const nodes=[],edges=[],tokens=[];
    let frameN=0,selectedIdx=null;
    let progRunning=false,iteration=0;
    const callStack=[];
    let accumState={...IDENT};

    // camera
    let camTheta=0,camPhi=1.1,camR=820,autoOrbit=true,drag=false,prevM={x:0,y:0},lastAct=0;
    const camSmooth=new THREE.Vector3();
    const onMove=e=>{if(!drag)return;camTheta-=(e.clientX-prevM.x)*0.007;camPhi=Math.max(0.2,Math.min(Math.PI-0.2,camPhi+(e.clientY-prevM.y)*0.007));prevM={x:e.clientX,y:e.clientY};lastAct=Date.now();};
    const onUp=()=>{drag=false;};
    renderer.domElement.addEventListener("mousedown",e=>{drag=true;autoOrbit=false;prevM={x:e.clientX,y:e.clientY};lastAct=Date.now();});
    window.addEventListener("mousemove",onMove);
    window.addEventListener("mouseup",onUp);
    renderer.domElement.addEventListener("wheel",e=>{camR=Math.max(200,Math.min(1800,camR+e.deltaY*0.5));lastAct=Date.now();},{passive:true});
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

    // ── NODE ──
    function makeNode(x,y,z,role,label,initState){
      const rd=ROLES[role]||ROLES.PROC;
      const state=initState?{...initState}:{...IDENT};
      const pos=new THREE.Vector3(x,y,z);
      const col=new THREE.Color(rd.css);

      // Cube geometry — represents 257 faces (256 vocab + 1 register)
      const coreGeo = role==="TOOL" ? new THREE.OctahedronGeometry(14,0) : new THREE.BoxGeometry(22,22,22);
      const coreMat=new THREE.MeshBasicMaterial({color:col,wireframe:true,transparent:true,opacity:0.9});
      const coreMesh=new THREE.Mesh(coreGeo,coreMat); coreMesh.position.copy(pos); scene.add(coreMesh);

      const fillGeo=role==="TOOL"?new THREE.OctahedronGeometry(10,0):new THREE.BoxGeometry(16,16,16);
      const fillMesh=new THREE.Mesh(fillGeo,new THREE.MeshBasicMaterial({color:col,transparent:true,opacity:role==="INPUT"||role==="OUTPUT"?0.15:0.07}));
      fillMesh.position.copy(pos); scene.add(fillMesh);

      const halo=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow(rd.glow),blending:THREE.AdditiveBlending,transparent:true,opacity:role==="INPUT"||role==="OUTPUT"?0.65:0.35,depthWrite:false}));
      halo.scale.setScalar(role==="INPUT"||role==="OUTPUT"?110:80); halo.position.copy(pos); scene.add(halo);

      // face256 indicator — the register face marker
      const f256m=new THREE.Mesh(new THREE.SphereGeometry(3,8,8),new THREE.MeshBasicMaterial({color:0xff8800}));
      f256m.position.copy(pos).add(new THREE.Vector3(16,16,0)); scene.add(f256m);
      const f256g=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow("#ff8800"),blending:THREE.AdditiveBlending,transparent:true,opacity:0.8,depthWrite:false}));
      f256g.scale.setScalar(22); f256g.position.copy(f256m.position); scene.add(f256g);

      // Face256 direction indicator — shows which cube face currently holds register
      const arrowGeo = new THREE.ConeGeometry(3, 10, 6);
      const arrowMat = new THREE.MeshBasicMaterial({color:0xff8800, transparent:true, opacity:0.7});
      const arrowMesh = new THREE.Mesh(arrowGeo, arrowMat);
      arrowMesh.position.copy(pos); scene.add(arrowMesh);

      let ring=null;
      if(role==="TOOL"){ring=new THREE.Mesh(new THREE.RingGeometry(20,22,32),new THREE.MeshBasicMaterial({color:0xff44ff,side:THREE.DoubleSide,transparent:true,opacity:0.35}));ring.position.copy(pos);scene.add(ring);}

      // Discrete rotation state: quaternion tracks accumulated orientation
      const discreteQuat = new THREE.Quaternion(); // current orientation
      const targetQuat = new THREE.Quaternion();    // target after pending rotation
      const rotAnimProgress = { v: 1.0 };           // 1.0 = no animation in progress
      const rotHistory = [];                         // log of rotations applied

      return {
        idx:nodes.length, state, pos, role, label,
        coreMesh, coreMat, fillMesh, halo,
        f256m, f256g, arrowMesh,
        ring, flash:0, birthFrame:frameN, receivedOps:[],
        // discrete rotation tracking
        discreteQuat, targetQuat, rotAnimProgress,
        rotHistory, rotCount: 0,
      };
    }

    function updateNodeColor(nd){
      if(nd.role==="INPUT"||nd.role==="OUTPUT"||nd.role==="TOOL") return;
      const col=new THREE.Color().setHSL(stateHue(nd.state)/360,0.85,0.55);
      nd.coreMat.color.copy(col); nd.fillMesh.material.color.copy(col);
      nd.halo.material.map=makeGlow(`#${col.getHexString()}`); nd.halo.material.needsUpdate=true;
      const fh=face256Source(nd.state)/256;
      const fc=new THREE.Color().setHSL(fh*0.8,1.0,0.6);
      nd.f256m.material.color.copy(fc);
      nd.f256g.material.map=makeGlow(`#${fc.getHexString()}`); nd.f256g.material.needsUpdate=true;
    }

    // ── APPLY DISCRETE ROTATION ──
    // Only called when a ROTATE opcode fires. This is the core mechanic:
    // rotation changes face256, which cascades to all connected edge opcodes.
    function applyDiscreteRotation(nd, rotKey) {
      const rotAff = ROT_TABLE[rotKey];
      if (!rotAff) return;

      // Compose the rotation into the node's affine state
      nd.state = composeAffine(nd.state, rotAff);
      nd.rotCount++;
      nd.rotHistory.push(rotKey);
      if (nd.rotHistory.length > 12) nd.rotHistory.shift();

      // Animate: set target quaternion
      const deltaQ = rotQuaternion(rotKey);
      nd.targetQuat.copy(nd.discreteQuat).multiply(deltaQ);
      nd.rotAnimProgress.v = 0.0; // start animation

      updateNodeColor(nd);

      // CASCADE: recalculate all connected edges
      const oldOps = {};
      for (const ed of edges) {
        if (ed.aIdx === nd.idx || ed.bIdx === nd.idx) {
          oldOps[edges.indexOf(ed)] = ed.op;
          refreshEdge(ed);
        }
      }

      // Log which edges changed opcodes
      for (const ed of edges) {
        if (ed.aIdx === nd.idx || ed.bIdx === nd.idx) {
          const edIdx = edges.indexOf(ed);
          if (oldOps[edIdx] && oldOps[edIdx] !== ed.op) {
            pushLog(`  ⟳ edge N${ed.aIdx}↔N${ed.bIdx}: ${oldOps[edIdx]}→${ed.op}`, OPCODES[ed.op]?.css||"#fff");
            pushRotLog(`N${nd.idx} ${rotKey} → edge ${ed.aIdx}↔${ed.bIdx} now ${ed.op}`, OPCODES[ed.op]?.css||"#fff");
          }
        }
      }

      pushLog(`⟳ N${nd.idx} ${rotKey}  face256=${face256Source(nd.state)}`, "#ff8800");
      pushRotLog(`N${nd.idx} ${rotKey} → f256=${face256Source(nd.state)}`, "#ff8800");
    }

    // ── EDGE ──
    function makeEdge(aIdx,bIdx,forcedOp,isLoop){
      const na=nodes[aIdx],nb=nodes[bIdx]; if(!na||!nb) return null;
      const op=forcedOp||deriveOpcode(na.state,nb.state);
      const css=OPCODES[op]?.css||"#fff";
      const pts=[na.pos.clone(),nb.pos.clone()];
      const geo=new THREE.BufferGeometry().setFromPoints(pts);
      const mat=new THREE.LineBasicMaterial({color:new THREE.Color(css),transparent:true,opacity:isLoop?0.7:0.4,blending:THREE.AdditiveBlending,depthWrite:false});
      const line=new THREE.Line(geo,mat); scene.add(line);

      // Opcode label sprite (small glow at midpoint showing current op)
      const midGlow=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow(css),blending:THREE.AdditiveBlending,transparent:true,opacity:0.5,depthWrite:false}));
      midGlow.scale.setScalar(isLoop?22:14);
      midGlow.position.addVectors(na.pos,nb.pos).multiplyScalar(0.5); scene.add(midGlow);

      let arrow=null;
      if(isLoop){
        arrow=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow(css),blending:THREE.AdditiveBlending,transparent:true,opacity:0.85,depthWrite:false}));
        arrow.scale.setScalar(20);
        arrow.position.addVectors(na.pos,nb.pos).multiplyScalar(0.5); scene.add(arrow);
      }
      return {aIdx,bIdx,op,line,mat,geo,isLoop,arrow,midGlow,forcedOp:!!forcedOp,css};
    }

    function refreshEdge(ed){
      const na=nodes[ed.aIdx],nb=nodes[ed.bIdx]; if(!na||!nb) return;
      if(!ed.forcedOp){
        const nop=deriveOpcode(na.state,nb.state);
        if(nop!==ed.op){
          ed.op=nop;
          ed.css=OPCODES[nop]?.css||"#fff";
          ed.mat.color.set(new THREE.Color(ed.css));
          // Update midpoint glow color
          ed.midGlow.material.map=makeGlow(ed.css);
          ed.midGlow.material.needsUpdate=true;
        }
      }
      const arr=ed.geo.attributes.position.array;
      arr[0]=na.pos.x;arr[1]=na.pos.y;arr[2]=na.pos.z;arr[3]=nb.pos.x;arr[4]=nb.pos.y;arr[5]=nb.pos.z;
      ed.geo.attributes.position.needsUpdate=true;
      ed.midGlow.position.addVectors(na.pos,nb.pos).multiplyScalar(0.5);
      if(ed.arrow){ed.arrow.position.addVectors(na.pos,nb.pos).multiplyScalar(0.5);ed.arrow.material.opacity=0.5+Math.sin(frameN*0.07)*0.35;}
      ed.mat.opacity=ed.isLoop?0.4+Math.sin(frameN*0.06)*0.3:0.22+Math.sin(frameN*0.035+ed.aIdx)*0.1;
    }

    // ── TOKEN ──
    function makeToken(fromNode,toNode,op,isExec,carryState){
      const css=OPCODES[op]?.css||"#fff"; const col=new THREE.Color(css);
      const sz=isExec?5.5:4;
      const mesh=new THREE.Mesh(isExec?new THREE.OctahedronGeometry(sz,0):new THREE.SphereGeometry(sz,8,8),new THREE.MeshBasicMaterial({color:isExec?0xffffff:col}));
      mesh.position.copy(fromNode.pos); scene.add(mesh);
      const sprite=new THREE.Sprite(new THREE.SpriteMaterial({map:makeGlow(isExec?"#ffffff":css),blending:THREE.AdditiveBlending,transparent:true,opacity:1.0,depthWrite:false}));
      sprite.scale.setScalar(isExec?40:24); sprite.position.copy(fromNode.pos); scene.add(sprite);
      const TRAIL=14,tArr=new Float32Array(TRAIL*3),tGeo=new THREE.BufferGeometry();
      tGeo.setAttribute("position",new THREE.BufferAttribute(tArr,3)); tGeo.setDrawRange(0,0);
      const tLine=new THREE.Line(tGeo,new THREE.LineBasicMaterial({color:isExec?0xffffff:col,transparent:true,opacity:0.5,blending:THREE.AdditiveBlending,depthWrite:false}));
      scene.add(tLine);
      return {fromNode,toNode,op,isExec,carryState:carryState?{...carryState}:{...IDENT},progress:0,trail:[],dead:false,mesh,sprite,tLine,tGeo,tArr,TRAIL,onArrive:null};
    }

    function disposeToken(tok){
      scene.remove(tok.mesh);tok.mesh.material.dispose();tok.mesh.geometry.dispose();
      scene.remove(tok.sprite);tok.sprite.material.dispose();
      scene.remove(tok.tLine);tok.tGeo.dispose();tok.tLine.material.dispose();
    }

    function sendToken(fromIdx,toIdx,op,isExec,carry,onArrive){
      const na=nodes[fromIdx],nb=nodes[toIdx]; if(!na||!nb) return;
      const tok=makeToken(na,nb,op,isExec,carry); tok.onArrive=onArrive; tokens.push(tok);
    }

    // ── TOPOLOGICAL DISPATCH ──────────────────────────────────────────────
    // No switch statement. Every opcode is a geometric rule over GF(257).
    // "LOOP" and "RETURN" are not special instructions — they are wire
    // behaviors. The onArrive callback IS the wire. Data is the clock.
    const DISPATCH = {
      // Discrete physical rotations — the cube turns, face256 shifts, edges cascade
      ROTATE_X: (nd, tok) => { applyDiscreteRotation(nd, pickRotation("ROTATE_X")); },
      ROTATE_Y: (nd, tok) => { applyDiscreteRotation(nd, pickRotation("ROTATE_Y")); },
      ROTATE_Z: (nd, tok) => { applyDiscreteRotation(nd, pickRotation("ROTATE_Z")); },

      // Affine compositions — geometry combining with geometry
      ALIGN:   (nd, tok) => { nd.state = composeAffine(nd.state, tok.fromNode.state); },
      COMPOSE: (nd, tok) => { nd.state = composeAffine(nd.state, tok.carryState); },
      SYNC:    (nd, tok) => {
        nd.state = {
          a: Math.max(1, Math.round((nd.state.a + tok.fromNode.state.a) / 2)),
          b: Math.round((nd.state.b + tok.fromNode.state.b) / 2) % N
        };
      },

      // Nearest-neighbor in GF(257) state space
      SEARCH: (nd, tok) => {
        let best = null, bestD = Infinity;
        for (const n of nodes) {
          if (n === nd) continue;
          const da = Math.abs(n.state.a - nd.state.a) / 256;
          const db = Math.abs(n.state.b - nd.state.b) / 256;
          const d = Math.sqrt(da * da + db * db);
          if (d < bestD) { bestD = d; best = n; }
        }
        if (best) { best.flash = 28; pushLog(`SEARCH → N${best.idx} (d=${bestD.toFixed(3)})`, "#ff8800"); }
      },

      // Mitosis — compose + crystallize a tool node
      FORK: (nd, tok) => {
        nd.state = composeAffine(nd.state, tok.fromNode.state);
        if (nodes.length < 16) synthesizeTool(nd, tok.carryState);
      },

      // Stack operations — storing/restoring chords (face256 as frame register)
      CALL: (nd, tok) => {
        callStack.push({ returnIdx: tok.fromNode.idx, state: { ...tok.carryState } });
        nd.state = composeAffine(nd.state, tok.carryState);
        pushLog(`CALL N${nd.idx} [stack=${callStack.length}]`, "#00eeff");
      },
      RETURN: (nd, tok) => {
        if (callStack.length > 0) {
          const frame = callStack.pop();
          pushLog(`RETURN → N${frame.returnIdx} [stack=${callStack.length}]`, "#ff6600");
        }
      },

      // Pure wire behaviors — the topology does the work, not the opcode name
      LOOP:  (nd, _tok) => { /* wire from OUT→IN: onArrive callback IS the loop */ },
      SYNTH: (nd, _tok) => { nd.flash = 35; pushLog(`◆ N${nd.idx} O(1) recall`, "#cc44ff"); },
    };

    function applyToken(tok) {
      const nd = tok.toNode, op = tok.op;
      nd.flash = 20;
      nd.receivedOps = [op, ...nd.receivedOps].slice(0, 5);

      // Dispatch: geometry decides behavior, not a CPU instruction decoder
      const rule = DISPATCH[op];
      if (rule) rule(nd, tok);
      else nd.state = composeAffine(nd.state, tok.fromNode.state); // fallback: pure compose

      // The wire fires. onArrive IS the dataflow edge.
      // No special-casing for LOOP or RETURN — the callback chain is the topology.
      if (tok.onArrive) tok.onArrive(nd.state);

      // State changed → cascade edge opcodes (rotations handle this internally)
      if (op !== "ROTATE_X" && op !== "ROTATE_Y" && op !== "ROTATE_Z") {
        updateNodeColor(nd);
        for (const ed of edges) {
          if (ed.aIdx === nd.idx || ed.bIdx === nd.idx) refreshEdge(ed);
        }
      }

      pushLog(`[N${tok.fromNode.idx}→N${nd.idx}] ${op}`, OPCODES[op]?.css || "#fff");
    }

    function updateTokenTick(tok){
      if(tok.dead) return;
      tok.trail.unshift(tok.mesh.position.clone());
      if(tok.trail.length>tok.TRAIL) tok.trail.pop();
      tok.progress+=0.022+Math.random()*0.004;
      const t=Math.min(tok.progress,1);
      tok.mesh.position.lerpVectors(tok.fromNode.pos,tok.toNode.pos,t);
      tok.sprite.position.copy(tok.mesh.position);
      if(tok.isExec){tok.mesh.rotation.y+=0.1;tok.mesh.rotation.x+=0.07;}
      const n=tok.trail.length;
      for(let i=0;i<n;i++){tok.tArr[i*3]=tok.trail[i].x;tok.tArr[i*3+1]=tok.trail[i].y;tok.tArr[i*3+2]=tok.trail[i].z;}
      tok.tGeo.attributes.position.needsUpdate=true; tok.tGeo.setDrawRange(0,n);
      if(tok.progress>=1){applyToken(tok);tok.dead=true;}
    }

    // ── TOOL SYNTHESIS ──
    function synthesizeTool(fromNode,crystallizedState){
      const dir=new THREE.Vector3(Math.random()-.5,Math.random()-.5,Math.random()-.5).normalize().multiplyScalar(150+Math.random()*60);
      const tn=makeNode(fromNode.pos.x+dir.x,fromNode.pos.y+dir.y,fromNode.pos.z+dir.z,"TOOL",`T${nodes.filter(n=>n.role==="TOOL").length}`,crystallizedState||fromNode.state);
      tn.idx=nodes.length; nodes.push(tn);
      const ed=makeEdge(fromNode.idx,tn.idx,"SYNTH",false);
      if(ed){ed.forcedOp=true;ed.css=OPCODES.SYNTH.css;ed.mat.color.set(new THREE.Color(OPCODES.SYNTH.css));edges.push(ed);}
      if(nodes.length>3){
        const ri=Math.floor(Math.random()*(nodes.length-1));
        const ed2=makeEdge(ri,tn.idx,"COMPOSE",false);
        if(ed2){ed2.forcedOp=true;ed2.css=OPCODES.COMPOSE.css;ed2.mat.color.set(new THREE.Color(OPCODES.COMPOSE.css));edges.push(ed2);}
      }
      setToolCount(nodes.filter(n=>n.role==="TOOL").length);
      pushLog(`◆ TOOL crystallized (N${tn.idx})`,"#cc44ff");
    }

    // ── PROGRAM SEQUENCER ──
    const IN=0,SR=1,RV=2,AL=3,OUT=4;

    function stepProgram(inState){
      if(!progRunning) return;
      if(iteration>=DATASET.length){
        progRunning=false; setRunning(false);
        setProgState({iteration,phase:"✓ done",accumState:{...accumState},stackDepth:callStack.length});
        pushLog(`── complete (${DATASET.length} iterations) ──`,"#00ff88"); return;
      }
      const input=DATASET[iteration];
      accumState=inState?composeAffine(inState,input):composeAffine(IDENT,input);
      nodes[IN].state={...input}; nodes[IN].flash=15; updateNodeColor(nodes[IN]);
      const iter=++iteration;
      pushLog(`── iter ${iter}: ${DATASET_LABELS[iter-1]} ──`,"#334466");
      setProgState({iteration:iter,phase:`running ${iter}/${DATASET.length}`,accumState:{...accumState},stackDepth:callStack.length});

      // ── PURE TOPOLOGICAL EXECUTION (SELF-CLOCKING) ──
      // Tokens fire only when the previous token arrives. No external timers.
      // Data is the clock. The wire topology IS the program.
      sendToken(IN, SR, "SEARCH", true, {...accumState}, (state1) => {
        if(!progRunning) return;
        sendToken(SR, RV, "CALL", true, state1, (state2) => {
          if(!progRunning) return;
          sendToken(RV, AL, "RETURN", true, state2, (state3) => {
            if(!progRunning) return;
            sendToken(AL, OUT, "ALIGN", true, state3, (state4) => {
              if(!progRunning) return;
              // The geometric LOOP: OUT connects back to IN.
              // No "case LOOP" needed — the wire IS the loop.
              sendToken(OUT, IN, "LOOP", true, state4, (state5) => {
                if(progRunning) stepProgram(state5);
              });
            });
          });
        });
      });
    }

    // ── INIT GRAPH ──
    const nd0=makeNode(  0, 200,   0,"INPUT","IN",    IDENT       ); nd0.idx=0; nodes.push(nd0);
    const nd1=makeNode(-220,  30,  90,"PROC","SEARCH",{a:3,b:47}  ); nd1.idx=1; nodes.push(nd1);
    const nd2=makeNode(  0,   0,-210,"PROC","RESOLVE",{a:9,b:12}  ); nd2.idx=2; nodes.push(nd2);
    const nd3=makeNode( 220,  30,  90,"PROC","ALIGN",  {a:3,b:81} ); nd3.idx=3; nodes.push(nd3);
    const nd4=makeNode(  0,-200,   0,"OUTPUT","OUT",  IDENT       ); nd4.idx=4; nodes.push(nd4);
    const nd5=makeNode(-180,-100, -80,"PROC","INDEX",  {a:27,b:200}); nd5.idx=5; nodes.push(nd5);
    const nd6=makeNode( 180,-100, -80,"PROC","STORE",  {a:81,b:44} ); nd6.idx=6; nodes.push(nd6);
    for(const nd of nodes) updateNodeColor(nd);

    // pipeline edges (fixed)
    const pipe=[[IN,SR,"SEARCH"],[SR,RV,"CALL"],[RV,AL,"RETURN"],[AL,OUT,"ALIGN"]];
    for(const [a,b,op] of pipe){
      const ed=makeEdge(a,b,op,false);
      if(ed){ed.forcedOp=true;ed.css=OPCODES[op].css;ed.mat.color.set(new THREE.Color(OPCODES[op].css));edges.push(ed);}
    }
    const loopEd=makeEdge(OUT,IN,"LOOP",true);
    if(loopEd){loopEd.forcedOp=true;loopEd.css=OPCODES.LOOP.css;loopEd.mat.color.set(new THREE.Color(OPCODES.LOOP.css));edges.push(loopEd);}
    // aux dynamic edges — these derive opcodes from face256!
    for(const [a,b] of [[1,5],[5,4],[2,6],[6,3],[5,6]]){
      const ed=makeEdge(a,b,null,false); if(ed) edges.push(ed);
    }

    simRef.current={
      start:()=>{
        if(progRunning) return;
        progRunning=true; iteration=0; accumState={...IDENT};
        callStack.length=0; setRunning(true);
        setProgState({iteration:0,phase:"starting…",accumState:IDENT,stackDepth:0});
        pushLog("── program start ──","#00ff88");
        nodes[IN].state={...IDENT}; nodes[OUT].state={...IDENT};
        updateNodeColor(nodes[IN]); updateNodeColor(nodes[OUT]);
        setTimeout(()=>stepProgram(null),300);
      },
      stop:()=>{progRunning=false;setRunning(false);setProgState(p=>({...p,phase:"stopped"}));},
      fire:()=>{if(!edges.length)return;const e=edges.filter(e=>!e.isLoop);if(e.length){const ed=e[Math.floor(Math.random()*e.length)];sendToken(ed.aIdx,ed.bIdx,ed.op,false,{...IDENT},null);}},
      info:(idx)=>{const nd=nodes[idx];if(!nd)return null;return{state:{...nd.state},f256:face256Source(nd.state),label:nd.label,role:nd.role,ops:[...nd.receivedOps],rotCount:nd.rotCount,rotHistory:[...nd.rotHistory]};},
    };

    // ── ANIMATE ──
    const _com=new THREE.Vector3(); let animId;
    const _tmpQ = new THREE.Quaternion();
    const animate=()=>{
      animId=requestAnimationFrame(animate); frameN++;
      if(!drag&&Date.now()-lastAct>3000) autoOrbit=true;
      if(autoOrbit) camTheta+=0.0014;
      _com.set(0,0,0); for(const n of nodes) _com.add(n.pos);
      if(nodes.length) _com.divideScalar(nodes.length);
      camSmooth.lerp(_com,0.02);
      const sp=Math.sin(camPhi),cp=Math.cos(camPhi);
      camera.position.set(camSmooth.x+camR*sp*Math.sin(camTheta),camSmooth.y+camR*cp,camSmooth.z+camR*sp*Math.cos(camTheta));
      camera.lookAt(camSmooth);

      for(const nd of nodes){
        const age=Math.min((frameN-nd.birthFrame)/30,1);
        const fl=nd.flash>0?nd.flash/20:0; if(nd.flash>0) nd.flash--;

        // ── DISCRETE ROTATION ANIMATION ──
        // Cubes do NOT spin. They only rotate when a ROTATE opcode fires.
        // Smoothly slerp toward the target quaternion.
        if (nd.rotAnimProgress.v < 1.0) {
          nd.rotAnimProgress.v = Math.min(1.0, nd.rotAnimProgress.v + 0.04);
          // Ease-out cubic
          const t = nd.rotAnimProgress.v;
          const eased = 1 - Math.pow(1 - t, 3);
          _tmpQ.slerpQuaternions(nd.discreteQuat, nd.targetQuat, eased);
          nd.coreMesh.quaternion.copy(_tmpQ);
          nd.fillMesh.quaternion.copy(_tmpQ);
          // When done, snap to target
          if (nd.rotAnimProgress.v >= 1.0) {
            nd.discreteQuat.copy(nd.targetQuat);
            nd.coreMesh.quaternion.copy(nd.targetQuat);
            nd.fillMesh.quaternion.copy(nd.targetQuat);
          }
        }

        nd.coreMesh.scale.setScalar(age*(1+fl*0.5));
        nd.fillMesh.scale.setScalar(age*(1+fl*0.5));

        if(nd.ring){nd.ring.rotation.z+=0.012;nd.ring.position.copy(nd.pos);nd.ring.material.opacity=0.2+fl*0.35;}
        const bh=nd.role==="INPUT"||nd.role==="OUTPUT"?110:nd.role==="TOOL"?95:78;
        nd.halo.scale.setScalar(bh*age+fl*32);
        nd.halo.material.opacity=(nd.role==="INPUT"||nd.role==="OUTPUT"?0.55:0.28)+fl*0.42;
        if(nd.idx===selectedIdx){nd.halo.scale.setScalar(118*age);nd.halo.material.opacity=0.82;}

        // face256 orbiter — orbits slowly to indicate which face holds the register
        const f256val = face256Source(nd.state);
        const orbitAngle = (f256val / 256) * Math.PI * 2 + frameN * 0.008;
        const orbitR = 20;
        nd.f256m.position.set(
          nd.pos.x + Math.cos(orbitAngle) * orbitR,
          nd.pos.y + Math.sin(orbitAngle * 0.7) * orbitR,
          nd.pos.z + Math.sin(orbitAngle) * orbitR * 0.5
        );
        nd.f256g.position.copy(nd.f256m.position);

        // Arrow points in direction determined by face256 value
        const arrowDir = new THREE.Vector3(
          Math.cos(f256val / 256 * Math.PI * 2),
          Math.sin(f256val / 128 * Math.PI),
          Math.cos(f256val / 64 * Math.PI)
        ).normalize();
        nd.arrowMesh.position.copy(nd.pos).addScaledVector(arrowDir, 16);
        nd.arrowMesh.quaternion.setFromUnitVectors(new THREE.Vector3(0,1,0), arrowDir);
        nd.arrowMesh.material.opacity = 0.4 + fl * 0.4;
      }

      if(frameN%50===0) for(const ed of edges) refreshEdge(ed);

      for(let i=tokens.length-1;i>=0;i--){
        updateTokenTick(tokens[i]);
        if(tokens[i].dead){disposeToken(tokens[i]);tokens.splice(i,1);}
      }

      // Ambient tokens (only when idle) — use dynamic edges so rotations cascade
      if(!progRunning&&frameN%120===0&&tokens.length<6){
        const dynamic=edges.filter(e=>!e.isLoop&&!e.forcedOp);
        if(dynamic.length){
          const e=dynamic[Math.floor(Math.random()*dynamic.length)];
          sendToken(e.aIdx,e.bIdx,e.op,false,{...IDENT},null);
        }
      }

      if(frameN%20===0){
        if(selectedIdx!==null&&simRef.current) setNodeInfo(simRef.current.info(selectedIdx));
        setToolCount(nodes.filter(n=>n.role==="TOOL").length);
        setProgState(p=>({...p,accumState:{...accumState},stackDepth:callStack.length}));
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
      if(el.contains(renderer.domElement)) el.removeChild(renderer.domElement);
      renderer.dispose();
    };
  },[pushLog,pushRotLog]);

  const mf={fontFamily:'"JetBrains Mono","Fira Code","Courier New",monospace'};
  const OC=k=>OPCODES[k]?.css||"#fff";

  return (
    <div style={{display:"flex",height:"100vh",background:"#010408",...mf}}>
      <div style={{flex:1,position:"relative",overflow:"hidden"}}>
        <div ref={mountRef} style={{width:"100%",height:"100%"}}/>

        {/* Legend */}
        <div style={{position:"absolute",top:14,left:14,fontSize:9,lineHeight:2.1,
          pointerEvents:"none",background:"rgba(1,4,8,0.82)",border:"1px solid #0a1422",padding:"10px 14px"}}>
          <div style={{color:"#223344",letterSpacing:2,marginBottom:4}}>NODE ROLES</div>
          {[["#00ff88","▣","IN  — program entry, always IDENT"],
            ["#4488ff","▣","OUT — accumulates result, LOOP source"],
            ["#00ccff","▣","PROC — 257-face cube (256 vocab + register)"],
            ["#ff44ff","◆","TOOL — crystallized solution, O(1)"],
          ].map(([c,s,l])=>(
            <div key={l} style={{display:"flex",gap:5}}>
              <span style={{color:c}}>{s}</span><span style={{color:"#1a3040"}}>{l}</span>
            </div>
          ))}
          <div style={{color:"#ff8800",marginTop:4,fontSize:8}}>● = face 256 register (rotation-dependent)</div>
          <div style={{color:"#334466",marginTop:2,fontSize:7,lineHeight:1.6}}>
            Cubes only rotate on ROTATE opcodes<br/>
            90° / 180° / 270° discrete steps<br/>
            Rotation shifts face256 → changes edge opcodes
          </div>
        </div>

        {/* Rotation cascade log */}
        <div style={{position:"absolute",top:14,right:280,
          background:"rgba(1,4,8,0.82)",border:"1px solid #0a1422",
          padding:"8px 12px",fontSize:8,lineHeight:1.9,pointerEvents:"none",
          minWidth:180,maxWidth:260}}>
          <div style={{color:"#223344",letterSpacing:2,marginBottom:4}}>ROTATION CASCADE</div>
          {rotLog.length===0 ? (
            <div style={{color:"#0a1a28",fontSize:8}}>no rotations yet</div>
          ) : rotLog.map((e,i)=>(
            <div key={e.id} style={{color:e.color,opacity:Math.max(0.15,1-i*0.12)}}>{e.msg}</div>
          ))}
        </div>

        {/* Program pipeline */}
        <div style={{position:"absolute",bottom:14,left:14,
          background:"rgba(1,4,8,0.85)",border:"1px solid #0a1422",
          padding:"10px 16px",fontSize:9,lineHeight:2,pointerEvents:"none"}}>
          <div style={{color:"#223344",letterSpacing:2,marginBottom:5}}>RUNNING PROGRAM</div>
          <div style={{display:"flex",alignItems:"center",gap:3,flexWrap:"wrap"}}>
            {[{l:"IN",c:"#00ff88"},{o:"SEARCH",c:OC("SEARCH")},{l:"SEARCH",c:"#00ccff"},
              {o:"CALL",c:OC("CALL")},{l:"RESOLVE",c:"#00ccff"},{o:"RETURN",c:OC("RETURN")},
              {l:"ALIGN",c:"#00ccff"},{o:"ALIGN",c:OC("ALIGN")},{l:"OUT",c:"#4488ff"},
              {o:"LOOP",c:OC("LOOP")},{l:"IN",c:"#00ff88"},
            ].map((item,i)=>(
              item.o
                ? <span key={i} style={{color:item.c,fontSize:8}}>─{item.o}→</span>
                : <span key={i} style={{color:item.c,border:`1px solid ${item.c}44`,padding:"0 5px",fontWeight:"bold"}}>{item.l}</span>
            ))}
          </div>
          <div style={{color:"#1a3040",fontSize:8,marginTop:3}}>
            self-clocking: each token fires only when the previous arrives · no timers
          </div>
        </div>
      </div>

      {/* Side panel */}
      <div style={{width:265,background:"#00020a",borderLeft:"1px solid #0a1422",
        display:"flex",flexDirection:"column",overflow:"hidden"}}>

        {/* Controls */}
        <div style={{padding:"14px 16px 12px",borderBottom:"1px solid #0a1422"}}>
          <div style={{fontSize:8,letterSpacing:3,color:"#00ffcc",opacity:0.35,marginBottom:4}}>GF(257) AI VM</div>
          <div style={{fontSize:9,color:"#2a4050",lineHeight:1.8,marginBottom:10}}>
            257 faces per cube: 256 vocab + 1 register<br/>
            data is the clock · the wire is the opcode
          </div>
          <div style={{display:"flex",gap:5}}>
            <button onClick={()=>simRef.current?.start()} disabled={running} style={{
              flex:2,background:running?"#060e14":"#001a10",
              border:`1px solid ${running?"#0a1a20":"#00ff8877"}`,
              color:running?"#0a2010":"#00ff88",
              padding:"9px",cursor:running?"not-allowed":"pointer",fontSize:9,letterSpacing:1,...mf}}>
              {running?"▶ RUNNING…":"▶ RUN"}</button>
            <button onClick={()=>simRef.current?.stop()} style={{background:"#180008",border:"1px solid #ff444433",color:"#ff4444",padding:"9px 10px",cursor:"pointer",fontSize:9,...mf}}>■</button>
            <button onClick={()=>simRef.current?.fire()} style={{background:"#001020",border:"1px solid #00ffcc22",color:"#00ffcc",padding:"9px 10px",cursor:"pointer",fontSize:9,...mf}}>~</button>
          </div>
        </div>

        {/* Program state */}
        <div style={{padding:"12px 16px",borderBottom:"1px solid #0a1422"}}>
          <div style={{fontSize:8,letterSpacing:2,color:"#1a3040",marginBottom:8}}>PROGRAM STATE</div>
          <div style={{display:"grid",gridTemplateColumns:"1fr 1fr",gap:6,marginBottom:8}}>
            {[
              {l:"iteration",v:`${progState.iteration} / ${DATASET.length}`,c:"#00ff88"},
              {l:"phase",    v:progState.phase,                             c:"#00ccff"},
              {l:"call stack",v:`depth ${progState.stackDepth}`,            c:"#00eeff"},
              {l:"tools",    v:toolCount,                                   c:"#cc44ff"},
            ].map(({l,v,c})=>(
              <div key={l} style={{background:"#030810",padding:"7px 9px"}}>
                <div style={{fontSize:7,color:"#0a1a28",marginBottom:2}}>{l}</div>
                <div style={{fontSize:10,color:c,fontWeight:"bold"}}>{v}</div>
              </div>
            ))}
          </div>
          <div style={{background:"#030810",padding:"8px 10px"}}>
            <div style={{fontSize:7,color:"#0a1a28",marginBottom:3}}>accumulated state</div>
            <div style={{fontSize:11,color:"#00ffcc"}}>f(p) = {affStr(progState.accumState)}</div>
            <div style={{display:"flex",gap:8,fontSize:8,color:"#0a2030",marginTop:2}}>
              <span>a={progState.accumState.a}</span>
              <span>b={progState.accumState.b}</span>
              <span style={{color:"#ff8800"}}>f256={face256Source(progState.accumState)}</span>
            </div>
            <div style={{position:"relative",width:"100%",height:18,background:"#020610",marginTop:6,border:"1px solid #0a1422"}}>
              <div style={{position:"absolute",
                left:`${(progState.accumState.a-1)/255*86+7}%`,
                top:"50%",width:5,height:5,borderRadius:"50%",
                background:"#00ffcc",boxShadow:"0 0 5px #00ffcc",
                transform:"translate(-50%,-50%)"}}/>
            </div>
          </div>
        </div>

        {/* Dataset */}
        <div style={{padding:"10px 16px",borderBottom:"1px solid #0a1422"}}>
          <div style={{fontSize:8,letterSpacing:2,color:"#1a3040",marginBottom:6}}>DATASET</div>
          <div style={{display:"flex",flexDirection:"column",gap:3}}>
            {DATASET.map((d,i)=>{
              const active=i===progState.iteration-1;
              const done=i<progState.iteration-1;
              return (
                <div key={i} style={{display:"flex",gap:6,alignItems:"center",
                  padding:"3px 7px",fontSize:8,
                  background:active?"#001a10":done?"#04080e":"#020610",
                  border:`1px solid ${active?"#00ff8855":done?"#0a2010":"#0a1422"}`,
                }}>
                  <span style={{color:active?"#00ff88":done?"#1a3040":"#0a1a28",minWidth:16}}>{i+1}.</span>
                  <span style={{color:active?"#c8e8f0":done?"#1a3040":"#0a1a28",flex:1}}>{DATASET_LABELS[i]}</span>
                  <span style={{color:active?"#00ffcc":"#0a1a28"}}>{affStr(d)}</span>
                </div>
              );
            })}
          </div>
        </div>

        {/* Selected node */}
        <div style={{padding:"10px 16px",borderBottom:"1px solid #0a1422",minHeight:80}}>
          <div style={{fontSize:8,letterSpacing:2,color:"#1a3040",marginBottom:5}}>
            {selected!==null?`N${selected} · ${nodeInfo?.label||""} · ${nodeInfo?.role||""}`:"CLICK A NODE"}
          </div>
          {nodeInfo&&selected!==null?(
            <>
              <div style={{fontSize:10,color:"#c8e8f0",marginBottom:3}}>
                f(p) = <span style={{color:"#00ffcc"}}>{affStr(nodeInfo.state)}</span>
              </div>
              <div style={{display:"flex",alignItems:"center",gap:6,marginBottom:4}}>
                <span style={{fontSize:8,color:"#1a3040"}}>face256</span>
                <div style={{width:8,height:8,borderRadius:"50%",
                  background:`hsl(${nodeInfo.f256/256*280},100%,60%)`,
                  boxShadow:`0 0 5px hsl(${nodeInfo.f256/256*280},100%,60%)`}}/>
                <span style={{fontSize:9,color:"#ff8800"}}>{nodeInfo.f256}</span>
              </div>
              <div style={{fontSize:8,color:"#0a2030",marginBottom:3}}>
                rotations: <span style={{color:"#3377ff"}}>{nodeInfo.rotCount}</span>
                {nodeInfo.rotHistory.length>0 && (
                  <span style={{color:"#1a3040",marginLeft:6}}>
                    [{nodeInfo.rotHistory.slice(-4).join(", ")}]
                  </span>
                )}
              </div>
              <div style={{fontSize:8,color:"#0a1a28"}}>
                ops: {nodeInfo.ops.map((op,i)=><span key={i} style={{color:OC(op),marginRight:3}}>{op}</span>)}
              </div>
            </>
          ):(
            <div style={{fontSize:9,color:"#060e18",lineHeight:1.9}}>inspect rotation state<br/>face 256 register · rotation history</div>
          )}
        </div>

        {/* Log */}
        <div style={{flex:1,padding:"10px 16px",overflow:"hidden",minHeight:0}}>
          <div style={{fontSize:8,letterSpacing:2,color:"#1a3040",marginBottom:5}}>LOG</div>
          {log.map((e,i)=>(
            <div key={e.id} style={{fontSize:9,lineHeight:1.85,color:e.color,opacity:Math.max(0.05,1-i*0.05)}}>{e.msg}</div>
          ))}
        </div>

        {/* Footer — wire geometry reference */}
        <div style={{padding:"7px 16px",borderTop:"1px solid #0a1422"}}>
          <div style={{fontSize:7,color:"#060e18",lineHeight:1.9,display:"flex",flexWrap:"wrap",gap:"0 12px"}}>
            {[
              {op:"ROTATE_X",label:"RX",note:"discrete X turn"},
              {op:"ROTATE_Y",label:"RY",note:"discrete Y turn"},
              {op:"ROTATE_Z",label:"RZ",note:"discrete Z turn"},
              {op:"COMPOSE",label:"∘",note:"carry state"},
              {op:"LOOP",label:"↺",note:"wire back"},
              {op:"CALL",label:"↓",note:"push chord"},
              {op:"RETURN",label:"↑",note:"pop chord"},
              {op:"SYNTH",label:"◆",note:"crystallize"},
            ].map(({op,label,note})=>(
              <span key={op}><span style={{color:OC(op)}}>{label}</span> {note}</span>
            ))}
          </div>
          <div style={{fontSize:6,color:"#060e14",marginTop:2}}>
            no switch · no timer · data is the clock · the wire is the opcode
          </div>
        </div>
      </div>
    </div>
  );
}
```