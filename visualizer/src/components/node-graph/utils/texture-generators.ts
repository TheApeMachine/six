/**
 * GPU texture generators for force-directed graph simulation
 *
 * These functions generate Float32Array data that gets uploaded to GPU textures.
 * The textures store node positions, velocities, attributes, and connectivity
 * information for the GPGPU force-directed layout algorithm.
 */

import * as THREE from 'three'

/**
 * Calculate texture size as power of 2
 *
 * GPU textures work best with power-of-2 dimensions. This finds the smallest
 * power of 2 that can fit the required number of elements.
 */
export function indexTextureSize(dataArray: number[][]): number {
  const num = dataArray.length
  let power = 1
  while (power * power < num) {
    power *= 2
  }
  return power / 2 > 1 ? power : 2
}

/**
 * Calculate data texture size
 *
 * Since each pixel stores 4 floats (RGBA), we need to count all items
 * in the nested arrays.
 */
export function dataTextureSize(dataArray: number[][]): number {
  let count = 0
  for (let i = 0; i < dataArray.length; i++) {
    count += dataArray[i]?.length ?? 0
  }
  // Create a dummy array of the right length to calculate index size
  const dummyArray = new Array(Math.ceil(count / 4)).fill([])
  return indexTextureSize(dummyArray)
}

/**
 * Count total items in nested arrays
 */
export function countDataArrayItems(dataArray: number[][]): number {
  let counter = 0
  for (let i = 0; i < dataArray.length; i++) {
    counter += dataArray[i]?.length ?? 0
  }
  return counter
}

/**
 * Generate random position texture
 *
 * Creates initial random positions for nodes within a bounding cube.
 * Each pixel stores (x, y, z, w) where w=1 indicates valid node.
 */
export function generatePositionTexture(
  inputArray: number[][],
  textureSize: number,
  size: number
): THREE.DataTexture {
  const bounds = size
  const boundsHalf = bounds / 2

  const textureArray = new Float32Array(textureSize * textureSize * 4)

  for (let i = 0; i < textureArray.length; i += 4) {
    if (i < inputArray.length * 4) {
      const x = Math.random() * bounds - boundsHalf
      const y = Math.random() * bounds - boundsHalf
      const z = Math.random() * bounds - boundsHalf

      textureArray[i] = x
      textureArray[i + 1] = y
      textureArray[i + 2] = z
      textureArray[i + 3] = 1.0
    } else {
      // Fill remaining pixels with -1 (invalid marker)
      textureArray[i] = -1.0
      textureArray[i + 1] = -1.0
      textureArray[i + 2] = -1.0
      textureArray[i + 3] = -1.0
    }
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate zeroed position texture
 *
 * Used for layout targets - positions that nodes should move towards.
 */
export function generateZeroedPositionTexture(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)

  for (let i = 0; i < textureArray.length; i += 4) {
    if (i < inputArray.length * 4) {
      textureArray[i] = 0.0
      textureArray[i + 1] = 0.0
      textureArray[i + 2] = 0.0
      textureArray[i + 3] = 0.0
    } else {
      textureArray[i] = -1.0
      textureArray[i + 1] = -1.0
      textureArray[i + 2] = -1.0
      textureArray[i + 3] = -1.0
    }
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate velocity texture
 *
 * Initial velocities are zero. The simulation will update these.
 */
export function generateVelocityTexture(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)

  for (let i = 0; i < textureArray.length; i += 4) {
    if (i < inputArray.length * 4) {
      textureArray[i] = 0.0
      textureArray[i + 1] = 0.0
      textureArray[i + 2] = 0.0
      textureArray[i + 3] = 0.0
    } else {
      textureArray[i] = -1.0
      textureArray[i + 1] = -1.0
      textureArray[i + 2] = -1.0
      textureArray[i + 3] = -1.0
    }
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate node attribute texture
 *
 * Stores per-node visual attributes:
 * - x: size
 * - y: opacity
 * - z: unused
 * - w: unused
 */
export function generateNodeAttribTexture(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)

  for (let i = 0; i < textureArray.length; i += 4) {
    if (i < inputArray.length * 4) {
      textureArray[i] = 200.0     // size
      textureArray[i + 1] = 0.2   // opacity
      textureArray[i + 2] = 0.0   // unused
      textureArray[i + 3] = 0.0   // unused
    } else {
      textureArray[i] = -1.0
      textureArray[i + 1] = -1.0
      textureArray[i + 2] = -1.0
      textureArray[i + 3] = -1.0
    }
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate ID mappings texture
 *
 * Maps texture position to node ID for lookup operations.
 */
export function generateIdMappings(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)
  let counter = 0

  for (let i = 0; i < textureArray.length; i += 4) {
    if (i < inputArray.length * 4) {
      textureArray[i] = counter
      textureArray[i + 1] = 0
      textureArray[i + 2] = 0
      textureArray[i + 3] = 0
    } else {
      textureArray[i] = -1.0
      textureArray[i + 1] = -1.0
      textureArray[i + 2] = -1.0
      textureArray[i + 3] = -1.0
    }
    counter++
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate indices texture
 *
 * Stores start/end indices for each node's edge list in the data texture.
 * Each pixel: (startPixel, startCoord, endPixel, endCoord)
 */
export function generateIndicesTexture(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)
  let currentPixel = 0
  let currentCoord = 0

  for (let i = 0; i < inputArray.length; i++) {
    const startPixel = currentPixel
    const startCoord = currentCoord

    for (let j = 0; j < (inputArray[i]?.length ?? 0); j++) {
      currentCoord++
      if (currentCoord === 4) {
        currentPixel++
        currentCoord = 0
      }
    }

    textureArray[i * 4] = startPixel
    textureArray[i * 4 + 1] = startCoord
    textureArray[i * 4 + 2] = currentPixel
    textureArray[i * 4 + 3] = currentCoord
  }

  for (let i = inputArray.length * 4; i < textureArray.length; i++) {
    textureArray[i] = -1
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate data texture
 *
 * Packs edge data into a texture. Each float is a connected node ID.
 */
export function generateDataTexture(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)

  let currentIndex = 0
  for (let i = 0; i < inputArray.length; i++) {
    for (let j = 0; j < (inputArray[i]?.length ?? 0); j++) {
      textureArray[currentIndex] = inputArray[i][j]
      currentIndex++
    }
  }

  for (let i = currentIndex; i < textureArray.length; i++) {
    textureArray[i] = -1
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate epoch data texture
 *
 * Stores timestamp data for temporal visualization, offset from minimum epoch.
 */
export function generateEpochDataTexture(
  inputArray: number[][],
  textureSize: number,
  epochOffset: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)

  let currentIndex = 0
  for (let i = 0; i < inputArray.length; i++) {
    for (let j = 0; j < (inputArray[i]?.length ?? 0); j++) {
      textureArray[currentIndex] = inputArray[i][j] - epochOffset
      currentIndex++
    }
  }

  for (let i = currentIndex; i < textureArray.length; i++) {
    textureArray[i] = -1
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

// Layout generators - create target positions for different layouts

/**
 * Generate circular layout positions
 */
export function generateCircularLayout(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const increase = (Math.PI * 2) / inputArray.length
  let angle = 0
  const radius = inputArray.length * 4 * 2

  const textureArray = new Float32Array(textureSize * textureSize * 4)

  for (let i = 0; i < textureArray.length; i += 4) {
    if (i < inputArray.length * 4) {
      const x = radius * Math.cos(angle)
      const y = radius * Math.sin(angle)
      const z = 0

      textureArray[i] = x
      textureArray[i + 1] = y
      textureArray[i + 2] = z
      textureArray[i + 3] = 1.0

      angle += increase
    } else {
      textureArray[i] = -1.0
      textureArray[i + 1] = -1.0
      textureArray[i + 2] = -1.0
      textureArray[i + 3] = -1.0
    }
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate spherical layout positions
 */
export function generateSphericalLayout(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const radius = inputArray.length * 4
  const textureArray = new Float32Array(textureSize * textureSize * 4)
  const l = inputArray.length

  for (let i = 0; i < l; i++) {
    const phi = Math.acos(-1 + (2 * i) / l)
    const theta = Math.sqrt(l * Math.PI) * phi

    const x = radius * Math.cos(theta) * Math.sin(phi)
    const y = radius * Math.sin(theta) * Math.sin(phi)
    const z = radius * Math.cos(phi)

    textureArray[i * 4] = z
    textureArray[i * 4 + 1] = y
    textureArray[i * 4 + 2] = x
    textureArray[i * 4 + 3] = 1.0
  }

  for (let i = inputArray.length * 4; i < textureArray.length; i++) {
    textureArray[i] = -1
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate helix layout positions
 */
export function generateHelixLayout(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)
  const l = inputArray.length

  for (let i = 0; i < l; i++) {
    const phi = i * 0.125 + Math.PI

    const x = i * 15
    const y = 500 * Math.sin(phi)
    const z = 500 * Math.cos(phi)

    textureArray[i * 4] = x
    textureArray[i * 4 + 1] = y
    textureArray[i * 4 + 2] = z
    textureArray[i * 4 + 3] = 1.0
  }

  for (let i = inputArray.length * 4; i < textureArray.length; i++) {
    textureArray[i] = -1
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}

/**
 * Generate grid layout positions
 */
export function generateGridLayout(
  inputArray: number[][],
  textureSize: number
): THREE.DataTexture {
  const textureArray = new Float32Array(textureSize * textureSize * 4)

  for (let i = 0; i < inputArray.length; i++) {
    const x = (i % 5) * 500 - 1000
    const y = -(Math.floor(i / 5) % 5) * 500 + 1000
    const z = Math.floor(i / 25) * 500 - 1000

    textureArray[i * 4] = x
    textureArray[i * 4 + 1] = y
    textureArray[i * 4 + 2] = z
    textureArray[i * 4 + 3] = 1.0
  }

  for (let i = inputArray.length * 4; i < textureArray.length; i++) {
    textureArray[i] = -1
  }

  const texture = new THREE.DataTexture(
    textureArray,
    textureSize,
    textureSize,
    THREE.RGBAFormat,
    THREE.FloatType
  )
  texture.needsUpdate = true
  return texture
}
