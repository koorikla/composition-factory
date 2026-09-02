# Demo GIF Recorder

Composition Factory includes a headless recording harness that drives the live web application using Playwright, captures frame buffers in memory, and quantizes them into crisp animated GIFs using native JavaScript encoders (`gifenc` + `pngjs`) without requiring `ffmpeg` or external binaries.

---

## Overview

The recording pipeline consists of two components:
1. **`scripts/record-demos/run.sh`**: Lifecycle supervisor. Builds the Go binary, seeds a scratch directory with test blueprints and lockfiles, starts `cf serve` on an isolated port (`8086`), executes the recording script, and ensures clean teardown.
2. **`scripts/record-demos/record.js`**: Playwright driver. Orchestrates real UI scenarios (drag-to-wire, provider discovery, full IRSA demo) while sampling canvas frames at a calibrated frame rate.

---

## Running the Recorder

Run the recorder from the repository root:

```sh
./scripts/record-demos/run.sh
```

Generated GIF and screenshot assets are written to `docs/screenshots/`:
- `demo.gif`: Hero demo showing IRSA dependency tree auto-layout, status ARN wiring into ServiceAccount annotations, and live Crossplane render validation.
- `compose.gif`: Dragging kinds from the schema palette and live composition YAML generation.
- `catalogue.gif`: Discovering and adding packages from the 476+ OSS provider catalogue.
- `wire.gif`: Click-and-drag variable mapping from XRD parameter dots onto resource cards.
- `examples.gif`: Startup starter blueprints modal chooser (IRSA, RDS PostgreSQL, Full-Stack App) and instant canvas loading.
- `tree.gif`: Artifacts & File Tree Explorer navigation with tab switching, collapsing, and clipboard copy.
- `kcl.gif`: Real-time engine switching between `go-templating` and `kcl` (`function-kcl` KCLInput).
- `floating.gif`: Floating the Inspector and Code Drawer, dragging across canvas, and docking back in place.
- High-resolution screenshots: `canvas.png`, `inspector.png`, `catalogue.png`, `examples-modal.png`, `tree-explorer.png`, `kcl-engine.png`, `fs-export.png`.

---

## Architecture & Design Decisions

- **Zero External Transcoding Dependencies**: Uses `gifenc` (NeuQuant color quantization) to produce small, high-fidelity GIFs directly from PNG frame buffers.
- **True Engine Integration**: Tests run against the actual `cf` Go binary and live HTTP API rather than mocked frontends.
- **Deterministic Playback**: Timing pauses and easing curves ensure animations remain smooth and readable in documentation.

