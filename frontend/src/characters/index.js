import * as THREE from 'three';
import * as blob from './blob.js';
import * as cat from './cat.js';
import * as chick from './chick.js';
import { collectOutlines } from './common.js';

export const CHARACTERS = { blob, cat, chick };
export const CHARACTER_LIST = [blob.meta, cat.meta, chick.meta];

/**
 * Creates a character rig. `color` '' means the character's default colour.
 * Returns { id, facing, root, slots, face, glasses, height, headY, outlines, dispose }.
 */
export function createCharacter(id, color) {
  const def = CHARACTERS[id] || CHARACTERS.blob;
  const c = color && /^#[0-9a-f]{3,6}$/i.test(color) ? color : def.meta.color;
  const rig = def.build(c);
  const facing = new THREE.Group();
  facing.name = 'facing';
  facing.add(rig.root);
  const slots = {};
  rig.root.traverse((o) => {
    if (o.name && o.name.startsWith('slot_')) slots[o.name.slice(5)] = o;
  });
  const outlines = collectOutlines(rig.root);
  return {
    id: def.meta.id,
    ...rig,
    facing,
    slots,
    outlines,
    dispose() {
      facing.traverse((o) => {
        if (o.geometry) o.geometry.dispose();
      });
    },
  };
}
