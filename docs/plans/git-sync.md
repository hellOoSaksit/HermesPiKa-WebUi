# แผน: sync โค้ดผ่าน GitHub / GitLab

สถานะ: **แผน รอตัดสินใจ** — ต่อจาก docs/plans/connected-mode.md (โค้ดไม่ผ่าน server ของ hpk; ผ่าน git host เท่านั้น)

## 1. หลักการ

1. **โค้ดเดินทางผ่าน git host ของคุณเท่านั้น** (GitHub, GitLab.com, GitLab self-hosted) — hpk ไม่คัดลอกไฟล์โค้ดเอง
2. **hpk ไม่เก็บ token ของ git host** — ใช้ที่ตั้งไว้แล้วในเครื่อง: `gh auth` / `glab auth` / git credential helper / SSH key
3. **push, เปิด PR/MR เข้า branch หลัก, merge = เส้นแดง** (มีอยู่แล้ว) — คนอนุมัติเสมอ; **ไม่มี auto-merge, ไม่มี force-push, ไม่ rebase เอง**
4. ทีมเห็น "ใครทำอะไรบน branch ไหน PR ไหน CI ผ่านไหม" บน server — โดยไม่ต้องเห็นโค้ด

## 2. ภาพรวม

```
เครื่องฉัน                          GitHub / GitLab                    server ทีม (hpk)
แชทหลัก ─ sub-task 1.2 (worktree)    branch hpk/harbor-api/1.2-...      บอร์ด: claim 1.2 ↔ PR #57
   │ commit (agent)                   PR/MR #57 (draft)                  สถานะ CI, รีวิว, merge
   │ push  ── เส้นแดง: คุณอนุมัติ ──▶   checks (CI) ───── webhook ─────▶  แจ้งทีม, ปล่อย claim
   │ เปิด draft PR ── เส้นแดง ──▶                                         ร่าง wiki จาก PR ที่ merge
   ◀─────── fetch / แจ้งเตือน base ขยับ, PR คนอื่นชนไฟล์ ◀────────────────
```

## 3. Branch และ worktree

- sub-task มี worktree + branch อยู่แล้ว; เปลี่ยนชื่อ branch ให้ไม่ชนข้ามคน/เครื่อง:
  `hpk/<project>/<lineage>-<slug>-<user>` เช่น `hpk/harbor-api/1.2-export-csv-max`
  (ตั้งรูปแบบได้ใน `project.yaml`: `git.branch_template`)
- base branch ของโปรเจค: `git.base` (ค่าเริ่มต้น = default branch ของ remote)
- remote ที่อนุญาต: `git.allowed_remotes` — push ไป remote ที่ไม่อยู่ในรายการ = ปฏิเสธ (กันส่งโค้ดออกไปผิดที่); โปรเจค `isolated` ต้องตั้งรายการนี้ก่อน push ได้

## 4. วงจรงานหนึ่ง sub-task

| ขั้น | ใครทำ | รายละเอียด |
|---|---|---|
| 1. เริ่ม | hpk | `git fetch`; สร้าง worktree จาก `origin/<base>` ล่าสุด; จอง claim (ถาม server ก่อน, connected-mode §5) |
| 2. ทำงาน | agent | commit ใน worktree ได้อิสระ (commit ไม่ใช่เส้นแดง) |
| 3. push | agent ขอ → **คนอนุมัติ** | push branch ของ sub-task เอง; push เข้า base ตรงๆ ถูกปฏิเสธเสมอ (ต้องผ่าน PR) |
| 4. เปิด PR/MR | agent ขอ → **คนอนุมัติ** | **draft** เสมอ; เนื้อหามาจากรายงานของ sub-task (Done / Evidence / Open) + ลิงก์ claim; label `hpk` |
| 5. CI | git host | ผล checks เข้า hpk ⇒ "Evidence: CI ผ่าน" ของ session (ตอนนี้ขึ้น "Not verified yet") |
| 6. รีวิว | คน | ใน GitHub/GitLab ตามปกติ; Console แสดงสถานะ + ลิงก์ |
| 7. merge | **คนเท่านั้น** | ใน GitHub/GitLab หรือปุ่มใน Console (เส้นแดง, ต้อง CI ผ่าน + ตามกฎ branch protection ของ host) |
| 8. หลัง merge | hpk | ปิด claim, session = done, ลบ worktree (ถามก่อน — ลบเป็นเส้นแดง), ร่าง wiki บทเรียน/เวอร์ชันจาก PR (คนอนุมัติ) |

## 5. ตามให้ทัน (sync) แต่ไม่แก้ประวัติเอง

- `hpk sync` (และทุก 5 นาทีตอน `hpk serve` ทำงาน): `git fetch --prune` ทุกโปรเจคที่ผูก — fetch ไม่ใช่เส้นแดง
- base ขยับ ⇒ แจ้ง "base ใหม่ 3 commit, ชนไฟล์ที่คุณแก้ 1 ไฟล์" + ปุ่ม **อัปเดต branch** = `git merge origin/<base>` ใน worktree (ไม่ rebase, ไม่ force-push); มี conflict ⇒ หยุด แล้วให้ agent/คนแก้
- **กันชนกับคนอื่นก่อนเกิด**: server เก็บดัชนี PR ที่เปิดอยู่ + ไฟล์ที่แตะ ⇒ ก่อนจอง claim หรือก่อน push ถ้ามี PR ของคนอื่นแตะไฟล์เดียวกัน ⇒ 🟠 เตือน (ต่อจากบอร์ดเดิม)
- branch หาย/ถูกลบบน remote ⇒ แจ้ง ไม่ลบ worktree เอง

## 6. server รู้สถานะจาก git host อย่างไร

**แนะนำ: webhook + token อ่านอย่างเดียวบน server**

- GitHub / GitLab ส่ง webhook (pull_request / merge_request, check_suite / pipeline, push) มาที่ `https://team…/api/git/webhook/<project>`
  - ตรวจลายเซ็น: GitHub `X-Hub-Signature-256` (HMAC), GitLab `X-Gitlab-Token`; secret อยู่ใน `team.yaml`? → **ไม่**: อยู่ใน `<home>/hpk/git-hooks.secret` สิทธิ์ 600 (ไม่ใช่ token ที่ใช้เข้าถึง repo)
  - เก็บเฉพาะ: เลข PR, branch, ผู้เปิด, สถานะ, ไฟล์ที่แตะ (path), ผล CI — **ไม่เก็บ diff หรือเนื้อโค้ด**
- งานที่ webhook ไม่ให้ (ไฟล์ที่ PR แตะ, รายการ PR ตอนเริ่ม): server เรียก `gh api` / `glab api` ด้วยบัญชีที่ owner ล็อกอินไว้บน server (แนะนำบัญชี bot สิทธิ์อ่าน) — token อยู่ใน gh/glab ไม่ใช่ hpk
- ไม่มี webhook (เช่น server อยู่หลัง firewall) ⇒ server poll ทุก 5 นาทีแทน

## 7. agent บน server (ถ้าทีมรันแชทบน server ด้วย)

- clone ของ server อยู่ใน `/opt/data/code/<project>`; ใช้ worktree เหมือนบนเครื่อง
- push ด้วยบัญชี bot ของทีม; PR ระบุ "เปิดในนามของ <member>" + `Co-authored-by` ของคนสั่งงาน
- เส้นแดงอนุมัติโดย maintainer/owner ใน Console ของ server

## 8. GitHub กับ GitLab

| | GitHub | GitLab |
|---|---|---|
| CLI ที่ใช้ | `gh` | `glab` |
| เปิด draft | `gh pr create --draft` | `glab mr create --draft` |
| CI | checks / check_suite | pipelines |
| webhook ตรวจด้วย | HMAC SHA-256 | secret token |
| self-hosted | GitHub Enterprise (`GH_HOST`) | GitLab self-managed (`GITLAB_HOST`) |

hpk ตรวจชนิด host จาก URL ของ remote (`github.com`, หรือตั้ง `git.host: gitlab` สำหรับ self-hosted)

## 9. Console

- แชท/sub-task: ป้าย branch, ปุ่ม "push" / "เปิด draft PR" (ผ่านการอนุมัติเส้นแดง), สถานะ PR + CI + ลิงก์
- บอร์ด: claim ↔ PR, ไฟล์ชนกับ PR ของคนอื่น
- งานของฉัน: PR ของฉันที่รอรีวิว / CI ล้ม / base ขยับ

## 10. ความปลอดภัย

- ไม่มี token ของ git host ใน hpk, ใน log, ใน project.yaml
- webhook ตรวจลายเซ็นทุกครั้ง, จำกัดขนาด, ไม่เชื่อ path ที่ส่งมา (ใช้แค่แสดง/เทียบ)
- push/merge/ลบ branch ผ่านเส้นแดงเดิม; push เข้า base ตรงๆ ปฏิเสธเสมอ; force-push ปฏิเสธเสมอ
- โปรเจค `private`/`isolated`: push ได้เฉพาะ remote ใน `git.allowed_remotes`; ไม่ส่งดัชนี PR ขึ้น server ทีม (ถ้าไม่ผูก)

## 11. ลำดับงาน

| ขั้น | งาน | ทดสอบ |
|---|---|---|
| G0 | ชื่อ branch ใหม่, `git.base`, `git.allowed_remotes`, push เข้า base/force-push = ปฏิเสธ | unit (gates) |
| G1 | `hpk sync` (fetch), แจ้ง base ขยับ + ไฟล์ชน, ปุ่มอัปเดต branch (merge) | repo ทดสอบ local + bare remote |
| G2 | push + draft PR/MR จากรายงาน sub-task ผ่าน `gh`/`glab` (หลังอนุมัติ) | repo ทดสอบบน GitHub/GitLab จริง (ต้องมี repo ทดสอบ) |
| G3 | webhook + ดัชนี PR บน server, ผล CI → evidence, merge → ปิด claim/ร่าง wiki | Docker server + webhook ทดสอบ |
| G4 | เตือนชนกับ PR ของคนอื่นก่อนจอง/ก่อน push | 2 "เครื่อง" + server |
| G5 | Console ทั้งหมด + docs/git.md | เบราว์เซอร์ |

G0–G1 ไม่ต้องใช้ GitHub/GitLab จริง (ใช้ bare repo ในเครื่อง); G2–G3 ต้องมี repo ทดสอบ (สร้าง repo = ต้องขออนุญาตคุณก่อน)

## 12. คำถามที่ยังเปิด

1. ทีมใช้ GitHub, GitLab.com หรือ GitLab self-hosted (หรือหลายแบบ)?
2. server มีบัญชี bot สำหรับอ่าน PR / push งานของ agent บน server ไหม?
3. หลัง merge ลบ worktree + branch ให้เลยไหม (ยังถามก่อนทุกครั้ง) — แนะนำ: ถามครั้งเดียวต่อ sub-task
4. ชื่อ branch แบบในข้อ 3 โอเคไหม?

## 13. หน้าจอ Git (Console → งาน → Git)

ดูจากโปรแกรม git แบบเสียเงิน (GitKraken, Tower, Fork, Sublime Merge) แล้วเลือก **แนวคิด** ที่ทำให้ใช้ง่ายและตรวจง่าย — เขียนเองทั้งหมด ไม่คัดลอกหน้าตาหรือโค้ด

| กลุ่ม | คำสั่ง | ทำในหน้าจออย่างไร | hpk เพิ่ม |
|---|---|---|---|
| ดูประวัติ | กราฟ commit, branch/tag, remote | เส้น lane สีต่อ branch, จุดกลวง = merge, ป้าย branch + ⇅ ตรงกับ remote / ↑ ยังไม่ push / ● มีแค่ในเครื่อง | ป้าย PR + สถานะ CI, เจ้าของ (claim), งาน (sub-task) |
| ตรวจ commit | รายละเอียด, ไฟล์ที่แก้ | คลิก commit ⇒ แผงขวา: ผู้เขียน, ไฟล์ + แถบ +/− เป็นภาพ | — |
| ประจำวัน | fetch, pull (ff-only), push, branch ใหม่, switch, stash/pop | แถบเครื่องมือ + เมนูบนป้าย branch | push = เส้นแดง |
| รวมงาน | merge, rebase, open draft PR | **ลากป้าย branch ไปวางบนอีก branch** ⇒ เลือก merge / rebase / PR | ภาพก่อน → หลัง |
| แก้ประวัติ | interactive rebase: เรียง, เก็บ, แก้ข้อความ, squash, fixup, drop | ลากเรียงแถว + ปุ่มเลือกต่อแถว + ภาพผลลัพธ์ทันที | จุดย้อนกลับก่อนเริ่ม, branch ที่ push แล้ว = เส้นแดง (force-with-lease) |
| แก้รายจุด | cherry-pick, revert, reset (soft / mixed / hard) | คลิกขวาที่ commit | hard = เส้นแดง (ลบ), reset branch ที่ push แล้ว = เส้นแดง |
| ขัดกัน | conflict ระหว่าง rebase/merge | ของเขา / ของฉัน วางคู่กัน, ปุ่มใช้ของเขา / ของฉัน / ทั้งคู่, ผลลัพธ์, ทำต่อ / ยกเลิก | ยกเลิก = กลับเหมือนเดิมทุกอย่าง |
| ย้อนกลับ | undo | รายการจุดย้อนกลับ (refs/hpk/undo/…) กดคืนค่า | บันทึกอัตโนมัติก่อนทุกคำสั่งที่เขียนประวัติใหม่หรือลบ |
| ทุกคำสั่ง | — | แผ่นยืนยัน: อธิบายเป็นภาษาคน + **คำสั่ง git จริง** + เส้นแดง + จุดย้อนกลับ | agent ขอคำสั่งเดียวกันได้ ผ่านการอนุมัติแบบเดียวกัน |

ยังไม่ทำในต้นแบบ: blame / ประวัติไฟล์, stage ทีละ hunk/บรรทัด, ค้นหา commit, tag, submodule, LFS

### 13.1 ต่อกับ repo จริง (ทำแล้ว)

เปิดผ่าน `hpk serve` หน้าจอนี้ใช้ repo ของโปรเจค; บน dev server ยังเป็นข้อมูลตัวอย่าง

| ส่วน | ทำอย่างไร |
|---|---|
| อ่าน | `GET /api/git?project=` ⇒ `internal/gitops.Load`: `git log --branches --remotes --tags HEAD --topo-order -n 300 --numstat` (จุดย้อนกลับและ stash ไม่ถูกวาด), branch + upstream, stash, ไฟล์ที่ยังไม่ commit, งานที่หยุดเพราะขัดกันในทุก worktree (ทั้งสองฝั่งจาก `git show :2:/:3:`), PR + CI จาก `gh pr list` ถ้ามี `gh` และ remote เป็น GitHub; เจ้าของ/sub-task/claim มาจาก session และบอร์ดของ hpk |
| ทำ | `POST /api/git/action` `{project, action, …}` ⇒ `gitops.Do`: fetch, pull (ff-only), push, branch, checkout, merge, rebase, interactive, cherry, revert, reset, delete, stash, pop, undo, resolve, continue, abort, pr |
| ไม่แตะ checkout ของคน | คำสั่งที่ทำกับ branch รันในโฟลเดอร์ที่ checkout branch นั้นอยู่ (ถ้าสะอาด) ไม่อย่างนั้นใน worktree ชั่วคราวใต้ `p.Dir/git-work` (เก็บไว้ถ้าหยุดเพราะขัดกัน); switch/cherry-pick/revert ปฏิเสธถ้ามีไฟล์ค้าง |
| interactive rebase | cherry-pick ทีละ commit ใน worktree แยกตามลำดับที่คนจัด (keep / reword / squash / fixup / drop); ขัดกันเมื่อไร ⇒ ยกเลิก ไม่มีอะไรเปลี่ยน; แล้วค่อยย้าย branch |
| เส้นแดง | `gitops.RedLine`: push / PR, ลบ branch, reset --hard, merge เข้า base, rebase/interactive ของ branch ที่ push แล้ว (force-with-lease) ⇒ ต้องมีสิทธิ์ `RedLine` ในโหมดทีม และบันทึกเป็นการตัดสินใจของคน (`gates`, Tool `git`); คำสั่งอื่นลง audit |
| ย้อนกลับ | ก่อน merge / rebase / interactive / reset / delete: `refs/hpk/undo/<n>` + `git-undo.json` (เก็บ 30 จุด); คืนค่า = สร้าง branch ที่ถูกลบกลับ, `reset --keep` ในโฟลเดอร์ที่ checkout อยู่, `update-ref` ที่เหลือ |
| ความปลอดภัย | ชื่อ branch ผ่าน `git check-ref-format`, commit id/stash id ตรวจรูปแบบ, `GIT_TERMINAL_PROMPT=0`, ไม่มี editor, timeout 2 นาที, ทำทีละคำสั่ง (mutex); hpk ไม่ถือ token ของ GitHub (ใช้ `gh` ที่คน sign in ไว้) |
| เมื่อ git ปฏิเสธ | Console แสดงข้อความ คำสั่งที่รันไปแล้ว และสิ่งที่ git ตอบ (เช่น ยังไม่ได้ตั้ง `user.name`/`user.email`) |

เทส: `internal/gitops/gitops_test.go` (repo ชั่วคราว + remote แบบ bare), `internal/serve/gitapi_test.go`

## 14. คำแนะนำจากกฎ (ไม่ใช้ AI, 0 token)

`assets/console/js/gitadvice.js` — ฟังก์ชันล้วน: รับรูปร่างของ repo (commit, branch, ตำแหน่งบน remote, PR, stash) คืนคำแนะนำพร้อมเหตุผลและตัวเลขที่ใช้ตัดสิน; เทส `node --test tools/` (อยู่ใน CI)
ตอนต่อ repo จริง hpk คำนวณข้อมูลชุดเดียวกันจาก `git` (ไม่ต้องใช้โมเดล) แล้วใช้กฎชุดนี้

| กฎ | เมื่อไร | แนะนำ | เหตุผลที่แสดง |
|---|---|---|---|
| R1 | branch ตามหลัง base | **rebase** ถ้ายังไม่มีคนอื่นใช้ (ไม่มี PR, เป็นของฉัน); **merge base เข้ามา** ถ้ามี PR หรือเป็นของคนอื่น | จำนวน commit ที่ตามหลัง, ไฟล์ที่น่าจะขัดกัน (ถูกแก้ทั้งสองฝั่ง), ต้อง force-push ไหม |
| R2 | มี commit แบบ `wip`, `fixup!`, `tmp`, ข้อความสั้นมาก และยังไม่ส่งรีวิว (ไม่มี PR หรือ PR ยัง draft) | จัด commit (interactive rebase: รวม / แก้ข้อความ) | ข้อความของ commit เหล่านั้น |
| R3 | มี commit ที่ยังไม่ push / branch มีแค่ในเครื่อง | push | ทีมและ CI ยังไม่เห็น; เครื่องเสีย = งานหาย |
| R4 | remote มี commit ที่เครื่องยังไม่มี (หรือแยกทางกัน) | pull ก่อน แล้วค่อย push | push จะถูกปฏิเสธ |
| R5 | CI ของ PR ไม่ผ่าน | เปิด PR / log ของ CI | merge ไม่ได้จนกว่าจะผ่าน |
| R6 | ทุก commit อยู่ใน base แล้ว | ลบ branch (เส้นแดง, มีจุดย้อนกลับ) | merge แล้ว |
| R7 | ไม่มี commit ใหม่ ≥ 7 วัน | เปิดดู / ทำให้เสร็จ / ส่งต่อ / ลบ | การจองค้างขวางคนอื่น |
| R8 | สอง branch แก้ไฟล์เดียวกัน | ตกลงลำดับ; อันหลัง rebase หลังอันแรก merge | ชื่อไฟล์ที่ชน |
| R9 | stash เก่า ≥ 3 วัน | คืนกลับ หรือทิ้ง | ข้อความของ stash |
| R10 | > 10 commit หรือ > 400 บรรทัด | แยกส่วนต่อไปเป็น branch ใหม่ | รีวิวยาก |
| R11 | มี commit บน base ที่ยังไม่ push | ย้ายไป branch ใหม่ แล้วเปิด PR | ข้ามการรีวิวและบอร์ด |

ลำดับ: แดง (ต้องแก้) → เหลือง (ควรทำก่อนจะยาก) → ข้อมูล; ค่าเกณฑ์ (7 วัน, 3 วัน, 10, 400) ปรับได้ใน `LIMITS`
