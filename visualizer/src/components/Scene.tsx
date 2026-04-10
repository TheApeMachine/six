import { useEffect, useRef, useState, useCallback } from 'react';
import { initEngine, type EngineCallbacks, type EngineStats } from '../lib/engine';
import type { VizEvent } from '../lib/wire';
import type {
  InspectorTarget, BeamState, ALUState, NodeState, TrieState,
  PipelineStageState,
} from '../lib/types';

interface LogEntry { html: string; ts: number }

export default function Scene() {
  const containerRef = useRef<HTMLDivElement>(null);
  const engineRef = useRef<ReturnType<typeof initEngine> | null>(null);

  const [stats, setStats] = useState<EngineStats | null>(null);
  const [connected, setConnected] = useState(false);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [inspectTarget, setInspectTarget] = useState<InspectorTarget | null>(null);
  const [nodeData, setNodeData] = useState<NodeState | null>(null);
  const [trieData, setTrieData] = useState<TrieState | null>(null);
  const [beamData, setBeamData] = useState<BeamState | null>(null);
  const [aluData, setALUData] = useState<ALUState | null>(null);
  const [pipeData, setPipeData] = useState<PipelineStageState | null>(null);
  const [paused, setPaused] = useState(false);
  const [showLog, setShowLog] = useState(true);
  const [showPrompt, setShowPrompt] = useState(false);
  const [promptText, setPromptText] = useState('');
  const [timelineCursor, setTimelineCursor] = useState(0);
  const [timelineTotal, setTimelineTotal] = useState(0);

  const inspectTargetRef = useRef(inspectTarget);
  inspectTargetRef.current = inspectTarget;
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const showPromptRef = useRef(showPrompt);
  showPromptRef.current = showPrompt;

  const onInspect = useCallback((target: InspectorTarget | null) => {
    setInspectTarget(target);
    if (!target) {
      setNodeData(null);
      setTrieData(null);
      setBeamData(null);
      setPipeData(null);
      return;
    }
    const eng = engineRef.current;
    if (!eng) return;

    if (target.kind === 'node') {
      setNodeData(eng.getNodeState(target.id));
      setBeamData(eng.getBeamState(target.id));
      setTrieData(null);
      setPipeData(null);
    } else if (target.kind === 'trie') {
      setNodeData(eng.getNodeState(target.id));
      setTrieData(eng.getTrieState(target.id, target.trieIndex ?? 0));
      setBeamData(null);
      setPipeData(null);
    } else if (target.kind === 'pipeline') {
      setPipeData(eng.getPipelineState(target.id));
      setNodeData(null);
      setTrieData(null);
      setBeamData(null);
    } else if (target.kind === 'beam') {
      setBeamData(eng.getBeamState(target.id));
      setNodeData(eng.getNodeState(target.id));
      setTrieData(null);
      setPipeData(null);
    } else {
      setNodeData(null);
      setTrieData(null);
      setBeamData(null);
      setPipeData(null);
    }
  }, []);

  useEffect(() => {
    if (!containerRef.current) return;

    const cbs: EngineCallbacks = {
      onEvent: (_ev: VizEvent) => {
        const tgt = inspectTargetRef.current;
        if (tgt && engineRef.current) {
          const eng = engineRef.current;
          if (tgt.kind === 'node') setNodeData(eng.getNodeState(tgt.id));
          if (tgt.kind === 'trie') setTrieData(eng.getTrieState(tgt.id, tgt.trieIndex ?? 0));
        }
      },
      onInspect,
      onStats: (s) => setStats(s),
      onLog: (html) => setLogs((prev) => [{ html, ts: Date.now() }, ...prev].slice(0, 32)),
      onBeamUpdate: (_nodeId, beam) => {
        const tgt = inspectTargetRef.current;
        if (tgt?.kind === 'node' || tgt?.kind === 'beam') {
          setBeamData(beam);
        }
      },
      onALUUpdate: (a) => setALUData(a),
      onConnectionChange: (c) => setConnected(c),
      onTimelineUpdate: (cursor, total) => {
        setTimelineCursor(cursor);
        setTimelineTotal(total);
      },
    };

    const engine = initEngine(containerRef.current, cbs);
    engineRef.current = engine;

    const handleKey = (e: KeyboardEvent) => {
      if (e.key === ' ' && !showPromptRef.current) {
        e.preventDefault();
        const p = engine.togglePause();
        setPaused(p);
      } else if (e.key === 'l') {
        setShowLog((v) => !v);
      } else if (e.key === 'p') {
        setShowPrompt((v) => !v);
      } else if (e.key === 'Escape') {
        setShowPrompt(false);
        engine.closeInspector();
        setInspectTarget(null);
        setNodeData(null);
        setTrieData(null);
        setBeamData(null);
        setPipeData(null);
      } else if (e.key === 'ArrowRight' && pausedRef.current) {
        engine.stepForward();
      } else if (e.key === 'ArrowLeft' && pausedRef.current) {
        engine.stepBackward();
      }
    };
    window.addEventListener('keydown', handleKey);

    return () => {
      window.removeEventListener('keydown', handleKey);
      engine.destroy();
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  function submitPrompt() {
    if (promptText.trim() && engineRef.current) {
      engineRef.current.sendPrompt(promptText.trim());
      setPromptText('');
      setShowPrompt(false);
    }
  }

  function closeInspector() {
    engineRef.current?.closeInspector();
    setInspectTarget(null);
    setNodeData(null);
    setTrieData(null);
    setBeamData(null);
    setPipeData(null);
  }

  return (
    <div className="relative w-full h-screen bg-[#0a0e1a] overflow-hidden font-mono text-[#c8d6e5]">
      <div ref={containerRef} className="absolute inset-0" />

      {/* ── Top bar ─────────────────────────────────────────── */}
      <div className="absolute top-0 left-0 right-0 z-10 flex items-center justify-between px-3 py-1.5 bg-[#0a0e1a]/80 border-b border-[#1b2a3a] backdrop-blur-sm">
        <div className="flex items-center gap-3">
          <h1 className="text-[12px] text-[#4cc9f0] tracking-widest">SIX — OBSERVABILITY</h1>
          <span className={`w-2 h-2 rounded-full ${connected ? 'bg-[#76ff03]' : 'bg-[#f44336]'} animate-pulse`} />
          <span className="text-[9px] opacity-40">{connected ? 'ws:6600' : 'disconnected'}</span>
        </div>

        {stats && (
          <div className="flex items-center gap-4 text-[9px] opacity-60">
            <span>nodes <b className="text-[#ffab00]">{stats.nodeCount}</b></span>
            <span>tries <b className="text-[#e6c930]">{stats.trieCount}</b></span>
            <span>edges <b className="text-[#4cc9f0]">{stats.edgeCount}</b></span>
            <span>events <b className="text-[#76ff03]">{stats.eventCount}</b></span>
            {stats.droppedCount > 0 && <span className="text-[#f44336]">dropped {stats.droppedCount}</span>}
            <span>{stats.fps} fps</span>
            <span>{stats.eventsPerSec} ev/s</span>
          </div>
        )}

        <div className="flex items-center gap-2">
          <button type="button" onClick={() => setShowLog((v) => !v)} className="text-[9px] px-2 py-0.5 border border-[#1b3a5a] hover:bg-[#1b3a5a]/50 transition-colors">{showLog ? 'hide log' : 'show log'}</button>
          <button type="button" onClick={() => setShowPrompt((v) => !v)} className="text-[9px] px-2 py-0.5 border border-[#1b3a5a] hover:bg-[#1b3a5a]/50 transition-colors">prompt</button>
          <button type="button" onClick={() => { const p = engineRef.current?.togglePause(); setPaused(!!p); }} className={`text-[9px] px-2 py-0.5 border transition-colors ${paused ? 'border-[#ffab00] text-[#ffab00]' : 'border-[#1b3a5a] hover:bg-[#1b3a5a]/50'}`}>
            {paused ? '\u25B6 play' : '\u23F8 pause'}
          </button>
        </div>
      </div>

      {/* ── Timeline bar ────────────────────────────────────── */}
      <div className="absolute bottom-0 left-0 right-0 z-10 h-5 bg-[#0a0e1a]/90 border-t border-[#1b2a3a] flex items-center px-2 gap-2">
        <span className="text-[8px] opacity-40 w-16">{timelineCursor}/{timelineTotal}</span>
        <button type="button" className="flex-1 h-1 bg-[#1b2a3a] rounded-full relative cursor-pointer border-0 p-0"
          onClick={(e) => {
            if (timelineTotal === 0) return;
            const rect = e.currentTarget.getBoundingClientRect();
            const pct = (e.clientX - rect.left) / rect.width;
            engineRef.current?.scrubTo(Math.floor(pct * timelineTotal));
          }}>
          <div className="h-full bg-[#4cc9f0]/50 rounded-full transition-all" style={{ width: timelineTotal > 0 ? `${(timelineCursor / timelineTotal) * 100}%` : '0%' }} />
        </button>
        {paused && <span className="text-[8px] text-[#ffab00] animate-pulse">PAUSED</span>}
      </div>

      {/* ── Log panel ───────────────────────────────────────── */}
      {showLog && (
        <div className="absolute bottom-6 left-3 z-10 text-[9px] bg-[#0a0e1a]/85 border border-[#1b2a3a] p-2 min-w-[340px] max-h-[200px] overflow-y-auto backdrop-blur-sm">
          <div className="text-[10px] text-[#4cc9f0] mb-1 tracking-wider">Event Log</div>
          <div className="flex flex-col gap-0.5 text-[#5a8aaa]">
            {logs.map((log, i) => (
              <div key={i} className="flex gap-2">
                <span className="opacity-30 shrink-0">{new Date(log.ts).toLocaleTimeString()}</span>
                <span dangerouslySetInnerHTML={{ __html: log.html }} />
              </div>
            ))}
            {logs.length === 0 && <div className="opacity-30">waiting for events...</div>}
          </div>
        </div>
      )}

      {/* ── Prompt panel ────────────────────────────────────── */}
      {showPrompt && (
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 z-30 bg-[#0a0e1a]/95 border border-[#2a4a6a] p-4 min-w-[500px] backdrop-blur-md shadow-2xl">
          <div className="text-[11px] text-[#4cc9f0] mb-2 tracking-wider">Send Prompt</div>
          <input
            type="text"
            value={promptText}
            onChange={(e) => setPromptText(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && submitPrompt()}
            className="w-full bg-[#0d1520] border border-[#1b3a5a] text-[12px] text-[#c8d6e5] px-3 py-2 outline-none focus:border-[#4cc9f0] transition-colors"
            placeholder="Type a prompt..."
            // eslint-disable-next-line jsx-a11y/no-autofocus
            autoFocus
          />
          <div className="flex justify-end gap-2 mt-2">
            <button type="button" onClick={() => setShowPrompt(false)} className="text-[9px] px-3 py-1 border border-[#1b3a5a] hover:bg-[#1b3a5a]/50">cancel</button>
            <button type="button" onClick={submitPrompt} className="text-[9px] px-3 py-1 border border-[#4cc9f0] text-[#4cc9f0] hover:bg-[#4cc9f0]/10">send</button>
          </div>
        </div>
      )}

      {/* ── ALU panel (always visible when data exists) ──────── */}
      {aluData && aluData.totalDispatches > 0 && (
        <div className="absolute top-12 left-3 z-10 text-[10px] bg-[#0a0e1a]/90 border border-[#1b2a3a] p-2.5 min-w-[220px] backdrop-blur-sm shadow-lg shadow-black/50">
          <h3 className="text-[#26c6da] text-[11px] mb-1 tracking-wider">ALU Pipeline</h3>
          <div className="flex justify-between"><span>total dispatches</span><span className="text-[#76ff03]">{aluData.totalDispatches}</span></div>
          <div className="mt-1 border-t border-[#1b2a3a] pt-1">
            {Object.entries(aluData.substrates).map(([name, sub]) => (
              <div key={name} className="mb-1">
                <div className="flex justify-between">
                  <span className={name === 'cpu' ? 'text-[#26c6da]' : name === 'cuda' ? 'text-[#66bb6a]' : 'text-[#bdbdbd]'}>{name}</span>
                  <span>{sub.inflight} inf</span>
                </div>
                <div className="flex justify-between text-[9px] opacity-60">
                  <span>{sub.totalDispatches} disp</span>
                  <span>{(sub.emaDurationNs / 1000).toFixed(1)}{'\u00B5'}s EMA</span>
                </div>
                <div className="h-0.5 bg-[#1b2a3a] mt-0.5 rounded-full overflow-hidden">
                  <div className="h-full bg-[#26c6da]/40 transition-all" style={{ width: `${Math.min(100, (sub.inflight / 8) * 100)}%` }} />
                </div>
              </div>
            ))}
          </div>

          {/* Recent ALU ops */}
          {aluData.recentOps.length > 0 && (
            <div className="mt-1 border-t border-[#1b2a3a] pt-1">
              <div className="text-[9px] opacity-40 mb-0.5">recent ops</div>
              {aluData.recentOps.slice(-6).reverse().map((op, i) => (
                <div key={i} className="text-[8px] flex gap-1 opacity-70">
                  <span className={op.substrate === 'cpu' ? 'text-[#26c6da]' : op.substrate === 'cuda' ? 'text-[#66bb6a]' : 'text-[#bdbdbd]'}>{op.substrate}</span>
                  <span>0x{op.opcode.toString(16)}</span>
                  <span className="text-[#76ff03]">{(op.durationNs / 1000).toFixed(1)}{'\u00B5'}s</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ── Inspector panel (right) ─────────────────────────── */}
      {inspectTarget && (
        <div className="absolute top-10 right-3 z-20 bg-[#0a0e1a]/95 border border-[#2a4a6a] p-3 min-w-[320px] max-w-[400px] max-h-[calc(100vh-80px)] overflow-y-auto backdrop-blur-md shadow-2xl shadow-black/80 text-[11px]">
          <button
            type="button"
            className="absolute top-2 right-3 text-[#5a7a9a] hover:text-white text-sm"
            onClick={closeInspector}
          >{'\u2715'}</button>

          <h2 className="text-[#ffab00] text-[14px] tracking-wider mb-2">
            {inspectTarget.kind === 'node' && inspectTarget.id}
            {inspectTarget.kind === 'trie' && `${inspectTarget.id} / T${inspectTarget.trieIndex}`}
            {inspectTarget.kind === 'pipeline' && inspectTarget.id}
            {inspectTarget.kind === 'algorithm' && inspectTarget.id}
          </h2>

          {/* ─── Node inspector ─── */}
          {inspectTarget.kind === 'node' && nodeData && (
            <div className="space-y-2">
              <Section title="finite-field layers">
                <Row k="DHT mesh" v="GF(65537)" color="#a868e8" />
                <Row k="this node" v="GF(8191)" color="#e89850" />
                <Row k="tries" v="GF(257) — per-column" color="#40c8a0" />
              </Section>

              <Section title="field digest">
                {(['surprisal', 'entropy', 'growth'] as const).map((key) => {
                  const val = nodeData.digest[key];
                  const pct = Math.min(Math.abs(val) / 10, 1) * 100;
                  const barColor = key === 'surprisal' ? (val > 5 ? '#c04040' : val > 2 ? '#c08030' : '#408060') : '#4a80c0';
                  return (
                    <div key={key}>
                      <Row k={key} v={val.toFixed(4)} />
                      <div className="h-0.5 bg-[#1b2a3a] rounded-full overflow-hidden mt-0.5">
                        <div className="h-full transition-all" style={{ width: `${pct}%`, background: barColor }} />
                      </div>
                    </div>
                  );
                })}
              </Section>

              {Object.keys(nodeData.pressure).length > 0 && (
                <Section title="field pressure">
                  {Object.entries(nodeData.pressure).map(([k, v]) => (
                    <Row key={k} k={k} v={typeof v === 'number' ? v.toFixed(6) : String(v)} />
                  ))}
                </Section>
              )}

              <Section title="activity">
                <Row k="inserts" v={String(nodeData.insertCount)} />
                <Row k="predictions" v={String(nodeData.predictCount)} />
                <Row k="gossip" v={String(nodeData.gossipCount)} />
                <Row k="tries" v={String(nodeData.trieCount)} />
              </Section>

              {Object.keys(nodeData.latencies).length > 0 && (
                <Section title="peer latencies">
                  {Object.entries(nodeData.latencies).map(([peer, ms]) => (
                    <Row key={peer} k={peer.substring(0, 16)} v={`${ms.toFixed(1)}ms`}
                      color={ms > 50 ? '#c04040' : ms > 10 ? '#c08030' : '#408060'} />
                  ))}
                </Section>
              )}

              {Object.keys(nodeData.labelCounts).length > 0 && (
                <Section title="label distribution">
                  {Object.entries(nodeData.labelCounts).sort((a, b) => b[1] - a[1]).map(([label, count]) => {
                    const total = Object.values(nodeData.labelCounts).reduce((s, v) => s + v, 0);
                    const pct = ((count / total) * 100).toFixed(1);
                    return (
                      <div key={label}>
                        <Row k={label} v={`${count} (${pct}%)`} />
                        <div className="h-0.5 bg-[#1b2a3a] rounded-full overflow-hidden mt-0.5">
                          <div className="h-full bg-[#7050a0] transition-all" style={{ width: `${pct}%` }} />
                        </div>
                      </div>
                    );
                  })}
                </Section>
              )}

              {nodeData.recentSequences.length > 0 && (
                <Section title="recent sequences">
                  {[...nodeData.recentSequences].reverse().slice(0, 8).map((seq, i) => (
                    <div key={i} className="text-[10px] text-[#5a8aaa] py-0.5 border-b border-[#1b2a3a]/30 break-all">{seq}</div>
                  ))}
                </Section>
              )}

              {/* Beam search section inside node inspector */}
              {beamData && (beamData.activeCount > 0 || beamData.converged) && (
                <Section title="beam search">
                  <Row k="active hypotheses" v={String(beamData.activeCount)} color="#408060" />
                  <Row k="rejected" v={String(beamData.rejectedCount)} color="#c04040" />
                  <Row k="best score" v={beamData.bestScore.toFixed(4)} color="#c08030" />
                  {beamData.converged && (
                    <Row k="converged" v={beamData.lastSequence || 'yes'} color="#76ff03" />
                  )}
                  {beamData.hypotheses.length > 0 && (
                    <div className="mt-1">
                      <div className="text-[9px] opacity-40 mb-0.5">hypotheses</div>
                      {beamData.hypotheses.slice(0, 6).map((hyp, i) => (
                        <div key={i} className="text-[9px] flex justify-between py-0.5 border-b border-[#1b2a3a]/20">
                          <span className="text-[#c8d6e5] break-all max-w-[200px]">{hyp.tokens}</span>
                          <span className={hyp.score > -5 ? 'text-[#76ff03]' : 'text-[#c04040]'}>{hyp.score.toFixed(3)}</span>
                        </div>
                      ))}
                    </div>
                  )}
                  {/* Beam progress bar */}
                  <div className="mt-1 h-1 bg-[#1b2a3a] rounded-full overflow-hidden">
                    <div
                      className="h-full bg-gradient-to-r from-[#ff6e40] to-[#ffab00] transition-all duration-300"
                      style={{ width: `${Math.min(100, Math.max(5, (beamData.bestScore + 15) / 15 * 100))}%` }}
                    />
                  </div>
                </Section>
              )}
            </div>
          )}

          {/* ─── Trie inspector ─── */}
          {inspectTarget.kind === 'trie' && trieData && (
            <div className="space-y-2">
              <Section title="trie state">
                <Row k="index" v={`T${trieData.index}`} />
                <Row k="surprisal" v={trieData.surprisal.toFixed(4)} color={trieData.surprisal > 5 ? '#c04040' : trieData.surprisal > 2 ? '#c08030' : '#408060'} />
                <Row k="entropy" v={trieData.entropy.toFixed(4)} />
                <Row k="growth" v={`${trieData.growth >= 0 ? '+' : ''}${trieData.growth.toFixed(4)}`} color={trieData.growth > 0 ? '#408060' : '#c04060'} />
                <Row k="decay mul" v={`x${trieData.decayMul.toFixed(3)}`} color="#c04060" />
                <Row k="learn mul" v={`x${trieData.learnMul.toFixed(3)}`} color="#408060" />
                <Row k="aligned" v={trieData.aligned ? 'yes' : 'no'} color={trieData.aligned ? '#c08030' : '#3a5878'} />
              </Section>

              {inspectTarget.vertexVid !== undefined && trieData.graphPayload && (
                <Section title="selected vertex">
                  {(() => {
                    const v = trieData.graphPayload.vertices.find((vt) => vt.vid === inspectTarget.vertexVid);
                    if (!v) return <div className="text-[#3a5878]">Waiting for snapshot...</div>;
                    return (
                      <>
                        <Row k="vid" v={String(v.vid)} />
                        <Row k="depth" v={String(v.depth)} />
                        <Row k="visits" v={String(v.visits)} />
                        <Row k="token" v={v.token || '\u2014'} />
                        <Row k="value_id" v={String(v.value_id)} />
                        {trieData.graphPayload.edges.filter((e) => e.to === v.vid).length > 0 && (
                          <div className="mt-1">
                            <div className="text-[9px] opacity-40">incoming</div>
                            {trieData.graphPayload.edges.filter((e) => e.to === v.vid).map((e, i) => (
                              <div key={i} className="text-[9px]">{'\u2190'} {e.token} (from {e.from})</div>
                            ))}
                          </div>
                        )}
                        {trieData.graphPayload.edges.filter((e) => e.from === v.vid).length > 0 && (
                          <div className="mt-1">
                            <div className="text-[9px] opacity-40">outgoing</div>
                            {trieData.graphPayload.edges.filter((e) => e.from === v.vid).map((e, i) => (
                              <div key={i} className="text-[9px]">{'\u2192'} {e.token} (to {e.to})</div>
                            ))}
                          </div>
                        )}
                        {trieData.graphPayload.truncated && (
                          <div className="text-[#c08030] text-[9px] mt-1">Snapshot truncated server-side</div>
                        )}
                      </>
                    );
                  })()}
                </Section>
              )}

              {nodeData && (
                <Section title="parent node">
                  <Row k="id" v={nodeData.id} />
                  <Row k="inserts" v={String(nodeData.insertCount)} />
                  <Row k="predictions" v={String(nodeData.predictCount)} />
                </Section>
              )}
            </div>
          )}

          {/* ─── Pipeline inspector ─── */}
          {inspectTarget.kind === 'pipeline' && pipeData && (
            <div className="space-y-2">
              <Section title="overview">
                <Row k="component" v={pipeData.id} color="#4cc9f0" />
                <Row k="total events" v={String(pipeData.totalEvents)} />
                {pipeData.bytesProcessed > 0 && <Row k="bytes" v={`${(pipeData.bytesProcessed / 1024).toFixed(1)} KiB`} />}
                {pipeData.inflight > 0 && <Row k="inflight" v={String(pipeData.inflight)} />}
                {pipeData.emaDurationMs > 0 && <Row k="EMA duration" v={`${pipeData.emaDurationMs.toFixed(2)}ms`} />}
              </Section>

              {pipeData.recentOps.length > 0 && (
                <Section title="recent operations">
                  {pipeData.recentOps.slice(-8).reverse().map((op, i) => (
                    <div key={i} className="text-[10px] text-[#5a8aaa] py-0.5 break-all">{op}</div>
                  ))}
                </Section>
              )}

              {/* Show ALU detail for compute substrates */}
              {['cpu', 'cuda', 'metal'].includes(pipeData.id) && aluData && (
                <Section title="substrate detail">
                  {(() => {
                    const sub = aluData.substrates[pipeData.id];
                    if (!sub) return null;
                    return (
                      <>
                        <Row k="dispatches" v={String(sub.totalDispatches)} />
                        <Row k="inflight" v={String(sub.inflight)} />
                        <Row k="last" v={`${(sub.lastDurationNs / 1000).toFixed(1)}\u00B5s`} />
                        <Row k="EMA" v={`${(sub.emaDurationNs / 1000).toFixed(1)}\u00B5s`} />
                        <div className="h-1 bg-[#1b2a3a] rounded-full overflow-hidden mt-1">
                          <div className="h-full bg-[#4a80c0] transition-all" style={{ width: `${Math.min(100, (sub.inflight / 8) * 100)}%` }} />
                        </div>
                      </>
                    );
                  })()}
                </Section>
              )}
            </div>
          )}

          {/* ─── Algorithm inspector ─── */}
          {inspectTarget.kind === 'algorithm' && (
            <div className="space-y-2">
              <Section title="algorithm component">
                <Row k="name" v={inspectTarget.id} color="#76ff03" />
                <div className="text-[9px] text-[#5a8aaa] mt-1">
                  Click a DHT node to inspect its algorithm stack state.
                  This component runs inside each node's algo.Stack.
                </div>
              </Section>
            </div>
          )}
        </div>
      )}

      {/* ── Controls help ───────────────────────────────────── */}
      <div className="absolute bottom-6 right-3 z-10 text-[9px] opacity-30 text-right leading-relaxed pointer-events-none">
        drag {'\u2192'} orbit {'\u00B7'} scroll {'\u2192'} zoom {'\u00B7'} click {'\u2192'} inspect<br />
        space {'\u2192'} pause {'\u00B7'} l {'\u2192'} log {'\u00B7'} p {'\u2192'} prompt {'\u00B7'} esc {'\u2192'} close
      </div>
    </div>
  );
}

/* ── Shared inspector sub-components ─────────────────────────── */

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="border-b border-[#1b2a3a] pb-1.5">
      <h4 className="text-[10px] text-[#5a7a9a] tracking-wider mb-0.5">{title}</h4>
      {children}
    </div>
  );
}

function Row({ k, v, color }: { k: string; v: string; color?: string }) {
  return (
    <div className="flex justify-between text-[10px]">
      <span className="text-[#7a9ab8]">{k}</span>
      <span style={color ? { color } : undefined} className={color ? '' : 'text-[#c8d6e5]'}>{v}</span>
    </div>
  );
}
