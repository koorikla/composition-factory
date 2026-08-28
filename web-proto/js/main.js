/**
 * main.js — boot. Imports the store and every region module, then loads the
 * document. Region modules subscribe to store topics at import time; the
 * initial "doc" emit from loadDoc() triggers their first render.
 *
 * Region root elements (see index.html):
 *   topbar    #region-topbar
 *   palette   #region-palette
 *   canvas    #cw
 *   inspector #region-inspector
 *   output    #region-output
 */
import { store } from "./store.js";
import "./regions/topbar.js";
import "./regions/palette.js";
import "./regions/canvas.js";
import "./regions/inspector.js";
import "./regions/output.js";

store.loadDoc();
