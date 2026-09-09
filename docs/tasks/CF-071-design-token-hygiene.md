# CF-071 — Design-token hygiene: a purple, five undefined \`var()\`s, and no type or spacing scale

> **Read \`docs/task-execution-contract.md\` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | \`CF-071 — Design-token hygiene: a purple, five undefined var()s, and no type or spacing scale.\` |
| **Worktree** | \`.worktrees/CF-071\` on branch \`CF-071-design-token-hygiene\`, branched from \`main\` |
| **May write** | \`internal/examples/examples.go\`, \`web-proto/css/proto.css\`, \`docs/design/canvas-prototype.html\`, \`tests/\` |
| **Merges after** | nothing |

## Symptom

1. \`#7c3aed\` (hue 262.1°) ships as the CRON card color (\`internal/examples/examples.go:105\`, painted inline at \`main.js:643\`) — the only violet in the served asset graph, breaching the design system's palette rules.
2. Five \`var()\` names are referenced in JS and CSS but never defined in \`:root\` / theme blocks:
   - \`--dim\` in \`web-proto/js/main.js:65\` (\`color:var(--dim)\`)
   - \`--accent\` in \`web-proto/js/regions/output.js:458\` (\`color:var(--accent)\`)
   - \`--pri\` in \`web-proto/css/proto.css:873\` (\`border: 2px solid var(--pri, #6ea8fe)\`)
   - \`--panel\` in \`web-proto/css/proto.css:876\` (\`background: var(--panel, #16181d)\`)
   - \`--fg\` in \`web-proto/css/proto.css:877\` (\`color: var(--fg, #e6e6e6)\`)
   Because \`--panel\` and \`--fg\` are not defined, the tour card falls back to \`#16181d\` (dark panel) and \`#e6e6e6\` even in light theme, rendering near-black in the light theme.
3. Zero token drift must be maintained between \`web-proto/css/proto.css\` and \`docs/design/canvas-prototype.html\`.

## Requirements

1. **Fix CRON card color**:
   - In \`internal/examples/examples.go\`, update the \`CRON\` example icon color to use a canonical palette token (e.g. \`var(--wire-status)\` or a palette-compliant color like \`#0284c7\` / \`#0f766e\` / \`var(--wire-ref)\`) instead of \`#7c3aed\`.
2. **Define missing tokens**:
   - In \`web-proto/css/proto.css\` and \`docs/design/canvas-prototype.html\`:
     Define the 5 missing CSS variables in both light and dark theme blocks:
     - \`--dim\`: muted/faint color (\`var(--faint)\` or equivalent hex).
     - \`--accent\`: brand/action accent (\`var(--wire-xrd)\` or equivalent hex).
     - \`--pri\`: primary action color (\`var(--wire-xrd)\` or equivalent hex).
     - \`--panel\`: surface/card panel background (\`var(--surface)\` in light, \`var(--surface)\` in dark).
     - \`--fg\`: foreground text color (\`var(--ink)\` in light, \`var(--ink)\` in dark).
     Ensure the tour card in light mode renders with light surface background and dark text, and dark mode uses dark surface and light text.
3. **Preserve Zero Token Drift**:
   - Ensure \`proto.css\` and \`canvas-prototype.html\` token lists remain 100% in sync.
4. **Automated Tests**:
   - Add Playwright assertions verifying:
     - The CRON card icon does not use \`#7c3aed\`.
     - The 5 variables \`--dim\`, \`--accent\`, \`--pri\`, \`--panel\`, \`--fg\` are defined in computed styles for both light and dark modes.
     - The tour card in light mode has light background (\`--surface\`) and dark text (\`--ink\`), not dark fallback \`#16181d\`.
     - Zero token drift between \`proto.css\` and \`canvas-prototype.html\`.

## Verification

\`\`\`sh
npm run lint:js
make lint
make test
npx playwright test tests/slice67-theme-native-controls.spec.js
\`\`\`
