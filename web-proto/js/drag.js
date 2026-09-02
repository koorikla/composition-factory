export function startDrag(e, onMove, onEnd) {
  const target = e.target;
  const hasCapture = typeof target.setPointerCapture === "function";
  if (hasCapture) {
    try {
      target.setPointerCapture(e.pointerId);
    } catch (_) {}
  }

  const listenerNode = hasCapture ? target : document;
  let active = true;

  function move(ev) {
    if (!active) return;
    if (onMove) onMove(ev);
  }

  function up(ev) {
    if (!active) return;
    cleanup();
    if (onEnd) onEnd(ev);
  }

  function cancel(ev) {
    if (!active) return;
    cleanup();
    if (onEnd) onEnd(ev);
  }

  function cleanup() {
    active = false;
    listenerNode.removeEventListener("pointermove", move);
    listenerNode.removeEventListener("pointerup", up);
    listenerNode.removeEventListener("pointercancel", cancel);
    if (hasCapture) {
      try {
        target.releasePointerCapture(e.pointerId);
      } catch (_) {}
    }
  }

  listenerNode.addEventListener("pointermove", move);
  listenerNode.addEventListener("pointerup", up);
  listenerNode.addEventListener("pointercancel", cancel);

  return function abort() {
    if (!active) return;
    cleanup();
    if (onEnd) onEnd(); // signal cancellation
  };
}
