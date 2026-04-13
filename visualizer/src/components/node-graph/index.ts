/**
 * Node Graph component exports
 *
 * GPU-accelerated force-directed graph visualization for neural network
 * architecture and activity visualization.
 */

export { NodeGraph, type NodeGraphProps, type LayoutType } from './NodeGraph'
export { Graph, generators, type GraphData, type Node, type Edge } from './core/graph'
export { Simulator, type SimulatorConfig } from './core/simulator'
export { GPUPicking, MouseTracker } from './core/gpu-picking'
export {
  createGraphGeometry,
  updateMaterialTextures,
  disposeGeometry,
  type GeometryResult,
  type LookupTableEntry,
} from './core/geometry'
export {
  indexTextureSize,
  dataTextureSize,
  generatePositionTexture,
  generateVelocityTexture,
  generateNodeAttribTexture,
  generateIndicesTexture,
  generateDataTexture,
  generateZeroedPositionTexture,
  generateEpochDataTexture,
  generateIdMappings,
  generateCircularLayout,
  generateSphericalLayout,
  generateHelixLayout,
  generateGridLayout,
} from './utils/texture-generators'
