# Missions

Each mission is a goal a real person would arrive with, plus the oracle that says it is
done. Run one per session; a tired tester stops noticing friction, which is the whole
product being measured.

Pick the mission the dispatcher named. If none was named, run **M1** - first contact is
where the most expensive defects live.

Every mission ends the same way: green Validate, export through the UI, re-import the
exported Composition intact (SKILL.md §2).

---

## M1 - First contact: an S3 bucket, from nothing

**Blank start.** You have never used this tool. You want a Composite Resource your
developers can ask for that gives them one encrypted S3 bucket in a region they choose.

Find a provider. Define the parameter. Get a `Bucket` onto the canvas. Wire the parameter
to it. Make it real.

**Oracle** - generated Composition contains an S3 `Bucket` whose `region` comes from the
XR parameter; XRD declares the parameter with its type; Validate green.

**Watch for** - what the canvas puts in front of you on arrival, before you have done
anything, and whether it helps or interrupts; whether provider search finds
`s3` by the word a user knows rather than the package name; whether anything reports that
schemas are being fetched; whether the first drag onto the canvas is discoverable at all.

---

## M2 - The status wire: IRSA

**Blank start.** You want an IAM Role plus a Kubernetes ServiceAccount annotated with that
role's ARN - the annotation cannot be written until the Role reports it.

**Oracle** - the ServiceAccount's `eks.amazonaws.com/role-arn` annotation is bound to the
Role's `status.atProvider.arn`, the binding is guarded, and the canvas lays the dependency
so the Role precedes the ServiceAccount. Validate green.

**Watch for** - how you learn a status field can be a source at all; whether dragging onto
an annotation (rather than a spec field) is discoverable; whether the guard appears
without you asking; whether the wire is readable once drawn.

---

## M3 - Connection secret: RDS PostgreSQL

**Blank start.** A Postgres instance whose credentials reach the requesting app as a
connection Secret.

**Oracle** - generated artifacts carry the connection-secret envelope; the password never
appears as a literal in the Composition; Validate green.

**Watch for** - whether the envelope is findable without knowing the word "envelope";
whether anything warns you when a secret-shaped field is typed in plain; what the
Inspector shows for a field whose value must not be authored directly.

---

## M4 - Adopt: someone else's Composition

**Blank start.** You inherited a hand-written Composition and want to keep working on it
here. Import `testdata/xqueue-pipeline.composition.golden.yaml` through the UI.

**Oracle** - resources, wires, and parameters appear on the canvas; re-export reproduces
an equivalent document; anything dropped in translation is *named* to you, not silently
lost.

**Watch for** - whether Import is findable from a blank canvas; what happens on a file the
importer cannot fully read; whether a loss is reported or swallowed. **A silent loss is
P0** - it is the Round-Trip Rule failing where the user can see it least.

---

## M5 - Switch engines: go-templating to KCL

**Pristine start** (`tests/fixtures/pristine-doc.json`). You have a working blueprint and
your platform team standardized on KCL.

**Oracle** - the emitted Composition uses `function-kcl` with typed input; the composed
resources are unchanged in substance; Validate green after the switch.

**Watch for** - whether you can tell what the switch will change before you commit to it;
whether the output drawer makes the difference legible; whether anything is lost that the
UI does not mention.

---

## M6 - Start from a starter, then diverge

**Blank start.** Take the starter blueprint chooser at its word: load "Full-Stack
Microservice", then make it yours - add a parameter, wire it to a new field, delete a
resource you do not want, undo that deletion, export.

**Oracle** - the modified blueprint exports and re-imports intact; undo restores the
deleted resource **and its wires**; Validate green.

**Watch for** - whether the starter is understandable on arrival or just a wall of cards;
whether deleting a wired resource explains what else it breaks; whether undo is trusted
enough that you would risk the delete in the first place.

---

## Off-script

The three-strike rule (SKILL.md §3) will sometimes take you somewhere no mission
describes. Follow it. A finding from a detour is worth more than a mission completed on
rails - just say in the report where you left the script and why.
