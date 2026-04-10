import * as THREE from 'three';

export function textMaterialFromText(text, color, fontSize, bold) {
  const scale = 2;
  const fs = (fontSize || 16) * scale;
  const c = document.createElement('canvas');
  const ctx = c.getContext('2d');
  c.width = 1024;
  c.height = 64 * scale;
  ctx.font = `${bold ? 'bold ' : ''}${fs}px monospace`;
  ctx.fillStyle = color || '#a0b0d0';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.fillText(text.substring(0, 56), c.width / 2, c.height / 2);
  const tex = new THREE.CanvasTexture(c);
  tex.minFilter = THREE.LinearFilter;
  tex.magFilter = THREE.LinearFilter;
  return new THREE.SpriteMaterial({ map: tex, transparent: true, depthWrite: false });
}

export function textSprite(text, color, fontSize, bold) {
  return new THREE.Sprite(textMaterialFromText(text, color, fontSize, bold));
}

export function escapeHtml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}
