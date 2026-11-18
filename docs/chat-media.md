# Media in the chat

The HPK Console shows what an agent makes inside the chat itself: a Blender
model you can turn and play, a render, a video, a chart of the numbers it
found. Nothing extra is needed from the agent: it names a file or writes a
table, and the Console shows it.

## What is shown

| In a message or tool output | Shown as |
|---|---|
| A file path: `renders/kart.fbx`, `C:\game\out\hero.glb`, `~/proj/a.png`, `"My Car.fbx"` (quoted, with spaces) | a card for that file |
| `![alt](renders/shot.png)` and `[text](docs/plan.pdf)` | a card (the markdown itself is not repeated) |
| `data:image/png;base64,…` (e.g. a Blender viewport screenshot from an MCP tool) | the image; the tool row shows the data shortened |
| A markdown table, a ```` ```csv ```` / ```` ```tsv ```` block | a table, with a **Chart** button when it has number columns |
| A ```` ```chart ```` block (see below) | a chart, with a **Table** button |
| Any other fenced code block | code, labelled with its language |
| A link to another website | a link card only: the Console loads nothing from other hosts |

File kinds:

- **3D**: glTF / GLB, FBX, OBJ (+ the `.mtl` with the same name), STL, PLY,
  Collada `.dae`, 3MF.
- **Images**: PNG, JPEG, GIF, WebP, AVIF, BMP, SVG. Click to zoom and pan
  (wheel, drag, `+` `-` `0`, Esc).
- **Video**: MP4, WebM, MOV, M4V, OGV. **Audio**: MP3, WAV, OGG, FLAC, M4A.
- **PDF** in the browser's own viewer.
- **Data**: CSV, TSV and JSON lists of records become tables (up to 5 MB).

A file mentioned in a tool row and again in the agent's reply is shown once,
with the reply.

## 3D viewer

Built on three.js 0.187 (MIT), bundled in `assets/console/vendor/three/` so
it works offline. It is loaded only when a chat shows a model, and a model
starts loading when it scrolls into view.

- Turn, pan and zoom; **Fit to view**; floor grid; wireframe; background
  (page, dark, light); **full screen**; save a **screenshot** (PNG).
- **Textures** inside the file (FBX, GLB) or next to it (`.gltf` images and
  `.bin`, `.mtl` maps, FBX textures beside the model) load through the same
  project file endpoint. An FBX that names a texture by an absolute path from
  the artist's machine is looked up by file name next to the model.
- **Animations**: every clip by name, play / pause, a time slider, speed
  0.25×–2×. Skinned meshes (bones) play as exported.
- **Lights**: the model's own lights (glTF `KHR_lights_punctual`, FBX and
  Collada lights) plus a studio light, either alone, or both. A model without
  lights always gets the studio light.
- **Model details**: meshes, triangles, vertices, materials, textures, bones,
  lights, animations.
- At most four 3D views run at once; older ones pause and keep a still image
  (click to show again). Views that scroll out of sight stop drawing.

Not supported: glTF files with Draco, Meshopt or Basis (KTX2) compression.
Their decoders need WebAssembly, which the Console's security policy does not
allow. The viewer says so; export without compression.

### Quality

Each 3D view has a **Quality** choice, and new views start at **Low**:

| Quality | Resolution | Smooth edges | Studio reflections | Shadows |
|---|---|---|---|---|
| Low (default) | 0.75 × | no | no | no |
| Medium | 1 × | yes | yes | no |
| High | up to 1.5 × | yes | yes | 1024 px |
| Ultra | up to 2 × (the screen's) | yes | yes | 2048 px |

**Make default** saves the current choice for every new 3D view in this
browser. At Low, videos also wait for Play before loading anything.

## Chart blocks

An agent can draw a chart by writing:

````
```chart
{"type": "line", "title": "Lap time and fuel left",
 "labels": ["1", "2", "3"],
 "series": [{"name": "Lap time (s)", "data": [61.8, 61.6, 61.4]},
            {"name": "Fuel left (l)", "data": [37.1, 34.2, 31.3]}],
 "unit": ""}
```
````

- `type`: `bar`, `stacked`, `line`, `area`, `pie`, `donut`, `scatter`
  (scatter data are `[x, y]` pairs). People can switch between the types that
  fit the data.
- Up to 8 series, in a fixed colour order validated for colour-blind reading
  on both themes; every chart has a legend (2+ series), hover values and a
  table view. There is one y-axis: put measures of different scale in two
  charts.

## Where files come from

`GET /api/files?project=<name>&path=<path>` serves files **from the project
folder only**, read only:

- the path is resolved after following links and must stay inside the
  project's root;
- hidden files and folders (`.env`, `.git`, …) and key files (`*.pem`, `*.key`,
  `id_*`, `credentials*`, `secrets.*`) are never served;
- up to 1 GB, with range requests (video seeking);
- every response is sandboxed (`Content-Security-Policy: sandbox`), and HTML /
  SVG come as downloads, so a file cannot run script in the Console. PDFs are
  allowed in the Console's own frame.

A path outside the project (for example a Blender export to a temp folder)
is shown as a card saying so. Ask the agent to save outputs inside the
project, e.g. under `renders/`.

## Development

`node tools/fixtures/make-media.mjs` writes small made-up files to
`tools/fixtures/media/` (an animated, textured glTF with a light, an OBJ +
MTL + texture, an STL, a PNG, a CSV). The dev server (`tools/devserve.mjs`)
serves them at `/api/files`, and the demo chat **kart › Blender model and lap
chart** uses them. The parser has tests in `tools/media-parse.test.mjs`.
