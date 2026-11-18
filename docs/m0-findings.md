# ผลตรวจ M0

ตรวจกับ Hermes Agent **v0.23.1** (2026-09-24) บน Windows 11 เมื่อ 2026-11-17

วิธีตรวจ:
- อ่านซอร์สของ Hermes ที่ติดตั้งในเครื่อง และรันได้เฉพาะ `--help` กับ `--version`
- ไม่เปิดแชท ไม่เรียกโมเดล ไม่แก้ config ของ Hermes และไม่เปิดไฟล์ `.env`
- path ที่อ้างถึงด้านล่างเป็น path ในซอร์สของ Hermes

## สรุปสั้น: สิ่งที่เปลี่ยนการออกแบบ

1. **`hermes` CLI เริ่มทำงานช้า ประมาณ 3.3 วินาทีต่อครั้ง**
   - hpk จึงต้องคุยผ่าน process `hermes-acp` ที่เปิดค้างไว้
   - ใช้ CLI เฉพาะงานนาน ๆ ครั้ง เช่น rename, cron, auth
2. **hook สั่ง "ให้คนตัดสิน" ได้จริง**
   - `pre_tool_call` ตอบ `{"action":"approve"}` แล้ว Hermes จะส่งเรื่องเข้าด่านอนุมัติของตัวเอง
   - เส้นแดงของ hpk จึงทำงานได้ทั้งใน session ที่ hpk สร้างและ session ที่ไม่ได้สร้าง
   - `hpk hook` ตอบในรูปแบบนี้แล้ว (`internal/hook`)
3. **การสลับบัญชี AI ใช้ของ Hermes**
   - Hermes มี credential pool ที่จัดลำดับได้ และหมุนบัญชีเองเมื่อเจอ 429
   - หน้า "บัญชี AI" จึงสั่ง `hermes auth priority` / `auth add --priority` แทนการสลับเอง
4. **หนึ่งโปรไฟล์ต่อหนึ่ง process**
   - ถ้าโปรเจคใช้โปรไฟล์ต่างกัน hpk ต้องเปิด `hermes-acp` แยก process ต่อโปรไฟล์
   - `WIKI_PATH` ตั้งได้ต่อ process เท่านั้น ส่วน cwd ตั้งได้ต่อ session
5. **cron ไม่มีเพดาน token ต่องาน** งบของงานตั้งเวลาต้องให้ hpk คุมเอง

## ผลตามคำถาม §18 และ §24.6 / §25.5

### 1. ติดตั้ง skill / hook / cron จาก git repo — ได้บางส่วน
- **Skill:** ได้ 2 ทาง
  - `hermes skills tap add owner/repo` แล้ว `hermes skills install … --yes`
  - เพิ่มโฟลเดอร์ใน `skills.external_dirs` ของ `config.yaml` (`hermes_cli/config_defaults.py:1432`)
- **Hook:** ใส่ใต้คีย์ `hooks:` ใน `config.yaml`
  - ครั้งแรกที่รัน hook ใดก็ตาม ต้องได้รับความยินยอมก่อน
  - ช่องทางยินยอม: prompt บน TTY, `--accept-hooks`, `HERMES_ACCEPT_HOOKS=1` หรือ `hooks_auto_accept: true`
- **Cron:** ไม่มีการติดตั้งจาก repo
  - งานถูกเก็บใน `cron/jobs.json` ของแต่ละโปรไฟล์
  - ต้องสร้างด้วย `hermes cron create` ห้ามเขียนไฟล์ตรง
- **hpk:** `hpk install` จะทำ 3 อย่าง
  - เพิ่ม `external_dirs`
  - รวม `hooks:` เข้า `config.yaml` โดยแสดง diff ให้คนยืนยันก่อน เพราะเป็นเส้นแดงประเภท "ไฟล์ควบคุม agent"
  - สร้าง cron ผ่าน CLI

### 2. hook บังคับ "ถามคน" ได้ไหม — ได้
- **ผลลัพธ์ของ `pre_tool_call`:**
  - `{"action":"block"}` หรือ exit code 2 จะบล็อก
  - `{"action":"approve","message":…}` จะส่งเข้าด่านอนุมัติ
  - ตอบ `modify` ได้ด้วย (`agent/shell_hooks.py:637`)
- **ACP:** ไม่ได้ส่ง permission request มาทุกคำสั่ง ส่งเฉพาะเมื่อระบบอนุมัติยกระดับเรื่อง
  - ค่าเริ่มต้น `approvals.mode: smart` ให้ LLM ตัดสินก่อน
  - ตัวเลือกที่ ACP ส่งมา: allow once / session / always และ deny
  - ถ้าไม่ตอบใน 300 วินาที จะถือว่าปฏิเสธ
- **hpk:** เส้นแดงต้องตัดสินใน hook ของเราเอง จะพึ่งการตัดสินของ smart mode ไม่ได้

### 3. hook ก่อนบีบอัด context / จบรอบ — ได้บางส่วน
- **ก่อนบีบอัด:** ไม่มี event สำหรับ shell hook
  - มีแค่ `on_pre_compress()` ของ memory-provider plugin (`agent/memory_provider.py:208`)
  - หลังบีบอัดมี `post_compaction`
- **จบรอบ:** `on_session_end` ยิงทุกรอบ พร้อมสถานะ completed / interrupted / failed
- **จบ session:** `on_session_finalize`
- **hpk:** `pre_compress_capture` ต้องเป็น memory-provider plugin หรือเลื่อนไปทำที่ `post_compaction`

### 4. `hermes-acp` ทำอะไรได้บ้าง — ได้บางส่วน
- **ได้:**
  - new / load / resume / fork / list / cancel session
  - ตั้งโมเดลต่อ session
  - ตั้งโหมดและค่า config
  - ตั้ง cwd ต่อ session
  - รับ permission request
- **ไม่ได้:**
  - ตั้งชื่อ session: ACP ส่งชื่ออัตโนมัติมาให้เท่านั้น ถ้าจะตั้งเองใช้ `hermes sessions rename`
  - เลือก worktree หรือโปรไฟล์ต่อ session
  - ระบุแม่ใตอน fork
- **hpk:** เก็บความสัมพันธ์แม่-ลูกเองใน `board.db` ส่วน worktree ใช้วิธีตั้ง cwd ของ session ให้ชี้ไปที่ worktree

### 5. `WIKI_PATH` อ่านตามโปรไฟล์ไหม — ได้ แต่ต่อ process
- skill `llm-wiki` อ่าน `${WIKI_PATH:-$HOME/wiki}` จาก environment
- Hermes โหลด `.env` ของโปรไฟล์ที่ active เข้า process และไม่ได้กรอง `WIKI_PATH` ออก
- ยังไม่ได้ตรวจกรณี gateway ที่รันหลายโปรไฟล์ใน process เดียว

### 6. cron เลือกโมเดลและตั้งเพดาน token ได้ไหม — โมเดลได้ token ไม่ได้
- เลือกโมเดลได้ด้วย `--model`, `--provider` และ `--reasoning-effort`
- ไม่มีเพดาน token ต่องาน มีแค่ `agent.max_turns` ที่ใช้ทั้งระบบ
- **hpk:** คุมงบเอง ใช้โปรไฟล์แยกสำหรับงาน batch และหยุดงานเมื่อเกินงบ

### 7. ชื่อโปรเจค — ยังใช้ "Hermes Project Kit / hpk" เป็นชื่อชั่วคราว

### 8. หลายผู้ใช้บน server — ได้บางส่วน
- ตาราง session มี `user_id` และ gateway แยก session ตามผู้ใช้ของแต่ละ platform
- CLI และ ACP ไม่มีแนวคิดเจ้าของ session
- **hpk:** Console รู้ตัวตนผู้ใช้ แล้วติดป้ายเจ้าของ session เอง ตามที่ออกแบบไว้ใน §19

### 9. ส่งข้อความต่อคิวโดยไม่ขัดจังหวะ — ได้
- ACP และ CLI รองรับ `/queue <ข้อความ>` และ `/steer <คำชี้นำ>`
- ข้อความธรรมดาที่ส่งระหว่างรอบอาจเปลี่ยนทิศของรอบนั้น
- **hpk:** ส่งเป็น `/queue` หรือ `/steer` เสมอ ให้ตรงกับปุ่มในแชท

### 10. ใช้ claim ของ Kanban ได้ไหม — ได้
- มีฟิลด์ `claim_lock` และ `claim_expires` และการ claim ใช้ compare-and-swap ใน SQLite
- เครื่องมือภายนอกควรสั่งผ่าน CLI: `hermes kanban claim | heartbeat | complete`
- **hpk:** เป็น backend ทางเลือกของ BOARD สำหรับโปรเจคที่ใช้ Kanban อยู่แล้ว ค่าเริ่มต้นยังเป็น `board.db` ของเรา

### 11. รูปแบบ hook — ชัดแล้ว
- **ตั้งค่าใน `config.yaml`:**
  - `hooks.<event>: [{command, matcher, timeout (60/สูงสุด 300), fail_closed}]`
- **stdin:** JSON
  - `{hook_event_name, tool_name, tool_input, session_id, cwd, profile, extra}`
- **stdout:** JSON ตามตาราง

  | event | รูปแบบคำตอบ |
  |---|---|
  | `pre_tool_call` | `action: block / approve / modify` |
  | `pre_llm_call` | `block` หรือ `{"context": …}` |
  | event อื่น | `{"context": …}` |

- **exit 2** บล็อกได้เฉพาะ `pre_tool_call`
- **เมื่อ hook พัง:** event อื่นปล่อยผ่าน เว้นแต่ตั้ง `fail_closed: true` ซึ่ง hpk ต้องตั้งสำหรับเส้นแดงเสมอ
- **event ที่ใช้ได้:** ดูใน `hermes_cli/plugins.py:111` เช่น
  - pre / post_tool_call
  - pre / post_approval_request
  - on_session_start / end / finalize
  - subagent_start / stop
  - on_kanban_*

### 12. เรียก `hermes` CLI จาก binary ภายนอกบน Windows — ได้ แต่ช้า
- ใช้ได้:
  - `chat -q` และ `--query-file -`
  - `--format stream-json`
  - `sessions list` / `rename`
  - `cron` และ `kanban`
- ข้อจำกัด:
  - `sessions list` ไม่มีผลลัพธ์แบบ JSON
  - `chat -q` ปฏิเสธคำสั่งอันตรายเป็นค่าเริ่มต้น
  - เริ่มทำงานช้าประมาณ 3.3 วินาทีต่อครั้ง
- **hpk:** ใช้ ACP แบบ process ค้างเป็นหลัก และอ่าน session จาก `state.db` แบบอ่านอย่างเดียวผ่าน adapter (§22.2)

### §24.6 credential pool — ได้
- กลยุทธ์มี fill_first, round_robin, random และ least_used ตั้งต่อผู้ให้บริการ
- ตั้งลำดับด้วย `hermes auth priority`
- เมื่อเจอ 429 จะหมุนไปบัญชีถัดไปเอง
- พักบัญชีที่ใช้ไม่ได้ (cooldown): 1 ชั่วโมงหลัง 429/402 และ 5 นาทีหลัง 401
- **hpk:** ใช้ pool ของ Hermes หน้า "บัญชี AI" ทำหน้าที่จัดลำดับและแสดงสถานะ ส่วนการสลับข้ามผู้ให้บริการยังเป็นเส้นแดง

### §25.5 โปรไฟล์ต่อ session — ได้บางส่วน
- โปรไฟล์คือ `HERMES_HOME` หนึ่งชุด
- หลายโปรไฟล์พร้อมกันได้ถ้าแยก process ด้วย `-p` หรือ `HERMES_HOME`
- ตั้ง cwd ต่อ session ได้ แต่ env เช่น `WIKI_PATH` ตั้งต่อ session ไม่ได้

## ตัวเลขที่วัดได้ (เทียบเป้า §21.5)

| ตัวชี้วัด | เป้า | วัดได้ (Windows 11) |
|---|---|---|
| `hpk hook noop` หนึ่งครั้ง (median 30 ครั้ง) | < 80 ms | **16 ms** (p90 21 ms) |
| `git --version` เพื่อเทียบ | — | 57 ms |
| `hermes --version` (median 10 ครั้ง) | — | ~3,345 ms |
| `hpk serve` RAM ตอนว่าง (working set) | < 30 MB | **11.6 MB** |
| binary (`-s -w`) | < 30 MB | **7.3 MB** |
| Docker image | < 40 MB | ยังไม่ได้สร้าง (ทำใน M9) |

CI (`.github/workflows/ci.yml`) รันทดสอบบน Linux, macOS และ Windows และเช็กงบ hook latency กับขนาด binary ทุกครั้ง

## ยังค้าง

- ยังไม่ได้ทดลอง hook จริงใน Hermes: ต้องเพิ่ม `hooks:` ใน `config.yaml` และกดยินยอม ซึ่งต้องได้รับอนุญาตจากเจ้าของเครื่องก่อน จึงยกไปทำตอนเริ่ม M4
- ยังไม่ได้เปิด `hermes-acp` จริง ทำใน M5
- ก่อนเผยแพร่ ควรให้ผู้รู้ด้านกฎหมายตรวจไฟล์ LICENSE
- ชื่อ module ของ Go ยังเป็น `hpk.local/hpk` ต้องเปลี่ยนเป็น path ของ repo จริงเมื่อขึ้น GitHub
