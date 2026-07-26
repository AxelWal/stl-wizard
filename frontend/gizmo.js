import * as THREE from "three";
import { TransformControls } from "three/addons/controls/TransformControls.js";
import { sceneRoot, cameraRef, domElement, controlsRef, modelBounds } from "./viewer.js";

let group;      // carries the plane's position and orientation
let quad;       // the translucent rectangle
let arrow;      // points along the normal, toward the part that gets cut off
let frame;      // outline, so the extent is readable against the model
let transform;
let helper;     // what TransformControls actually renders — see initGizmo
let changeHandlers = [];

// Width and height are stored here rather than baked into the geometry, so the
// resize handles in the next task can change them without rebuilding anything.
let width = 10;
let height = 10;

let handles = [];       // four corner spheres
let dragging = null;    // the handle currently being dragged
const raycaster = new THREE.Raycaster();
const pointer = new THREE.Vector2();

function handleRadius() {
  return Math.max(width, height) * 0.03;
}

function layoutHandles() {
  // buildHandles() runs at the end of initGizmo(), after the quad exists but
  // before rebuildQuad() is ever called again — so handles is never empty by
  // the time layoutHandles() runs. Guarded anyway: cheaper than proving it.
  if (!handles.length) return;

  const r = handleRadius();
  const hw = width / 2;
  const hh = height / 2;
  const corners = [
    [-hw, -hh], [hw, -hh], [hw, hh], [-hw, hh],
  ];
  handles.forEach((h, i) => {
    h.position.set(corners[i][0], corners[i][1], 0);
    h.scale.setScalar(r);
  });
}

function buildHandles() {
  const geo = new THREE.SphereGeometry(1, 12, 8);
  const mat = new THREE.MeshBasicMaterial({ color: 0xffcc44 });
  for (let i = 0; i < 4; i++) {
    const h = new THREE.Mesh(geo, mat);
    h.userData.corner = i;
    handles.push(h);
    group.add(h);
  }
  layoutHandles();
}

// ponytail: a corner drag resizes symmetrically about the plane's centre rather
// than pinning the opposite corner, so the rectangle stays centred on the origin
// the cut uses. That keeps planeInput() a straight read of the transform.
// Upgrade path if off-centre rectangles are wanted: move the group's origin as
// the extent changes and keep the cut origin at the rectangle's centre.
function installHandleDragging() {
  const el = domElement();

  const toPointer = (event) => {
    const rect = el.getBoundingClientRect();
    pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
    pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
  };

  el.addEventListener("pointerdown", (event) => {
    if (!group.visible) return;
    toPointer(event);
    raycaster.setFromCamera(pointer, cameraRef());
    const hit = raycaster.intersectObjects(handles, false)[0];
    if (!hit) return;

    dragging = hit.object;
    controlsRef().enabled = false;   // do not orbit mid-resize
    // TransformControls' own pointerdown ran first on this same canvas. If it
    // latched an axis, disabling it now would make its pointerup bail out and
    // leave dragging stuck true, killing move and rotate for the session.
    if (transform.dragging === false) {
      transform.enabled = false;     // do not also translate
    }
    el.setPointerCapture(event.pointerId);
    event.stopPropagation();
  });

  el.addEventListener("pointermove", (event) => {
    if (!dragging) return;
    toPointer(event);
    raycaster.setFromCamera(pointer, cameraRef());

    // Intersect the ray with the gizmo's own plane, then read the hit in the
    // group's local frame — that gives the new half-extents directly.
    group.updateMatrixWorld();
    const normal = new THREE.Vector3(0, 0, 1).applyQuaternion(group.quaternion);
    const plane = new THREE.Plane().setFromNormalAndCoplanarPoint(normal, group.position);

    const hit = new THREE.Vector3();
    if (!raycaster.ray.intersectPlane(plane, hit)) return;

    const local = group.worldToLocal(hit.clone());
    setExtent(Math.abs(local.x) * 2, Math.abs(local.y) * 2);
  });

  const endDrag = (event) => {
    if (!dragging) return;
    dragging = null;
    controlsRef().enabled = true;
    transform.enabled = true;
    if (el.hasPointerCapture(event.pointerId)) el.releasePointerCapture(event.pointerId);
  };
  el.addEventListener("pointerup", endDrag);
  el.addEventListener("pointercancel", endDrag);
}

function rebuildQuad() {
  quad.geometry.dispose();
  quad.geometry = new THREE.PlaneGeometry(width, height);

  frame.geometry.dispose();
  frame.geometry = new THREE.EdgesGeometry(new THREE.PlaneGeometry(width, height));

  layoutHandles();
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
  transform.enabled = false;

  // Orbiting while dragging a handle would fight the drag.
  transform.addEventListener("dragging-changed", (e) => {
    controlsRef().enabled = !e.value;
  });
  transform.addEventListener("objectChange", emitChange);

  // three.js changed how TransformControls is added to a scene; newer versions
  // expose a helper object. Support both rather than pinning behaviour. The
  // helper is what renders, and attach() above made it visible, so visibility
  // has to be toggled here — transform.visible is a property nothing reads.
  helper = transform.getHelper ? transform.getHelper() : transform;
  helper.visible = false;
  sceneRoot().add(helper);

  buildHandles();
  installHandleDragging();

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

// mode reports which of the two the gizmo is in. Read by the mode-switch test,
// which otherwise has no way to tell a button that wired up from one that did
// not — TransformControls looks identical from outside until you drag it.
export function mode() {
  return transform.mode;
}

// showGizmo reveals the plane, by default parking it at the centre of what is on screen
// and sized to cover it, so the first thing the user sees is a plane that would actually
// cut.
//
// reframe false leaves the placement alone, for showing a plane that already has one — a
// planned cut being previewed. It is a parameter rather than something the caller undoes
// afterwards because undoing it is what went wrong: restoring the position and the extent
// but not the rotation showed every planned cut lying flat, so the preview disagreed with
// what the cut would actually do.
export function showGizmo(reframe = true) {
  const box = modelBounds();
  if (reframe && box) {
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
  helper.visible = true;
  transform.enabled = true;
  emitChange();
}

export function hideGizmo() {
  // Restore orbiting first, for either kind of drag. TransformControls' pointer
  // handlers all bail out when disabled, including the one that ends a drag and
  // re-enables the camera; and a corner-handle drag is tracked separately, by
  // `dragging`, so its pointerup would land on a hidden gizmo. Either way,
  // disabling mid-drag would leave the camera dead with no error and no way back.
  if (transform.dragging || dragging) {
    controlsRef().enabled = true;
  }
  // Abandon any handle drag outright, so a later pointerup cannot resize a
  // rectangle the user can no longer see.
  dragging = null;
  group.visible = false;
  helper.visible = false;
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

// placement reports where the plane is, for the numeric fields. Euler angles in degrees
// rather than a quaternion, because degrees are what a user can type.
export function placement() {
  group.updateMatrixWorld();
  const e = new THREE.Euler().setFromQuaternion(group.quaternion, "XYZ");
  const deg = 180 / Math.PI;
  return {
    position: [group.position.x, group.position.y, group.position.z],
    rotation: [e.x * deg, e.y * deg, e.z * deg],
  };
}

// setPlacement moves the plane to typed coordinates and angles. Non-finite components are
// ignored rather than applied: a number field emptied mid-edit yields NaN, and a NaN
// position would put the plane nowhere with no way back.
export function setPlacement(position, rotation) {
  const rad = Math.PI / 180;
  if (position && position.every(Number.isFinite)) {
    group.position.set(position[0], position[1], position[2]);
  }
  if (rotation && rotation.every(Number.isFinite)) {
    group.rotation.set(rotation[0] * rad, rotation[1] * rad, rotation[2] * rad);
  }
  group.updateMatrixWorld();
  emitChange();
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
