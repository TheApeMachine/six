import * as THREE from 'three';

/*
Phase direction markers lie in the XZ plane (horizontal). Azimuth zero matches
nodeAngle (idx/total·2π): reference radius is +X. The arrow points along +Z —
the tangent direction for increasing angle when viewed from +Y (CCW), so when
real field phase is wired in, rotating the parent group around Y aligns the arrow
with the live phase origin without re-animating for decoration.
*/
const HORIZ_PHASE_TANGENT = new THREE.Vector3(0, 0, 1);

export function createPhaseDirectionArrow(color, shaftLength, headLength, headWidth) {
  const arrow = new THREE.ArrowHelper(
    HORIZ_PHASE_TANGENT,
    new THREE.Vector3(0, 0, 0),
    shaftLength,
    color,
    headLength,
    headWidth,
  );
  arrow.userData.role = 'gfPhaseDirection';
  return arrow;
}
