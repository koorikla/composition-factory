# CF-235 — After Import, the loss bar, the Adopted toast and the error toast all paint at the same spot and hide each other

## Severity & Scope
- **Severity**: P2
- **Scale**: ux
- **Touch set**: `web-proto/js/main.js`, `web-proto/css/proto.css`, `tests/cf235-toast-stacking.spec.js`, `docs/tasks/CF-235-toast-stacking.md`

## Background & Problem
Every notice the canvas raises after an import lands in the same 54px band under the topbar:
- The loss bar `#import-warn` is positioned directly under the topbar (`web-proto/js/main.js:778-781`).
- Both the "📥 Adopted …" toast (`main.js:671`) and the error toast (`main.js:62`) share `.toast-bar{position:fixed;top:54px;left:50%;transform:translateX(-50%)}` (`web-proto/css/proto.css:934`).
Nothing stacks, queues, or offsets them, so whichever paints last covers the rest. When an imported document triggers a loss warning and/or an error, the crucial diagnostic notices are obscured behind the top toast.

## Expected Behavior & Contract
Concurrent toasts and notices must be readable:
- Toasts and warning/loss banners must not occlude each other. Multiple simultaneous toasts must stack vertically with appropriate offset or use a clear notification stack/queue container.
- Error toasts must remain visible and legible, never hidden behind informational toasts or loss bars.

## Acceptance Test
- Playwright spec `tests/cf235-toast-stacking.spec.js` asserting that multiple concurrent notices (e.g. loss bar + toast, or error toast + adopted toast) do not collide/obscure each other and are simultaneously readable in the DOM/viewport.
