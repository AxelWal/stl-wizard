import * as THREE from "three";
import { TransformControls } from "three/addons/controls/TransformControls.js";
import { sceneRoot, cameraRef, domElement, controlsRef, modelBounds } from "./viewer.js";

let group;      // carries the plane's position and orientation
let quad;       // the translucent rectangle
let arrow;      // points along the normal, toward the part that gets cut off
let frame;      // outline, so the extent is readable against the model
let transform;
let changeHandlers = [];

// Width and height are stored here rather than baked into the geometry, so the
// resize handles in the next task can change them without rebuilding anything.
let width = 10;
let height = 10;

function rebuildQuad() {
  quad.geometry.dispose();
  quad.geometry = new THREE.PlaneGeometry(width, height);

  frame.geometry.dispose();
  frame.geometry = new THREE.EdgesGeometry(new THREE.PlaneGeometry(width, height));
}

export function initGizmo() {
  group = new THREE.Group();

  // PlaneGeometry lies in XY with its normal along +Z, so the group's local +Z
  // is the cut normal. Everything below depends on that.
  quad = new THREE.Mesh(
    new THREE.PlaneGeometry(width, height),
    new THREE.MeshBasicMaterial({
      color: 0x4a9eda,
      transparent: true,
      opacity: 0.25,
      side: THREE.DoubleSide,
      depthWrite: false,
    })
  );
  group.add(quad);

  frame = new THREE.LineSegments(
    new THREE.EdgesGeometry(new THREE.PlaneGeometry(width, height)),
    new THREE.LineBasicMaterial({ color: 0x9ad0ff })
  );
  group.add(frame);

  // The arrow answers "which side gets cut off?", which is otherwise guesswork.
  arrow = new THREE.ArrowHelper(
    new THREE.Vector3(0, 0, 1),
    new THREE.Vector3(0, 0, 0),
    5,
    0xffcc44
  );
  group.add(arrow);

  group.visible = false;
  sceneRoot().add(group);

  transform = new TransformControls(cameraRef(), domElement());
  transform.attach(group);
  transform.setMode("translate");
  transform.visible = false;
  transform.enabled = false;

  // Orbiting while dragging a handle would fight the drag.
  transform.addEventListener("dragging-changed", (e) => {
    controlsRef().enabled = !e.value;
  });
  transform.addEventListener("objectChange", emitChange);

  // three.js changed how TransformControls is added to a scene; newer versions
  // expose a helper object. Support both rather than pinning behaviour.
  const helper = transform.getHelper ? transform.getHelper() : transform;
  sceneRoot().add(helper);

  return { setMode, showGizmo, hideGizmo, planeInput };
}

function emitChange() {
  const input = planeInput();
  for (const fn of changeHandlers) fn(input);
}

export function onChange(fn) {
  changeHandlers.push(fn);
}

export function setMode(mode) {
  transform.setMode(mode); // "translate" or "rotate"
}

// showGizmo parks the plane at the centre of what is on screen, sized to cover
// it, so the first thing the user sees is a plane that would actually cut.
export function showGizmo() {
  const box = modelBounds();
  if (box) {
    const centre = box.getCenter(new THREE.Vector3());
    const size = box.getSize(new THREE.Vector3());
    group.position.copy(centre);
    group.quaternion.identity();

    const span = Math.max(size.x, size.y, size.z) || 10;
    width = span * 1.2;
    height = span * 1.2;
    rebuildQuad();
    arrow.setLength(span * 0.4, span * 0.1, span * 0.05);
  }
  group.visible = true;
  transform.visible = true;
  transform.enabled = true;
  emitChange();
}

export function hideGizmo() {
  // Restore orbiting first. TransformControls' pointer handlers all bail out
  // when disabled, including the one that ends a drag and re-enables the
  // camera — so disabling mid-drag would leave the camera dead with no error
  // and no way back.
  if (transform.dragging) {
    controlsRef().enabled = true;
  }
  group.visible = false;
  transform.visible = false;
  transform.enabled = false;
}

export function setExtent(w, h) {
  // An emptied number field yields NaN, and Math.max(NaN, x) is NaN — which
  // would rebuild the quad with NaN vertices.
  if (Number.isFinite(w)) {
    width = Math.max(w, 1e-6);
  }
  if (Number.isFinite(h)) {
    height = Math.max(h, 1e-6);
  }
  rebuildQuad();
  emitChange();
}

export function extent() {
  return { width, height };
}

export function gizmoGroup() {
  return group;
}

// planeInput converts the gizmo's transform into exactly what App.Cut expects.
// The basis is taken from the group's own axes, so what the user sees is what
// gets cut — deriving it again in Go could pick a different in-plane rotation.
export function planeInput() {
  group.updateMatrixWorld();

  const origin = new THREE.Vector3();
  const q = new THREE.Quaternion();
  const s = new THREE.Vector3();
  group.matrixWorld.decompose(origin, q, s);

  const u = new THREE.Vector3(1, 0, 0).applyQuaternion(q).normalize();
  const v = new THREE.Vector3(0, 1, 0).applyQuaternion(q).normalize();
  const n = new THREE.Vector3(0, 0, 1).applyQuaternion(q).normalize();

  return {
    origin: [origin.x, origin.y, origin.z],
    normal: [n.x, n.y, n.z],
    u: [u.x, u.y, u.z],
    v: [v.x, v.y, v.z],
    width,
    height,
  };
}
