/**
 * Geometry builders for node graph visualization
 *
 * Creates Three.js buffer geometries for rendering nodes, edges, and labels.
 * All geometries use custom attributes for GPU-based position updates via shaders.
 */

import * as THREE from 'three'
import chroma from 'chroma-js'
import type { Graph, Node } from './graph'
import {
  nodeVertexShader,
  nodeFragmentShader,
  pickingFragmentShader,
  edgeVertexShader,
  edgeFragmentShader,
  textVertexShader,
  textFragmentShader,
} from '../shaders'
import { getTextCoordinates, ATLAS_SIZE } from '../utils/font-data'

export interface LookupTableEntry {
  texPos: [number, number]
  color?: number[]
}

export interface GeometryResult {
  nodeGeometry: THREE.BufferGeometry
  pickingGeometry: THREE.BufferGeometry
  edgeGeometry: THREE.BufferGeometry
  labelGeometry: THREE.BufferGeometry
  nodeMaterial: THREE.ShaderMaterial
  pickingMaterial: THREE.ShaderMaterial
  edgeMaterial: THREE.ShaderMaterial
  labelMaterial: THREE.ShaderMaterial
  nodeMesh: THREE.Points
  pickingMesh: THREE.Points
  edgeMesh: THREE.LineSegments
  labelMesh: THREE.Mesh
  lookupTable: Record<string, LookupTableEntry>
  nodeNames: string[]
}

// Default color scale for nodes
const DEFAULT_COLOR_SCALE = [
  '#a6cee3', '#1f78b4', '#b2df8a', '#33a02c', '#fb9a99',
  '#fdbf6f', '#ff7f00', '#cab2d6', '#6a3d9a', '#ffff99', '#b15928'
]

/**
 * Create all geometries for the graph
 *
 * Builds node points, edge lines, picking geometry, and text labels.
 * Returns materials and meshes ready to add to a Three.js scene.
 */
export function createGraphGeometry(
  graph: Graph,
  nodesWidth: number,
  edgesWidth: number,
  epochsWidth: number,
  nodeTexture: THREE.Texture,
  threatTexture: THREE.Texture,
  fontTexture?: THREE.Texture
): GeometryResult {
  const nodesCount = graph.getNodeCount()
  const edgesCount = graph.getEdgeCount()

  // Build lookup table mapping node names to texture positions and colors
  const lookupTable: Record<string, LookupTableEntry> = {}
  const nodeNames: string[] = []
  const chromaScale = chroma.scale(DEFAULT_COLOR_SCALE).domain([0, nodesCount])

  let nodeIndex = 0
  Object.keys(graph.nodes).forEach((key) => {
    const texStartX = (nodeIndex % nodesWidth) / nodesWidth
    const texStartY = Math.floor(nodeIndex / nodesWidth) / nodesWidth
    const color = chromaScale(nodeIndex).gl()

    lookupTable[key] = {
      texPos: [texStartX, texStartY],
      color: [color[0], color[1], color[2]],
    }
    nodeNames.push(key)
    nodeIndex++
  })

  // Create node geometry
  const { nodeGeometry, pickingGeometry, nodeMaterial, pickingMaterial, nodeMesh, pickingMesh } =
    createNodeGeometry(graph, nodesCount, nodesWidth, epochsWidth, lookupTable, nodeTexture, threatTexture)

  // Create edge geometry
  const { edgeGeometry, edgeMaterial, edgeMesh } =
    createEdgeGeometry(graph, edgesCount, lookupTable)

  // Create label geometry
  const { labelGeometry, labelMaterial, labelMesh } =
    createLabelGeometry(graph, lookupTable, fontTexture)

  return {
    nodeGeometry,
    pickingGeometry,
    edgeGeometry,
    labelGeometry,
    nodeMaterial,
    pickingMaterial,
    edgeMaterial,
    labelMaterial,
    nodeMesh,
    pickingMesh,
    edgeMesh,
    labelMesh,
    lookupTable,
    nodeNames,
  }
}

function createNodeGeometry(
  graph: Graph,
  nodesCount: number,
  nodesWidth: number,
  epochsWidth: number,
  lookupTable: Record<string, LookupTableEntry>,
  nodeTexture: THREE.Texture,
  threatTexture: THREE.Texture
) {
  const nodeGeometry = new THREE.BufferGeometry()
  const pickingGeometry = new THREE.BufferGeometry()

  // Visible geometry attributes
  const nodePositions = new Float32Array(nodesCount * 3)
  const nodeReferences = new Float32Array(nodesCount * 2)
  const nodeColors = new Float32Array(nodesCount * 3)
  const nodePick = new Float32Array(nodesCount)
  const threat = new Float32Array(nodesCount)

  // Picking geometry attributes (unique colors for identification)
  const pickingColors = new Float32Array(nodesCount * 3)
  const pickingPick = new Float32Array(nodesCount)

  const color = new THREE.Color()

  let v = 0
  Object.entries(graph.nodes).forEach(([key, node]) => {
    const lookup = lookupTable[key]

    // Initial positions (will be set by shader)
    nodePositions[v * 3] = 0
    nodePositions[v * 3 + 1] = 0
    nodePositions[v * 3 + 2] = 0

    // Node colors from lookup table
    if (lookup.color) {
      nodeColors[v * 3] = lookup.color[0]
      nodeColors[v * 3 + 1] = lookup.color[1]
      nodeColors[v * 3 + 2] = lookup.color[2]
    }

    // Picking colors - encode node ID as color
    color.setHex(v + 1)
    pickingColors[v * 3] = color.r
    pickingColors[v * 3 + 1] = color.g
    pickingColors[v * 3 + 2] = color.b

    // Picking flags
    nodePick[v] = 1.0
    pickingPick[v] = 0.0

    // Texture coordinates for position lookup
    nodeReferences[v * 2] = lookup.texPos[0]
    nodeReferences[v * 2 + 1] = lookup.texPos[1]

    // Threat detection (placeholder - can be extended based on data)
    threat[v] = 0

    v++
  })

  // Set up visible geometry
  nodeGeometry.setAttribute('position', new THREE.BufferAttribute(nodePositions, 3))
  nodeGeometry.setAttribute('texPos', new THREE.BufferAttribute(nodeReferences, 2))
  nodeGeometry.setAttribute('customColor', new THREE.BufferAttribute(nodeColors, 3))
  nodeGeometry.setAttribute('pickingNode', new THREE.BufferAttribute(nodePick, 1))
  nodeGeometry.setAttribute('threat', new THREE.BufferAttribute(threat, 1))

  // Set up picking geometry (shares position and texPos)
  pickingGeometry.setAttribute('position', new THREE.BufferAttribute(nodePositions, 3))
  pickingGeometry.setAttribute('texPos', new THREE.BufferAttribute(nodeReferences, 2))
  pickingGeometry.setAttribute('customColor', new THREE.BufferAttribute(pickingColors, 3))
  pickingGeometry.setAttribute('pickingNode', new THREE.BufferAttribute(pickingPick, 1))
  pickingGeometry.setAttribute('threat', new THREE.BufferAttribute(threat, 1))

  // Node uniforms
  const nodeUniforms = {
    positionTexture: { value: null as THREE.Texture | null },
    nodeAttribTexture: { value: null as THREE.Texture | null },
    sprite: { value: nodeTexture },
    threatSprite: { value: threatTexture },
    currentTime: { value: 0.0 },
  }

  // Visible node material
  const nodeMaterial = new THREE.ShaderMaterial({
    uniforms: nodeUniforms,
    defines: {
      EPOCHSWIDTH: epochsWidth.toFixed(2),
    },
    vertexShader: nodeVertexShader,
    fragmentShader: nodeFragmentShader,
    blending: THREE.AdditiveBlending,
    depthTest: false,
    transparent: true,
  })

  // Picking material needs its own uniforms (no blending for accurate color reading)
  const pickingUniforms = {
    positionTexture: { value: null as THREE.Texture | null },
    nodeAttribTexture: { value: null as THREE.Texture | null },
    sprite: { value: nodeTexture },
    threatSprite: { value: threatTexture },
    currentTime: { value: 0.0 },
  }

  const pickingMaterial = new THREE.ShaderMaterial({
    uniforms: pickingUniforms,
    defines: {
      EPOCHSWIDTH: epochsWidth.toFixed(2),
      NODESWIDTH: nodesWidth.toFixed(2),
    },
    vertexShader: nodeVertexShader,
    fragmentShader: pickingFragmentShader,
    depthTest: true,
    transparent: false,
  })

  const nodeMesh = new THREE.Points(nodeGeometry, nodeMaterial)
  const pickingMesh = new THREE.Points(pickingGeometry, pickingMaterial)

  return { nodeGeometry, pickingGeometry, nodeMaterial, pickingMaterial, nodeMesh, pickingMesh }
}

function createEdgeGeometry(
  graph: Graph,
  edgesCount: number,
  lookupTable: Record<string, LookupTableEntry>
) {
  const edgeGeometry = new THREE.BufferGeometry()

  // Each edge has 2 vertices (start and end)
  const edgePositions = new Float32Array(edgesCount * 2 * 3)
  const edgeReferences = new Float32Array(edgesCount * 2 * 2)
  const edgeColors = new Float32Array(edgesCount * 2 * 3)

  let v = 0
  Object.entries(graph.edges).forEach(([key, edge]) => {
    const startNode = lookupTable[edge.source]
    const endNode = lookupTable[edge.target]

    if (!startNode || !endNode) return

    // Start of line
    edgeReferences[v * 2] = startNode.texPos[0]
    edgeReferences[v * 2 + 1] = startNode.texPos[1]

    edgePositions[v * 3] = 0
    edgePositions[v * 3 + 1] = 0
    edgePositions[v * 3 + 2] = 0

    if (startNode.color) {
      edgeColors[v * 3] = startNode.color[0]
      edgeColors[v * 3 + 1] = startNode.color[1]
      edgeColors[v * 3 + 2] = startNode.color[2]
    }

    v++

    // End of line
    edgeReferences[v * 2] = endNode.texPos[0]
    edgeReferences[v * 2 + 1] = endNode.texPos[1]

    edgePositions[v * 3] = 0
    edgePositions[v * 3 + 1] = 0
    edgePositions[v * 3 + 2] = 0

    if (endNode.color) {
      edgeColors[v * 3] = endNode.color[0]
      edgeColors[v * 3 + 1] = endNode.color[1]
      edgeColors[v * 3 + 2] = endNode.color[2]
    }

    v++
  })

  edgeGeometry.setAttribute('position', new THREE.BufferAttribute(edgePositions, 3))
  edgeGeometry.setAttribute('texPos', new THREE.BufferAttribute(edgeReferences, 2))
  edgeGeometry.setAttribute('customColor', new THREE.BufferAttribute(edgeColors, 3))

  const edgeUniforms = {
    positionTexture: { value: null as THREE.Texture | null },
    nodeAttribTexture: { value: null as THREE.Texture | null },
  }

  const edgeMaterial = new THREE.ShaderMaterial({
    uniforms: edgeUniforms,
    vertexShader: edgeVertexShader,
    fragmentShader: edgeFragmentShader,
    depthTest: false,
    transparent: true,
  })

  const edgeMesh = new THREE.LineSegments(edgeGeometry, edgeMaterial)

  return { edgeGeometry, edgeMaterial, edgeMesh }
}

function createLabelGeometry(
  graph: Graph,
  lookupTable: Record<string, LookupTableEntry>,
  fontTexture?: THREE.Texture
) {
  // Count total characters needed
  let particleCount = 0
  Object.keys(graph.nodes).forEach((key) => {
    particleCount += key.length
  })

  const labelGeometry = new THREE.BufferGeometry()

  // 6 vertices per character (2 triangles forming a quad)
  const positions = new Float32Array(particleCount * 6 * 3)
  const labelPositions = new Float32Array(particleCount * 6 * 3)
  const labelColors = new Float32Array(particleCount * 6 * 3)
  const uvs = new Float32Array(particleCount * 6 * 2)
  const ids = new Float32Array(particleCount * 6)
  const textCoords = new Float32Array(particleCount * 6 * 4)
  const labelReferences = new Float32Array(particleCount * 6 * 2)

  // Character size settings - matches original
  const letterWidth = 20
  const letterSpacing = 15

  let counter = 0
  Object.keys(graph.nodes).forEach((key) => {
    const nodeLookup = lookupTable[key]
    if (!nodeLookup) return

    for (let i = 0; i < key.length; i++) {
      const index = counter * 3 * 2  // Same indexing as original

      // Get texture coordinates for this character using proper font atlas data
      const tc = getTextCoordinates(key[i])
      // tc = [left, top, width, height, xoffset, yoffset] - all normalized to 0-1

      // Compute vertex positions using character metrics
      // Left is offset
      const l = tc[4]
      // Right is offset + width
      const r = tc[4] + tc[2]
      // Bottom is y offset - height
      const b = tc[5] - tc[3]
      // Top is y offset
      const t = tc[5]

      // Set character IDs
      ids[index + 0] = i
      ids[index + 1] = i
      ids[index + 2] = i
      ids[index + 3] = i
      ids[index + 4] = i
      ids[index + 5] = i

      // Set positions (will be updated by shader - all zeros)
      for (let j = 0; j < 18; j++) {
        positions[index * 3 + j] = 0
      }

      // Label positions - vertex offsets from node center
      // Uses font metrics for proper character spacing and alignment
      // Triangle 1: top-left, bottom-left, top-right
      labelPositions[index * 3 + 0] = (i * letterSpacing) + l * letterWidth * 10
      labelPositions[index * 3 + 1] = t * letterWidth * 10
      labelPositions[index * 3 + 2] = 0 * letterWidth * 10

      labelPositions[index * 3 + 3] = (i * letterSpacing) + l * letterWidth * 10
      labelPositions[index * 3 + 4] = b * letterWidth * 10
      labelPositions[index * 3 + 5] = 0 * letterWidth * 10

      labelPositions[index * 3 + 6] = (i * letterSpacing) + r * letterWidth * 10
      labelPositions[index * 3 + 7] = t * letterWidth * 10
      labelPositions[index * 3 + 8] = 0 * letterWidth * 10

      // Triangle 2: bottom-right, top-right, bottom-left
      labelPositions[index * 3 + 9] = (i * letterSpacing) + r * letterWidth * 10
      labelPositions[index * 3 + 10] = b * letterWidth * 10
      labelPositions[index * 3 + 11] = 0 * letterWidth * 10

      labelPositions[index * 3 + 12] = (i * letterSpacing) + r * letterWidth * 10
      labelPositions[index * 3 + 13] = t * letterWidth * 10
      labelPositions[index * 3 + 14] = 0 * letterWidth * 10

      labelPositions[index * 3 + 15] = (i * letterSpacing) + l * letterWidth * 10
      labelPositions[index * 3 + 16] = b * letterWidth * 10
      labelPositions[index * 3 + 17] = 0 * letterWidth * 10

      // UV coordinates for the quad - same for all
      uvs[index * 2 + 0] = 0
      uvs[index * 2 + 1] = 1

      uvs[index * 2 + 2] = 0
      uvs[index * 2 + 3] = 0

      uvs[index * 2 + 4] = 1
      uvs[index * 2 + 5] = 1

      uvs[index * 2 + 6] = 1
      uvs[index * 2 + 7] = 0

      uvs[index * 2 + 8] = 1
      uvs[index * 2 + 9] = 1

      uvs[index * 2 + 10] = 0
      uvs[index * 2 + 11] = 0

      // Text coordinates - same for all 6 vertices of this character
      // tc[0] = left, tc[1] = top, tc[2] = width, tc[3] = height (all normalized)
      textCoords[index * 4 + 0] = tc[0]
      textCoords[index * 4 + 1] = tc[1]
      textCoords[index * 4 + 2] = tc[2]
      textCoords[index * 4 + 3] = tc[3]

      textCoords[index * 4 + 4] = tc[0]
      textCoords[index * 4 + 5] = tc[1]
      textCoords[index * 4 + 6] = tc[2]
      textCoords[index * 4 + 7] = tc[3]

      textCoords[index * 4 + 8] = tc[0]
      textCoords[index * 4 + 9] = tc[1]
      textCoords[index * 4 + 10] = tc[2]
      textCoords[index * 4 + 11] = tc[3]

      textCoords[index * 4 + 12] = tc[0]
      textCoords[index * 4 + 13] = tc[1]
      textCoords[index * 4 + 14] = tc[2]
      textCoords[index * 4 + 15] = tc[3]

      textCoords[index * 4 + 16] = tc[0]
      textCoords[index * 4 + 17] = tc[1]
      textCoords[index * 4 + 18] = tc[2]
      textCoords[index * 4 + 19] = tc[3]

      textCoords[index * 4 + 20] = tc[0]
      textCoords[index * 4 + 21] = tc[1]
      textCoords[index * 4 + 22] = tc[2]
      textCoords[index * 4 + 23] = tc[3]

      // Label references to node position texture - same for all 6 vertices
      labelReferences[index * 2 + 0] = nodeLookup.texPos[0]
      labelReferences[index * 2 + 1] = nodeLookup.texPos[1]

      labelReferences[index * 2 + 2] = nodeLookup.texPos[0]
      labelReferences[index * 2 + 3] = nodeLookup.texPos[1]

      labelReferences[index * 2 + 4] = nodeLookup.texPos[0]
      labelReferences[index * 2 + 5] = nodeLookup.texPos[1]

      labelReferences[index * 2 + 6] = nodeLookup.texPos[0]
      labelReferences[index * 2 + 7] = nodeLookup.texPos[1]

      labelReferences[index * 2 + 8] = nodeLookup.texPos[0]
      labelReferences[index * 2 + 9] = nodeLookup.texPos[1]

      labelReferences[index * 2 + 10] = nodeLookup.texPos[0]
      labelReferences[index * 2 + 11] = nodeLookup.texPos[1]

      // Label colors from node - same for all 6 vertices
      labelColors[index * 3 + 0] = nodeLookup.color?.[0] ?? 1
      labelColors[index * 3 + 1] = nodeLookup.color?.[1] ?? 1
      labelColors[index * 3 + 2] = nodeLookup.color?.[2] ?? 1

      labelColors[index * 3 + 3] = nodeLookup.color?.[0] ?? 1
      labelColors[index * 3 + 4] = nodeLookup.color?.[1] ?? 1
      labelColors[index * 3 + 5] = nodeLookup.color?.[2] ?? 1

      labelColors[index * 3 + 6] = nodeLookup.color?.[0] ?? 1
      labelColors[index * 3 + 7] = nodeLookup.color?.[1] ?? 1
      labelColors[index * 3 + 8] = nodeLookup.color?.[2] ?? 1

      labelColors[index * 3 + 9] = nodeLookup.color?.[0] ?? 1
      labelColors[index * 3 + 10] = nodeLookup.color?.[1] ?? 1
      labelColors[index * 3 + 11] = nodeLookup.color?.[2] ?? 1

      labelColors[index * 3 + 12] = nodeLookup.color?.[0] ?? 1
      labelColors[index * 3 + 13] = nodeLookup.color?.[1] ?? 1
      labelColors[index * 3 + 14] = nodeLookup.color?.[2] ?? 1

      labelColors[index * 3 + 15] = nodeLookup.color?.[0] ?? 1
      labelColors[index * 3 + 16] = nodeLookup.color?.[1] ?? 1
      labelColors[index * 3 + 17] = nodeLookup.color?.[2] ?? 1

      counter++
    }
  })

  labelGeometry.setAttribute('position', new THREE.BufferAttribute(positions, 3))
  labelGeometry.setAttribute('labelPositions', new THREE.BufferAttribute(labelPositions, 3))
  labelGeometry.setAttribute('uv', new THREE.BufferAttribute(uvs, 2))
  labelGeometry.setAttribute('id', new THREE.BufferAttribute(ids, 1))
  labelGeometry.setAttribute('textCoord', new THREE.BufferAttribute(textCoords, 4))
  labelGeometry.setAttribute('texPos', new THREE.BufferAttribute(labelReferences, 2))
  labelGeometry.setAttribute('customColor', new THREE.BufferAttribute(labelColors, 3))

  const labelUniforms = {
    t_text: { value: fontTexture ?? null },
    positionTexture: { value: null as THREE.Texture | null },
    nodeAttribTexture: { value: null as THREE.Texture | null },
  }

  const labelMaterial = new THREE.ShaderMaterial({
    uniforms: labelUniforms,
    vertexShader: textVertexShader,
    fragmentShader: textFragmentShader,
    depthTest: false,
    transparent: true,
  })

  const labelMesh = new THREE.Mesh(labelGeometry, labelMaterial)

  console.log('[Labels] Created label geometry with', particleCount, 'characters,', counter, 'processed')
  console.log('[Labels] Sample textCoord:', Array.from(textCoords.slice(0, 4)))
  console.log('[Labels] Sample labelPositions:', Array.from(labelPositions.slice(0, 6)))

  return { labelGeometry, labelMaterial, labelMesh }
}

/**
 * Update texture uniforms for all materials
 *
 * Called each frame to update position and attribute textures
 * from the GPU simulator.
 */
export function updateMaterialTextures(
  result: GeometryResult,
  positionTexture: THREE.Texture | null,
  nodeAttribTexture: THREE.Texture | null
): void {
  // Update node materials
  result.nodeMaterial.uniforms.positionTexture.value = positionTexture
  result.nodeMaterial.uniforms.nodeAttribTexture.value = nodeAttribTexture
  result.pickingMaterial.uniforms.positionTexture.value = positionTexture
  result.pickingMaterial.uniforms.nodeAttribTexture.value = nodeAttribTexture

  // Update edge material
  result.edgeMaterial.uniforms.positionTexture.value = positionTexture
  result.edgeMaterial.uniforms.nodeAttribTexture.value = nodeAttribTexture

  // Update label material
  result.labelMaterial.uniforms.positionTexture.value = positionTexture
  result.labelMaterial.uniforms.nodeAttribTexture.value = nodeAttribTexture
}

/**
 * Dispose all geometries and materials
 */
export function disposeGeometry(result: GeometryResult): void {
  result.nodeGeometry.dispose()
  result.pickingGeometry.dispose()
  result.edgeGeometry.dispose()
  result.labelGeometry.dispose()
  result.nodeMaterial.dispose()
  result.pickingMaterial.dispose()
  result.edgeMaterial.dispose()
  result.labelMaterial.dispose()
}
