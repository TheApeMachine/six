import * as THREE from 'three';
import { scene } from './scene.js';
import { state } from './state.js';
import { EK, EDGE_COLORS, MAX_VIZ_TRIE_VISUALS } from './constants.js';
import { lerpColor } from './utils.js';
import {
  createNode, repositionNodes, renderNodeStats, tickNodeActivity, pulseNode,
} from './nodes.js';
import {
  addTrieVisual, applyTrieGraphSnapshot, removeLastTrieVisual, updateTrieCouplingArc, updateTrieAppearance,
} from './tries.js';
import { addEdge, updateEdgeLabel, pulseEdge } from './peers.js';
import { updateFieldArc, pulseFieldArc, showEigenmodeCluster } from './field.js';
import { spawnFloater, spawnEdgeParticle } from './fx.js';
import { renderComputePanel } from './hud.js';
import { refreshInspector } from './inspector.js';
import {
  pulseStage, tickStageActivity, addStageOp, spawnPipelineFlowParticle, renderStageStats,
} from './pipeline.js';

/*
pipelineEvent is a shorthand that pulses a pipeline stage, increments its
event counter, logs an operation, ticks the sparkline, and redraws the stats.
*/
function pipelineEvent(stageId, activity, opText) {
  const stage = state.pipeline.stages.get(stageId);
  if (!stage) return;
  stage.metrics.totalEvents++;
  stage.glow = Math.min(stage.glow + 0.3, 1.0);
  pulseStage(stageId);
  tickStageActivity(stageId, activity);
  if (opText) addStageOp(stageId, opText);
  renderStageStats(stage);
}

export function applyEvent(ev) {
  switch (ev.kind) {
    case EK.NodeCreated:
      createNode(ev.src, ev.lbl);
      break;

    case EK.NodeUpdated: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      if (ev.vals?.trie_count !== undefined) {
        const newCount = Math.min(Math.floor(ev.vals.trie_count), MAX_VIZ_TRIE_VISUALS);
        while (node.tries.length > newCount) removeLastTrieVisual(ev.src);
        while (node.tries.length < newCount) addTrieVisual(ev.src);
        node.data.trieCount = newCount;
        renderNodeStats(node);
      }
      Object.assign(node.data.vals, ev.vals || {});
      break;
    }

    case EK.NodeRemoved: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      scene.remove(node.group);
      for (const eid of node.edges) {
        const e = state.edges.get(eid);
        if (e) {
          scene.remove(e.mesh);
          scene.remove(e.labelSprite);
          state.edges.delete(eid);
        }
      }
      state.nodes.delete(ev.src);
      repositionNodes();
      updateStats();
      break;
    }

    case EK.PeerAdded:
      if (!state.nodes.has(ev.src)) createNode(ev.src, ev.src);
      if (!state.nodes.has(ev.tgt)) createNode(ev.tgt, ev.tgt);
      addEdge(ev.src, ev.tgt);
      break;

    case EK.PeerLatency: {
      pulseEdge(ev.src, ev.tgt, EDGE_COLORS.latency);
      const eid = ev.src < ev.tgt ? `${ev.src}|${ev.tgt}` : `${ev.tgt}|${ev.src}`;
      const edge = state.edges.get(eid);
      if (edge) {
        edge.latencyMs = ev.vals?.latency_ms || 0;
        updateEdgeLabel(edge);
      }
      const nA = state.nodes.get(ev.src);
      const nB = state.nodes.get(ev.tgt);
      if (nA) nA.data.latencies[ev.tgt] = edge?.latencyMs || 0;
      if (nB) nB.data.latencies[ev.src] = edge?.latencyMs || 0;
      break;
    }

    case EK.ValuePublished: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.insertCount++;
      tickNodeActivity(ev.src, 1);
      pulseNode(ev.src);
      if (ev.lbl) {
        node.data.labelCounts[ev.lbl] = (node.data.labelCounts[ev.lbl] || 0) + 1;
      }
      const pos = node.group.position.clone();
      pos.y += 1;
      spawnFloater(pos, ev.lbl || 'publish', '#c04060');
      renderNodeStats(node);
      break;
    }

    case EK.ValueReplicated: {
      const nA = state.nodes.get(ev.src);
      const nB = state.nodes.get(ev.tgt);
      if (nA && nB) {
        spawnEdgeParticle(ev.src, ev.tgt, EDGE_COLORS.replication);
        const eid = ev.src < ev.tgt ? `${ev.src}|${ev.tgt}` : `${ev.tgt}|${ev.src}`;
        const edge = state.edges.get(eid);
        if (edge) {
          edge.replicationCount++;
          edge.lastFlowType = 'replication';
          updateEdgeLabel(edge);
        }
      }
      break;
    }

    case EK.GossipSent: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.gossipCount++;
      tickNodeActivity(ev.src, 0.5);
      for (const eid of node.edges) {
        const e = state.edges.get(eid);
        if (!e) continue;
        const peerId = e.from === ev.src ? e.to : e.from;
        spawnEdgeParticle(ev.src, peerId, EDGE_COLORS.gossip);
        e.gossipCount++;
        e.lastFlowType = 'gossip';
        updateEdgeLabel(e);
        pulseEdge(ev.src, peerId, EDGE_COLORS.gossip);
      }
      renderNodeStats(node);
      break;
    }

    case EK.GossipReceived: {
      const nA = state.nodes.get(ev.tgt);
      const nB = state.nodes.get(ev.src);
      if (nA && nB) {
        pulseEdge(ev.src, ev.tgt, EDGE_COLORS.gossip);
        spawnEdgeParticle(ev.src, ev.tgt, EDGE_COLORS.gossip);
        tickNodeActivity(ev.tgt, 0.5);
        pulseNode(ev.tgt);
      }
      break;
    }

    case EK.FieldDigest: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.digest = ev.vals || {};
      const s = ev.vals?.surprisal || 0;
      const t = Math.min(s / 8, 1);
      node.core.material.color.setHex(lerpColor(0x408060, 0xc04040, t));
      node.wire.material.color.setHex(lerpColor(0x203858, 0x603030, t));
      node.pulseRing.material.color.setHex(lerpColor(0x408060, 0xc04040, t));
      tickNodeActivity(ev.src, 0.3);
      renderNodeStats(node);
      break;
    }

    case EK.EigenmodeDetected: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const energy = ev.vals?.dominant_energy || 0;
      const modeCount = ev.vals?.mode_count || 0;
      if (energy > 0) {
        node.wire.material.color.setHex(0xc08030);
        node.wire.material.opacity = 0.5;
        const pos = node.group.position.clone();
        pos.y += 3.5;
        spawnFloater(pos, `eigenmode x${modeCount} E=${energy.toFixed(2)}`, '#c08030');
        setTimeout(() => { node.wire.material.opacity = 0.15; }, 600);
        showEigenmodeCluster(ev.src, modeCount, energy);
      }
      break;
    }

    case EK.FieldPressure: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.pressure = ev.vals || {};
      const learn = ev.vals?.learning || 0;
      const decay = ev.vals?.decay || 0;
      node.targetPos.y = Math.max(-3, Math.min(3, (learn - decay) * 3));
      const pressure = Math.abs(learn) + Math.abs(decay);
      for (const eid of node.edges) {
        const e = state.edges.get(eid);
        if (!e) continue;
        const peerId = e.from === ev.src ? e.to : e.from;
        updateFieldArc(ev.src, peerId, Math.min(pressure * 2, 1));
        pulseFieldArc(ev.src, peerId);
      }
      renderNodeStats(node);
      break;
    }

    case EK.TrieInsert: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.insertCount++;
      tickNodeActivity(ev.src, 1);
      pulseNode(ev.src);
      const seq = ev.meta?.sequence || '';
      if (seq) {
        node.data.recentSequences.push(seq);
        if (node.data.recentSequences.length > 10) node.data.recentSequences.shift();
        const pos = node.group.position.clone();
        pos.y -= 1;
        const display = seq.length > 30 ? `${seq.slice(0, 30)}...` : seq;
        spawnFloater(pos, display, '#408060', new THREE.Vector3(0, -0.02, 0));
      }
      if (ev.lbl) {
        node.data.labelCounts[ev.lbl] = (node.data.labelCounts[ev.lbl] || 0) + 1;
      }
      let flashTrie = null;
      const tIdx = ev.vals?.trie_idx;
      if (tIdx !== undefined && tIdx >= 0 && Math.floor(tIdx) < node.tries.length) {
        flashTrie = node.tries[Math.floor(tIdx)];
      } else if (node.tries.length > 0) {
        flashTrie = node.tries[Math.floor(Math.random() * node.tries.length)];
      }
      if (flashTrie) flashTrie.insertFlash = 1.0;
      renderNodeStats(node);
      break;
    }

    case EK.TriePredict:
    case EK.TrieClassify: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      node.data.predictCount++;
      tickNodeActivity(ev.src, 0.8);
      pulseNode(ev.src);
      const conf = ev.vals?.confidence;
      const txt = ev.lbl + (conf !== undefined ? ` (${(conf*100).toFixed(0)}%)` : '');
      const pos = node.group.position.clone();
      pos.y += 2;
      spawnFloater(pos, txt, '#c08030');
      renderNodeStats(node);
      break;
    }

    case EK.TrieExperience: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const pos = node.group.position.clone();
      pos.y -= 0.5;
      spawnFloater(pos, `exp s=${(ev.vals?.surprisal||0).toFixed(2)}`, '#305848');
      tickNodeActivity(ev.src, 0.3);
      break;
    }

    case EK.AdaptiveUpdate: {
      const node = state.nodes.get(ev.src);
      if (node) {
        Object.assign(node.data.vals, ev.vals || {});
        tickNodeActivity(ev.src, 0.2);
      }
      break;
    }

    case EK.PoolSchedule: {
      const name = ev.lbl || 'unknown';
      const inflight = ev.vals?.queue_size || 0;
      if (!state.compute.substrates[name]) {
        state.compute.substrates[name] = { inflight: 0, lastDurationMs: 0, totalDispatches: 0, emaDurationMs: 0 };
      }
      state.compute.substrates[name].inflight = inflight;
      state.compute.substrates[name].totalDispatches++;
      state.compute.totalDispatches++;
      state.compute.recentActions.push(`-> ${name} (inflight:${inflight})`);
      if (state.compute.recentActions.length > 14) state.compute.recentActions.shift();
      renderComputePanel();

      pipelineEvent('queue', 1, `sched ${name} inf=${inflight}`);
      break;
    }

    case EK.PoolComplete: {
      const name = ev.lbl || 'unknown';
      const durationMs = ev.vals?.duration_ms || 0;
      if (!state.compute.substrates[name]) {
        state.compute.substrates[name] = { inflight: 0, lastDurationMs: 0, totalDispatches: 0, emaDurationMs: 0 };
      }
      const s = state.compute.substrates[name];
      s.inflight = Math.max(0, s.inflight - 1);
      s.lastDurationMs = durationMs;
      s.emaDurationMs = s.emaDurationMs === 0 ? durationMs : s.emaDurationMs + (durationMs - s.emaDurationMs) * 0.125;
      state.compute.recentActions.push(`ok ${name} ${durationMs}ms`);
      if (state.compute.recentActions.length > 14) state.compute.recentActions.shift();
      renderComputePanel();

      pipelineEvent('queue', 0.5, `done ${name} ${durationMs}ms`);
      break;
    }

    case EK.CompilerCompile: {
      const op = Math.floor(ev.vals?.operation || 0);
      const ns = ev.vals?.compile_ns || 0;
      const depth = Math.floor(ev.vals?.finalizer_depth || 0);
      const batch = (ev.vals?.batch_affinity || 0) > 0;
      const corr = ev.meta?.correlation || '';
      const target = ev.lbl || 'cpu';
      const batchS = batch ? ' batch' : '';
      const logLine = `compile ${target} op=0x${op.toString(16)} ${(ns / 1000).toFixed(1)}µs fin=${depth}${batchS}${corr ? ` #${corr}` : ''}`;
      state.compute.recentActions.push(logLine);
      if (state.compute.recentActions.length > 14) state.compute.recentActions.shift();
      renderComputePanel();

      pipelineEvent('backend', 1, logLine);
      spawnPipelineFlowParticle('backend', 'queue', 0x806030);
      break;
    }

    case EK.ALUDispatch: {
      const op = Math.floor(ev.vals?.opcode || 0);
      const ms = ev.vals?.duration_ms || 0;
      const sub = ev.lbl || '?';
      const corr = ev.meta?.correlation || '';
      const logLine = `ALU ${sub} op=0x${op.toString(16)} ${ms}ms${corr ? ` #${corr}` : ''}`;
      state.compute.recentActions.push(logLine);
      if (state.compute.recentActions.length > 14) state.compute.recentActions.shift();
      renderComputePanel();

      const subStageId = sub === 'metal' ? 'metal' : sub === 'cuda' ? 'cuda' : 'cpu';
      pipelineEvent(subStageId, 1, logLine);
      const subStage = state.pipeline.stages.get(subStageId);
      if (subStage) {
        subStage.metrics.lastDurationMs = ms;
        subStage.metrics.emaDurationMs = subStage.metrics.emaDurationMs === 0
          ? ms : subStage.metrics.emaDurationMs + (ms - subStage.metrics.emaDurationMs) * 0.125;
      }
      pipelineEvent('backend', 0.5, logLine);
      spawnPipelineFlowParticle('queue', subStageId, 0x7050a0);
      break;
    }

    case EK.FinalizerRun: {
      const depth = Math.floor(ev.vals?.depth || 0);
      const emitted = Math.floor(ev.vals?.emitted || 0);
      const err = (ev.vals?.error || 0) > 0;
      const corr = ev.meta?.correlation || '';
      const logLine = `finalize d=${depth} out=${emitted}${err ? ' ERR' : ''}${corr ? ` #${corr}` : ''}`;
      state.compute.recentActions.push(logLine);
      if (state.compute.recentActions.length > 14) state.compute.recentActions.shift();
      renderComputePanel();

      pipelineEvent('backend', 0.5, logLine);
      break;
    }

    case EK.TrieCoupling: {
      // trie_a / trie_b are digest-participant indices in kadabra.Field, not
      // node.tries[] slots; only draw an arc when both already exist as visuals.
      const trieA = ev.vals?.trie_a | 0;
      const trieB = ev.vals?.trie_b | 0;
      const coupling = ev.vals?.coupling || 0;
      if (trieA < MAX_VIZ_TRIE_VISUALS && trieB < MAX_VIZ_TRIE_VISUALS) {
        updateTrieCouplingArc(ev.src, trieA, trieB, coupling);
      }
      break;
    }

    case EK.TrieMode: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const trieIdx = ev.vals?.trie_idx | 0;
      const modeIdx = ev.vals?.mode_idx | 0;
      const aligned = (ev.vals?.aligned || 0) > 0.5;
      const energy = ev.vals?.energy || 0;
      while (node.trieModes.length <= trieIdx) node.trieModes.push({ aligned: false, modeIdx: -1, energy: 0 });
      node.trieModes[trieIdx] = { aligned, modeIdx, energy };
      updateTrieAppearance(ev.src, trieIdx);
      break;
    }

    case EK.TriePressure: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const trieIdx = ev.vals?.trie_idx | 0;
      const decay = ev.vals?.decay || 0;
      const learn = ev.vals?.learn || 0;
      const decayMul = ev.vals?.decay_mul || 0;
      const learnMul = ev.vals?.learn_mul || 0;
      while (node.triePressures.length <= trieIdx) node.triePressures.push({ decay: 0, learn: 0, decayMul: 1, learnMul: 1 });
      node.triePressures[trieIdx] = { decay, learn, decayMul, learnMul };
      updateTrieAppearance(ev.src, trieIdx);
      tickNodeActivity(ev.src, 0.3);
      break;
    }

    case EK.TrieSignal: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      const trieIdx = ev.vals?.trie_idx | 0;
      const surprisal = ev.vals?.surprisal || 0;
      const entropy = ev.vals?.entropy || 0;
      const growth = ev.vals?.growth || 0;
      while (node.trieSignals.length <= trieIdx) node.trieSignals.push({ surprisal: 0, entropy: 0, growth: 0 });
      node.trieSignals[trieIdx] = { surprisal, entropy, growth };
      updateTrieAppearance(ev.src, trieIdx);
      break;
    }

    case EK.TrieGraphSnapshot: {
      const kadNode = state.nodes.get(ev.src);
      if (!kadNode) break;
      const trieIdx = Math.floor(ev.vals?.trie_idx ?? -1);
      if (trieIdx < 0) break;
      let payload;
      try {
        payload = JSON.parse(ev.meta?.graph || '{}');
      } catch {
        break;
      }
      if (kadNode.tries[trieIdx]) {
        applyTrieGraphSnapshot(kadNode, trieIdx, payload);
        renderNodeStats(kadNode);
        if (state.selected === ev.src) {
          refreshInspector();
        }
      }
      break;
    }

    case EK.BeamCollect: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      console.log('viz: BeamCollect', ev.src, ev.vals);
      const trieCount = ev.vals?.trie_count || 0;
      const contCount = ev.vals?.continuation_count || 0;
      const beam = node.beam;
      beam.collecting = true;
      beam.lastCollect = performance.now();
      beam.activeCount = contCount;
      tickNodeActivity(ev.src, 2);

      // Spawn collection rays: animated tubes from each trie up to the node core.
      for (const oldRay of beam.rays) {
        node.group.remove(oldRay.mesh);
        oldRay.mesh.geometry.dispose();
        oldRay.mesh.material.dispose();
      }
      beam.rays.length = 0;

      const nodeY = 0;
      const trieBaseY = -5.5;
      const numRays = Math.min(node.tries.length, trieCount, 12);
      for (let i = 0; i < numRays; i++) {
        const trie = node.tries[i];
        if (!trie) continue;
        const from = new THREE.Vector3(trie.group.position.x, trieBaseY, trie.group.position.z);
        const to = new THREE.Vector3(0, nodeY, 0);
        const mid = from.clone().add(to).multiplyScalar(0.5);
        mid.x += (Math.random() - 0.5) * 1.5;
        mid.z += (Math.random() - 0.5) * 1.5;
        const curve = new THREE.QuadraticBezierCurve3(from, mid, to);
        const geo = new THREE.TubeGeometry(curve, 12, 0.06, 4, false);
        const mat = new THREE.MeshBasicMaterial({
          color: 0x4a80c0, transparent: true, opacity: 0.0,
        });
        const mesh = new THREE.Mesh(geo, mat);
        node.group.add(mesh);
        beam.rays.push({ mesh, t: 0, from, to, trieIdx: i });
        trie.insertFlash = 1.0;
      }

      // Also pulse the node strongly during collection.
      node.pulseAlpha = 0.8;
      node.pulseScale = 1.0;
      node.pulseRing.material.color.setHex(0x4a80c0);
      break;
    }

    case EK.BeamCompose: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      console.log('viz: BeamCompose', ev.src, ev.vals);
      const selected = ev.vals?.selected_count || 0;
      const rejected = ev.vals?.rejected_count || 0;
      const score = ev.vals?.best_score || 0;
      const beam = node.beam;
      beam.lastCompose = performance.now();
      beam.activeCount = selected;
      beam.rejectedCount = rejected;
      beam.bestScore = score;
      beam.collecting = false;

      // Clear old hypotheses.
      for (const hyp of beam.hypotheses) {
        node.group.remove(hyp.mesh);
        hyp.mesh.geometry.dispose();
        hyp.mesh.material.dispose();
      }
      beam.hypotheses.length = 0;

      // Spawn hypothesis orbs orbiting the node — one per selected candidate.
      const orbitR = 3.0;
      const orbitY = 2.5;
      for (let i = 0; i < Math.min(selected, 8); i++) {
        const angle = (i / Math.min(selected, 8)) * Math.PI * 2;
        const geo = new THREE.SphereGeometry(0.25 + score * 0.15, 8, 8);
        const brightness = 0.4 + (1 - i / Math.min(selected, 8)) * 0.6;
        const mat = new THREE.MeshBasicMaterial({
          color: rejected > 0 ? 0xc08030 : 0x408060,
          transparent: true,
          opacity: brightness * 0.8,
        });
        const mesh = new THREE.Mesh(geo, mat);
        mesh.position.set(
          Math.cos(angle) * orbitR,
          orbitY,
          Math.sin(angle) * orbitR,
        );
        node.group.add(mesh);
        beam.hypotheses.push({ mesh, score: brightness, origin: i, angle, fade: 1.0 });
      }

      pulseNode(ev.src);

      // Small floater with stats.
      const pos = node.group.position.clone();
      pos.y += 3;
      const color = rejected > 0 ? '#c08030' : '#408060';
      spawnFloater(pos, `${selected}↑ ${rejected}✗ (${score.toFixed(2)})`, color);
      break;
    }

    case EK.BeamBreak: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      console.log('viz: BeamBreak', ev.src, ev.tgt, ev.vals);
      const beam = node.beam;
      tickNodeActivity(ev.src, 2);

      // Pick a trie to shatter — flash red and spawn break particles falling down.
      const trieIdx = node.tries.length > 0 ? Math.floor(Math.random() * node.tries.length) : -1;
      if (trieIdx >= 0) {
        const trie = node.tries[trieIdx];
        if (!trie) {
          /* empty slot */
        } else if (trie.pickMeshes && trie.pickMeshes.length) {
          for (const mesh of trie.pickMeshes) {
            mesh.backupScale = mesh.scale.x;
            mesh.material.color.setHex(0xc04040);
            mesh.material.emissive.setHex(0x401010);
            mesh.scale.setScalar(1.35);
          }
          setTimeout(() => {
            updateTrieAppearance(ev.src, trieIdx);
            for (const mesh of trie.pickMeshes) {
              mesh.scale.setScalar(mesh.backupScale || 1.0);
            }
          }, 900);
        }

        if (trie) {
          const trieWorldPos = new THREE.Vector3();
          trie.group.getWorldPosition(trieWorldPos);
          for (let p = 0; p < 8; p++) {
            const geo = new THREE.TetrahedronGeometry(0.06, 0);
            const mat = new THREE.MeshBasicMaterial({ color: 0xc04040, transparent: true, opacity: 0.9 });
            const pm = new THREE.Mesh(geo, mat);
            pm.position.copy(trieWorldPos);
            scene.add(pm);
            beam.breakParticles.push({
              mesh: pm,
              velocity: new THREE.Vector3(
                (Math.random() - 0.5) * 0.08,
                -0.02 - Math.random() * 0.04,
                (Math.random() - 0.5) * 0.08,
              ),
              life: 1.0,
            });
          }
        }
      }

      // Flash a red X descending from node to trie area.
      const pos = node.group.position.clone();
      pos.y -= 2;
      spawnFloater(pos, '✗ BREAK', '#c04040', new THREE.Vector3(0, -0.025, 0));
      break;
    }

    case EK.BeamConverge: {
      const node = state.nodes.get(ev.src);
      if (!node) break;
      console.log('viz: BeamConverge', ev.src, ev.lbl, ev.vals);
      const seq = ev.lbl || '';
      const score = ev.vals?.score || 0;
      const beam = node.beam;

      // Clear hypotheses — they've resolved.
      for (const hyp of beam.hypotheses) {
        node.group.remove(hyp.mesh);
        hyp.mesh.geometry.dispose();
        hyp.mesh.material.dispose();
      }
      beam.hypotheses.length = 0;

      // Convergence ring: expanding bright ring around the node.
      if (beam.convergeRing) {
        node.group.remove(beam.convergeRing.mesh);
        beam.convergeRing.mesh.geometry.dispose();
        beam.convergeRing.mesh.material.dispose();
      }
      const ringGeo = new THREE.RingGeometry(1.0, 1.4, 32);
      const ringMat = new THREE.MeshBasicMaterial({
        color: 0x408060, transparent: true, opacity: 0.9,
        side: THREE.DoubleSide,
      });
      const ring = new THREE.Mesh(ringGeo, ringMat);
      ring.rotation.x = -Math.PI / 2;
      ring.position.y = 1.5;
      node.group.add(ring);
      beam.convergeRing = { mesh: ring, scale: 1.0, fade: 1.0 };

      node.core.material.color.setHex(0x408060);
      setTimeout(() => {
        node.core.material.color.setHex(0x4a80c0);
      }, 1500);

      pulseNode(ev.src);

      // Result floater.
      const pos = node.group.position.clone();
      pos.y += 4.5;
      const display = seq.length > 40 ? `${seq.slice(0, 40)}...` : seq;
      spawnFloater(pos, `→ ${display}`, '#408060');

      if (score > 0) {
        const pos2 = node.group.position.clone();
        pos2.y += 3.5;
        spawnFloater(pos2, `score: ${score.toFixed(3)}`, '#308060');
      }
      break;
    }

    case EK.DatasetRead: {
      const bytes = ev.vals?.bytes_read || 0;
      const total = ev.vals?.total_bytes || 0;
      const ds = state.pipeline.stages.get('dataset');
      if (ds) {
        ds.metrics.bytesProcessed = total;
      }
      pipelineEvent('dataset', 1, `read ${bytes}B (${(total / 1024).toFixed(1)} KiB)`);
      spawnPipelineFlowParticle('dataset', 'machine', 0x408060);
      break;
    }

    case EK.TokenizerChunk: {
      const bytes = ev.vals?.bytes_written || 0;
      const ts = state.pipeline.stages.get('tokenizer');
      if (ts) {
        ts.metrics.bytesProcessed += bytes;
      }
      pipelineEvent('tokenizer', 0.5, `chunk ${bytes}B`);
      break;
    }

    case EK.TokenizerEmit: {
      pipelineEvent('tokenizer', 1, `emit ${ev.lbl || ''}`);
      spawnPipelineFlowParticle('machine', 'backend', 0x4a80c0);
      break;
    }

    case EK.QueueSubmit: {
      const inflight = ev.vals?.inflight || 0;
      const qs = state.pipeline.stages.get('queue');
      if (qs) {
        qs.metrics.inflight = inflight;
      }
      pipelineEvent('queue', 0.8, `submit inf=${inflight}`);
      spawnPipelineFlowParticle('backend', 'queue', 0x806030);
      break;
    }

    case EK.CausalHubProbe: {
      const depth = ev.vals?.depth || 0;
      const status = ev.meta?.status || 'unknown';
      pipelineEvent('queue', 1, `hub depth=${depth} ${status}`);
      break;
    }

    case EK.Prompt: {
      const pos = new THREE.Vector3(0, 6, 0);
      spawnFloater(pos, ev.meta?.prompt || 'prompt', '#4a80c0');
      break;
    }

    case EK.PromptResult: {
      const pos = new THREE.Vector3(0, 4, 0);
      spawnFloater(pos, ev.meta?.generation || 'result', '#408060');
      break;
    }
  }

  if (state.selected && (ev.src === state.selected || ev.tgt === state.selected)) {
    refreshInspector();
  }
}
