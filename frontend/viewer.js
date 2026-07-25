// three.js r185 (vendored under frontend/vendor/three/, MIT licensed — see vendor/three/LICENSE)
import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { STLLoader } from "three/addons/loaders/STLLoader.js";

let renderer, scene, camera, controls;
let partGroup;                 // holds one Mesh per visible part
const loader = new STLLoader();

export function initViewer(container) {
  scene = new THREE.Scene();
  scene.background = new THREE.Color(0x2b2b2b);

  camera = new THREE.PerspectiveCamera(45, 1, 0.1, 100000);
  camera.position.set(120, 90, 120);

  renderer = new THREE.WebGLRenderer({ antialias: true });
  renderer.setPixelRatio(window.devicePixelRatio);
  container.appendChild(renderer.domElement);

  controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;

  scene.add(new THREE.AmbientLight(0xffffff, 0.6));
  const key = new THREE.DirectionalLight(0xffffff, 1.0);
  key.position.set(1, 1.5, 1);
  scene.add(key);
  const fill = new THREE.DirectionalLight(0xffffff, 0.4);
  fill.position.set(-1, -0.5, -1);
  scene.add(fill);

  partGroup = new THREE.Group();
  scene.add(partGroup);

  const resize = () => {
    const { clientWidth: w, clientHeight: h } = container;
    // Let three.js set the canvas' CSS size too. Without it the canvas' CSS size
    // falls back to its attribute size — devicePixelRatio times too big — which
    // grows the flex item, refires this observer, and runs away.
    renderer.setSize(w, h);
    camera.aspect = w / Math.max(h, 1);
    camera.updateProjectionMatrix();
  };
  new ResizeObserver(resize).observe(container);
  resize();

  renderer.setAnimationLoop(() => {
    controls.update();
    renderer.render(scene, camera);
  });

  return { scene, camera, renderer, controls };
}

export function clearParts() {
  while (partGroup.children.length) {
    const m = partGroup.children.pop();
    m.geometry.dispose();
    m.material.dispose();
    partGroup.remove(m);
  }
}

// showParts loads every leaf part's geometry and renders it. The selected part is
// solid; the others are dimmed and translucent so you can see where the selection
// sits inside the whole model.
export async function showParts(parts, selectedId) {
  clearParts();

  await Promise.all(
    parts.map(
      (p) =>
        new Promise((resolve, reject) => {
          loader.load(
            `/part/${p.id}.stl`,
            (geometry) => {
              geometry.computeVertexNormals();
              const selected = p.id === selectedId;
              const material = new THREE.MeshLambertMaterial({
                color: selected ? 0x4a9eda : 0x8a8a8a,
                // A part flagged not watertight is tinted so the warning is
                // visible on the model itself, not only in the sidebar.
                emissive: p.watertight ? 0x000000 : 0x552200,
                transparent: !selected,
                opacity: selected ? 1.0 : 0.35,
                side: THREE.DoubleSide,
              });
              const mesh = new THREE.Mesh(geometry, material);
              mesh.userData.partId = p.id;
              partGroup.add(mesh);
              resolve();
            },
            undefined,
            reject
          );
        })
    )
  );
}

// frameAll points the camera at everything currently loaded.
export function frameAll() {
  const box = new THREE.Box3().setFromObject(partGroup);
  if (box.isEmpty()) return;

  const size = box.getSize(new THREE.Vector3());
  const centre = box.getCenter(new THREE.Vector3());
  const radius = Math.max(size.x, size.y, size.z) || 1;

  controls.target.copy(centre);
  camera.position.copy(centre).add(new THREE.Vector3(1, 0.8, 1).multiplyScalar(radius * 1.8));
  camera.near = radius / 100;
  camera.far = radius * 100;
  camera.updateProjectionMatrix();
  controls.update();
}

// modelBounds is what the gizmo needs to size and place itself sensibly.
export function modelBounds() {
  const box = new THREE.Box3().setFromObject(partGroup);
  return box.isEmpty() ? null : box;
}

export function sceneRoot() {
  return scene;
}

export function domElement() {
  return renderer.domElement;
}

export function cameraRef() {
  return camera;
}

export function controlsRef() {
  return controls;
}
