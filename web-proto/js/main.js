/**
 * main.js — boot. The single module entry (index.html loads only this).
 * Imports the shared store/api and explicitly initializes every region,
 * then loads the document; the initial "doc" emit triggers first renders.
 *
 * Region root elements (see index.html):
 *   palette   #region-palette
 *   canvas    #cw
 *   inspector #region-inspector
 *   output    #region-output  (also drives the topbar #region-topbar —
 *             crumb/version/valid chip/theme/validate/generate live off
 *             this region's generate cycle)
 */
import { store } from "./store.js";
import * as api from "./api.js";
import { init as initPalette } from "./regions/palette.js";
import { init as initCanvas } from "./regions/canvas.js";
import { init as initInspector } from "./regions/inspector.js";
import { init as initOutput } from "./regions/output.js";

const deps = { store, api };
initPalette(document.getElementById("region-palette"), deps);
initCanvas(document.getElementById("cw"), deps);
initInspector(document.getElementById("region-inspector"), deps);
initOutput(document.getElementById("region-output"), deps);

store.loadDoc();
