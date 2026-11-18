// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// 3D viewer for the chat (three.js, bundled; loaded only when a model is shown).
// glTF/GLB, FBX (embedded or side-by-side textures, skinning, animations),
// OBJ (+ same-name .mtl), STL, PLY, Collada (.dae), 3MF.
// Textures, .bin and .mtl next to the model are fetched through the same file
// endpoint as the model itself (see fileURL in view.js).
import * as THREE from '../../vendor/three/build/three.module.js';
import { OrbitControls } from '../../vendor/three/examples/jsm/controls/OrbitControls.js';
import { RoomEnvironment } from '../../vendor/three/examples/jsm/environments/RoomEnvironment.js';

export const QUALITY = {
  low: { ratio: 0.75, aa: false, shadows: 0, env: false },
  medium: { ratio: 1, aa: true, shadows: 0, env: true },
  high: { ratio: 1.5, aa: true, shadows: 1024, env: true },
  ultra: { ratio: 2, aa: true, shadows: 2048, env: true },
};

const loaders = {
  async glb() { const { GLTFLoader } = await import('../../vendor/three/examples/jsm/loaders/GLTFLoader.js'); return GLTFLoader; },
  async fbx() { const { FBXLoader } = await import('../../vendor/three/examples/jsm/loaders/FBXLoader.js'); return FBXLoader; },
  async obj() { const { OBJLoader } = await import('../../vendor/three/examples/jsm/loaders/OBJLoader.js'); return OBJLoader; },
  async mtl() { const { MTLLoader } = await import('../../vendor/three/examples/jsm/loaders/MTLLoader.js'); return MTLLoader; },
  async stl() { const { STLLoader } = await import('../../vendor/three/examples/jsm/loaders/STLLoader.js'); return STLLoader; },
  async ply() { const { PLYLoader } = await import('../../vendor/three/examples/jsm/loaders/PLYLoader.js'); return PLYLoader; },
  async dae() { const { ColladaLoader } = await import('../../vendor/three/examples/jsm/loaders/ColladaLoader.js'); return ColladaLoader; },
  async '3mf'() { const { ThreeMFLoader } = await import('../../vendor/three/examples/jsm/loaders/3MFLoader.js'); return ThreeMFLoader; },
};
loaders.gltf = loaders.glb;

// res:// names a file next to the model; resolve() turns it into a real URL.
function manager(resolve, onProgress) {
  const m = new THREE.LoadingManager();
  m.setURLModifier((url) => {
    if (url.startsWith('res://')) {
      let rel = decodeURIComponent(url.slice(6)).replace(/\\/g, '/');
      if (/^([a-z]:)?\//i.test(rel)) rel = rel.split('/').pop(); // an absolute path from the author's machine: look next to the model
      return resolve(rel);
    }
    return url;
  });
  m.onProgress = (_, done, total) => onProgress?.(done / Math.max(1, total));
  return m;
}

// Load a model. Returns { root, clips, lights } where root is an Object3D.
export async function loadModel({ url, ext, resolve, sibling, onProgress }) {
  const mgr = manager(resolve, onProgress);
  const L = loaders[ext];
  if (!L) throw new Error('unsupported: .' + ext);
  const Loader = await L();
  const loader = new Loader(mgr);
  loader.setResourcePath?.('res://');
  if (ext === 'obj') {
    const mtlURL = sibling('.mtl');
    if (mtlURL) {
      try {
        const MTL = await loaders.mtl();
        const ml = new MTL(mgr);
        ml.setResourcePath('res://');
        const mats = await ml.loadAsync(mtlURL);
        mats.preload();
        loader.setMaterials(mats);
      } catch { /* no .mtl: grey material */ }
    }
  }
  const out = await loader.loadAsync(url, (e) => e.total && onProgress?.(e.loaded / e.total));
  if (ext === 'glb' || ext === 'gltf') return { root: out.scene, clips: out.animations || [] };
  if (ext === 'dae') return { root: out.scene, clips: out.scene.animations || out.animations || [] };
  if (ext === 'stl' || ext === 'ply') {
    const geo = out;
    if (!geo.attributes.normal) geo.computeVertexNormals();
    const mat = new THREE.MeshStandardMaterial({ color: geo.attributes.color ? 0xffffff : 0xb9b0c4, vertexColors: !!geo.attributes.color, roughness: 0.55, metalness: 0.1 });
    return { root: new THREE.Mesh(geo, mat), clips: [] };
  }
  return { root: out, clips: out.animations || [] };
}

export function statsOf(root, clips) {
  const st = { meshes: 0, triangles: 0, vertices: 0, materials: new Set(), textures: new Set(), bones: 0, lights: 0, clips: clips.length };
  root.traverse((o) => {
    if (o.isMesh || o.isPoints || o.isLine) {
      st.meshes++;
      const g = o.geometry;
      const vc = g?.attributes?.position?.count || 0;
      st.vertices += vc;
      st.triangles += g?.index ? g.index.count / 3 : vc / 3;
      for (const m of [].concat(o.material || [])) {
        st.materials.add(m);
        for (const k of ['map', 'normalMap', 'roughnessMap', 'metalnessMap', 'emissiveMap', 'aoMap', 'alphaMap', 'bumpMap', 'specularMap']) if (m[k]) st.textures.add(m[k]);
      }
    }
    if (o.isSkinnedMesh) st.bones = Math.max(st.bones, o.skeleton?.bones?.length || 0);
    if (o.isLight) st.lights++;
  });
  return { ...st, triangles: Math.round(st.triangles), materials: st.materials.size, textures: st.textures.size };
}

// A live view of a loaded model in a host element.
export class Viewer {
  constructor(host, model, opts) {
    this.host = host;
    this.model = model;
    this.opts = { quality: 'low', lights: 'both', wire: false, grid: true, bg: 'theme', ...opts };
    this.last = performance.now();
    this.scene = new THREE.Scene();
    this.camera = new THREE.PerspectiveCamera(45, 1, 0.01, 1000);
    this.mixer = model.clips.length ? new THREE.AnimationMixer(model.root) : null;
    this.speed = 1;
    this.playing = !!this.mixer;
    this.action = null;
    // the model's own lights; ours are added beside them
    this.sceneLights = [];
    model.root.traverse((o) => { if (o.isLight) this.sceneLights.push(o); });
    this.scene.add(model.root);
    this.studio = new THREE.Group();
    this.hemi = new THREE.HemisphereLight(0xffffff, 0x3a3240, 1.2);
    this.key = new THREE.DirectionalLight(0xffffff, 2.2);
    this.studio.add(this.hemi, this.key, this.key.target);
    this.scene.add(this.studio);
    this.fitInfo = this.measure();
    this.grid = new THREE.GridHelper(this.fitInfo.size * 2.4, 24, 0x6c6372, 0x3a3241);
    this.grid.position.y = this.fitInfo.box.min.y;
    this.grid.material.transparent = true; this.grid.material.opacity = 0.45;
    this.scene.add(this.grid);
    this.build();
    if (this.mixer) this.play(0);
    this.fit();
    this.applyLights(); this.applyWire(); this.applyGrid(); this.applyBg();
  }

  measure() {
    const box = new THREE.Box3().setFromObject(this.model.root);
    if (box.isEmpty()) box.set(new THREE.Vector3(-1, -1, -1), new THREE.Vector3(1, 1, 1));
    const size = box.getSize(new THREE.Vector3()).length() || 1;
    return { box, size, center: box.getCenter(new THREE.Vector3()) };
  }

  // (re)create the renderer for the current quality
  build() {
    const q = QUALITY[this.opts.quality] || QUALITY.low;
    if (this.renderer) { this.renderer.setAnimationLoop(null); this.controls?.dispose(); this.renderer.dispose(); this.renderer.domElement.remove(); this.pmrem?.dispose(); this.envTex?.dispose(); }
    const r = new THREE.WebGLRenderer({ antialias: q.aa, alpha: true, preserveDrawingBuffer: true, powerPreference: q.ratio > 1 ? 'high-performance' : 'low-power' });
    r.setPixelRatio(Math.min(q.ratio, window.devicePixelRatio || 1) || q.ratio);
    r.outputColorSpace = THREE.SRGBColorSpace;
    r.toneMapping = q.env ? THREE.ACESFilmicToneMapping : THREE.NoToneMapping;
    r.shadowMap.enabled = q.shadows > 0;
    r.shadowMap.type = THREE.PCFSoftShadowMap;
    this.renderer = r;
    this.host.prepend(r.domElement);
    r.domElement.className = 'media-3d-canvas';
    if (q.env) {
      this.pmrem = new THREE.PMREMGenerator(r);
      this.envTex = this.pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
    } else { this.pmrem = null; this.envTex = null; }
    this.key.castShadow = q.shadows > 0;
    if (q.shadows) {
      this.key.shadow.mapSize.set(q.shadows, q.shadows);
      const s = this.fitInfo.size;
      Object.assign(this.key.shadow.camera, { left: -s, right: s, top: s, bottom: -s, near: 0.01, far: s * 6 });
      this.key.shadow.camera.updateProjectionMatrix();
    }
    this.model.root.traverse((o) => { if (o.isMesh) { o.castShadow = q.shadows > 0; o.receiveShadow = q.shadows > 0; } });
    const target = this.controls?.target.clone();
    this.controls = new OrbitControls(this.camera, r.domElement);
    this.controls.enableDamping = true;
    if (target) this.controls.target.copy(target);
    this.controls.addEventListener('change', () => (this.dirty = true));
    this.resize();
    this.applyLights();
    this.dirty = true;
    r.setAnimationLoop(() => this.frame());
  }

  setQuality(q) { if (QUALITY[q] && q !== this.opts.quality) { this.opts.quality = q; this.build(); } }

  frame() {
    if (!this.host.isConnected) { this.dispose(); return; }
    if (this.paused) return;
    const now = performance.now(); const dt = Math.min(0.1, (now - this.last) / 1000); this.last = now;
    let moved = this.controls.update();
    if (this.mixer && this.playing) { this.mixer.update(dt * this.speed); moved = true; this.onTime?.(this.time()); }
    if (moved || this.dirty) { this.renderer.render(this.scene, this.camera); this.dirty = false; }
  }

  resize() {
    const w = this.host.clientWidth || 640; const hgt = this.host.clientHeight || 380;
    this.renderer.setSize(w, hgt, false);
    this.camera.aspect = w / hgt; this.camera.updateProjectionMatrix();
    this.dirty = true;
  }

  fit() {
    const { center, size, box } = this.measure();
    this.fitInfo = { center, size, box };
    const dist = size / (2 * Math.tan((this.camera.fov * Math.PI) / 360)) * 1.1;
    this.camera.near = size / 1000; this.camera.far = size * 100; this.camera.updateProjectionMatrix();
    this.camera.position.copy(center).add(new THREE.Vector3(0.6, 0.45, 1).normalize().multiplyScalar(dist));
    this.controls.target.copy(center);
    this.key.position.copy(center).add(new THREE.Vector3(size, size * 1.6, size * 0.8));
    this.key.target.position.copy(center);
    this.dirty = true;
  }

  applyLights() {
    const mode = this.opts.lights;
    const hasOwn = this.sceneLights.length > 0;
    this.sceneLights.forEach((l) => (l.visible = mode !== 'studio'));
    // studio when asked, or when the model brings no light of its own
    const studio = mode !== 'scene' || !hasOwn;
    this.studio.visible = studio;
    this.scene.environment = studio && this.envTex ? this.envTex : null;
    this.dirty = true;
  }
  applyWire() {
    this.model.root.traverse((o) => { if (o.isMesh) [].concat(o.material).forEach((m) => { if (m && 'wireframe' in m) m.wireframe = this.opts.wire; }); });
    this.dirty = true;
  }
  applyGrid() { this.grid.visible = this.opts.grid; this.dirty = true; }
  applyBg() {
    const bg = this.opts.bg;
    this.scene.background = bg === 'dark' ? new THREE.Color(0x0d0b0f) : bg === 'light' ? new THREE.Color(0xf4f1f6) : null;
    this.dirty = true;
  }

  // animation
  play(i) {
    if (!this.mixer) return;
    this.action?.stop();
    this.clipIndex = i;
    this.action = this.mixer.clipAction(this.model.clips[i]);
    this.action.reset().play();
    this.playing = true;
  }
  toggle() { this.playing = !this.playing; this.last = performance.now(); }
  duration() { return this.action?.getClip().duration || 0; }
  time() { return this.action ? this.action.time % (this.duration() || 1) : 0; }
  seek(t) { if (!this.action) return; this.action.time = t; this.mixer.update(0); this.dirty = true; this.onTime?.(t); }

  screenshot() {
    this.renderer.render(this.scene, this.camera);
    return this.renderer.domElement.toDataURL('image/png');
  }

  dispose() {
    if (this.gone) return;
    this.gone = true;
    this.renderer.setAnimationLoop(null);
    this.controls.dispose();
    this.mixer?.stopAllAction();
    this.scene.traverse((o) => {
      o.geometry?.dispose?.();
      [].concat(o.material || []).forEach((m) => { for (const v of Object.values(m)) if (v?.isTexture) v.dispose(); m.dispose?.(); });
    });
    this.pmrem?.dispose(); this.envTex?.dispose();
    this.renderer.dispose();
    this.renderer.forceContextLoss?.();
  }
}
