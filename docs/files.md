# Files: folder rules, upload and tidy

Every project keeps its files its own way: a 3D project may split `RAW 3D`,
`Export` and `Texture`, a web app keeps `docs` and `assets`. hpk learns the
split once per project from a wiki page you approve, then uses it in three
places: where uploads land, the **Tidy** button, and the **Files** view in the
Console (Work → Files).

hpk never moves or deletes one of your files without a click on **Confirm**.
Approving the rules only creates folders that are missing.

## The rules page

The rules live on the wiki page `decisions/folders.md` of the project. hpk reads
the first table whose header is exactly:

```markdown
| Folder | What goes here | Extensions |
|---|---|---|
| RAW 3D | Source scenes | .blend .max |
| Export | Files for the engine | .fbx .obj .glb |
| Art/Texture | Images used by models | .png .jpg .tga |
```

- **Folder**: a path inside the project, `/` for nested folders. No `..`, no
  full paths (`C:\code\app`, `/home/me`), nothing that leads out of the project.
- **Extensions**: separated by spaces or commas, any case. An extension belongs
  to one row only; a later row that repeats it is skipped.
- A page with no rows means "no split": the question is answered and hpk keeps
  every file where it is.
- A row hpk cannot use is skipped, never guessed at. `hpk wiki lint` and the
  Files view say which rows were skipped.

The page goes through a draft like any other `decisions/` page, and only a
person can approve it (Auto mode cannot). When you approve it, hpk saves the
page's SHA-256 in `<home>/hpk/trusted/`, and creates the folders that do not
exist yet. While the AI runs under its own account (docs/isolation.md), that
folder is read-only for it, and hpk itself compares against its own copy in
your private folder, so a hash the AI writes there changes nothing.

A rule's folder follows the same name rules as upload: no hidden names, no
key or credential names, no Windows device or short (`GIT~1`) names, no
control characters. A row that breaks one is skipped.

## Rule states

| State | Means | Files view |
|---|---|---|
| `none` | no page and no draft | a note: the AI will ask how to split folders the first time it saves a file |
| `pending` | a draft waits for approval | a link to the draft |
| `ok` | the page is the one you approved | rule cards with a green line; Tidy is on |
| `changed` | the page differs from what you approved (or was never approved) | a caution banner; rules are off until you approve the page again |

The rules are used only in state `ok`. A page edited outside the draft flow,
by a person or by the AI, turns the state to `changed` and hpk stops using it.

## Where hpk acts

`project.yaml` sits in the Hermes home, which the AI can change, so every
upload, list, file read, tidy and folder creation checks the project folder
again: not one of hpk's own folders and, while the AI is isolated, a folder
you shared with it. Inside the project hpk walks one folder at a time and never
follows a link or a Windows junction (`mklink /J`), so a folder the AI swaps
for a junction cannot send a file anywhere else.

## Upload

`POST /api/files/upload?project=` (multipart, field `file`, repeatable) saves
each file in the folder its extension's rule names, else in the project root.

- 2 GB per file by default; set `files.upload_max` (bytes) in `project.yaml`.
- A clash never overwrites: the new file becomes `name (2).ext`, `name (3).ext`, …
- Names with a path, hidden names, key and credential files, and Windows device
  names are refused. Nothing is written through a link or junction.
- When one file fails, the files saved before it are still listed in the answer.
- Solo mode only. On a team server, or without a connected project, the
  Upload button and drops are off.

In the Console, drop files anywhere on the Files view or use **Upload**. Chat
attachments use the same upload.

## Tidy

**Tidy** lists files whose extension has a rule but that sit outside the rule's
folder, grouped by where they would go, all ticked. Files no rule names are
shown under **No place yet** and stay where they are.

hpk leaves alone: `.git`, `node_modules`, hidden folders, files git ignores,
links and junctions, the project's wiki (when you moved it inside the project
folder), other projects' folders and wikis inside this one, and files already
anywhere inside their folder.

Untick what should stay, then **Confirm**:

- A file that changed since the list was made (size or modified time) is
  skipped and reported.
- A clash picks `name (2).ext`; a file git tracks moves with `git mv`.
- Every move is written to the project's `audit.log`.
- The round is saved in your private folder (`tidy/<project>.json`), bound to
  the project folder it was made in. **Undo tidy** (in the toast, and as a
  button while that record exists) lists what goes back and moves it only on
  Confirm; a file that changed since the move stays where it is.

## API

| Route | Does |
|---|---|
| `GET /api/files/rules?project=` | `{state, rows: [{folder, what, exts}], bad}` |
| `GET /api/files/list?project=&path=` | `{path, entries: [{name, dir, size, mtime, folder}]}`; `folder` is the rule folder of a file (state `ok`) |
| `POST /api/files/upload?project=` | multipart `file` → `{files: [{name, path, folder}]}`; on an error `{error, files}` with the files saved before it |
| `GET /api/files/tidy?project=` | `{moves: [{from, to, size, mtime}], homeless, canUndo}`; 409 `{state}` unless the rules are `ok` |
| `POST /api/files/tidy` | `{project, moves}` → `{done, skipped}`; only moves the current plan also holds |
| `GET /api/files/tidy/undo?project=` | `{at, moves}`: what an undo would put back |
| `POST /api/files/tidy/undo` | `{project, at}` → `{done, skipped}`; `at` must name the round the person saw |

Upload and tidy answer only in solo mode, and tidy (and its undo) waits while a
chat of the project is working and needs the rules in state `ok`.
