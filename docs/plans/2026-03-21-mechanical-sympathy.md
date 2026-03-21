# Mechanical Sympathy: Six Performance Hardening Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate six critical bottlenecks that prevent the system from scaling to production workloads.

**Architecture:** Each fix targets a different package and can be implemented independently. The fixes align the mathematical core with how hardware actually moves data — GPU memory residency, batched disk I/O, arena allocation, consensus safety, saturation guards, and lock-free dispatch.

**Tech Stack:** Go, CUDA C++, Metal/Objective-C, Cap'n Proto

---

## 1. GPU Resident Memory Pool

**Files:** `pkg/compute/kernel/cuda/resolver.cu`, `pkg/compute/kernel/metal/resolver.m`, `pkg/compute/kernel/cuda/cuda_backend.go`, `pkg/compute/kernel/metal/resolver.go`

Pre-allocate VRAM buffers at init, lease per-call. Only copy the active context query; graph nodes stay resident.

## 2. WAL Group Commit

**Files:** `pkg/store/dmt/persist.go`

Replace per-insert Flush+Sync with batch accumulator. Single Write call per entry via pre-built byte slice. Flush on timer (5ms) or batch size (4KB).

## 3. Tokenizer Allocation Elimination

**Files:** `pkg/system/process/sequencer/sequitur.go`, `pkg/system/process/sequencer/mdl.go`

Sequitur: arena-allocated nodes, index-based digram map. MDL: incremental left/right Distribution maintenance in detectBoundary.

## 4. Consensus Log Binding

**Files:** `pkg/store/dmt/network.go`, `pkg/store/dmt/election.go`, `pkg/store/dmt/forest.go`

Bind WAL to election log. Sync handler diffs against peer's actual Merkle root, not empty tree. Coordinate syncWithPeers and election state transitions.

## 5. Value Saturation Guard

**Files:** `pkg/logic/substrate/graph.go`

When AND operands exceed density threshold, use PhaseDial similarity instead of discrete AND. Graduated transition based on ShannonDensity.

## 6. Lock-Free Worker Dispatch

**Files:** `pkg/system/pool/pool.go`, `pkg/system/pool/worker.go`

Replace `chan chan Job` with ring-buffer-based dispatch. Workers read from shared ring with backoff.

---
