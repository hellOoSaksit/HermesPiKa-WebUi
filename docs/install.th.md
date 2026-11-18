English: [install.md](install.md)

# ติดตั้ง hpk

## ติดตั้งแบบเร็ว

ส่วนนี้สำหรับคนที่ใช้ครั้งแรก ใช้เวลาประมาณห้านาที ส่วนที่ละเอียดกว่านี้อยู่ใน [install.md](install.md) (ภาษาอังกฤษ)

### 1. สิ่งที่ต้องมี

- คอมพิวเตอร์ที่ใช้ Windows 10 หรือ 11, macOS หรือ Linux
- อินเทอร์เน็ต
- เว็บเบราว์เซอร์ (Chrome, Edge, Firefox หรือ Safari)
- พื้นที่ว่างในดิสก์ประมาณ 1 GB ถ้า hpk ต้องติดตั้ง Hermes Agent ให้ด้วย
- บัญชีของผู้ให้บริการ AI (เช่น API key หรือการล็อกอิน) ที่ Hermes Agent ใช้คุยกับโมเดล เพิ่มได้หลังเปิดใช้ครั้งแรก

ไม่ต้องใช้สิทธิ์ผู้ดูแลระบบ hpk ติดตั้งให้เฉพาะผู้ใช้ของคุณเท่านั้น

### 2. Windows (Docker คำสั่งเดียว)

บน Windows hpk รันใน Docker คือ Hermes Agent กับ hpk อยู่รวมกันในคอนเทนเนอร์เดียว ไม่มีโปรแกรมติดตั้งลง Windows เลย โปรแกรมแอนตี้ไวรัสจึงไม่มีอะไรให้จับ

1. ติดตั้ง [Docker Desktop](https://www.docker.com/products/docker-desktop/) (ฟรีสำหรับใช้ส่วนตัวและธุรกิจขนาดเล็ก) เปิดมัน แล้วรอจนขึ้นว่ากำลังทำงาน
2. เปิด PowerShell แล้วรัน:

```powershell
irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.ps1 | iex
```

3. สคริปต์จะดาวน์โหลด `compose.release.yaml` ตรวจกับค่า SHA-256 ที่ประกาศไว้ บันทึกไว้พร้อมการตั้งค่าของคุณใน `%USERPROFILE%\hpk` แล้วเปิดคอนเทนเนอร์ ครั้งแรกจะใช้เวลาสักสองสามนาที
4. สคริปต์จะเปิด HPK Console ในเบราว์เซอร์ด้วยลิงก์ใช้ครั้งเดียว เบราว์เซอร์นั้นจะเข้าใช้ได้ 30 วัน hpk รันแบบคนเดียว (คนเดียว ใช้บนเครื่องนี้เท่านั้น) จึงไม่มีบัญชีหรือรหัสผ่าน ครั้งต่อไปให้เปิดจากเมนูสตาร์ต: **HPK Console** ทางลัดจะเปิด hpk ให้ถ้ายังไม่เปิด และเปิดลิงก์ใหม่ให้ ไม่ต้องพิมพ์คำสั่งเลย

โฟลเดอร์ `Documents` ของคุณคือโฟลเดอร์ `/projects` ใน Console ถ้าจะเพิ่มโปรเจคที่อยู่ใน `Documents\my-app` ให้ใส่ `/projects/my-app` ถ้าอยากใช้โฟลเดอร์อื่น ให้ตั้ง `HPK_PROJECTS` (เช่น `$env:HPK_PROJECTS='D:\work'`) ก่อนรันสคริปต์ครั้งแรก หรือแก้ `HPK_PROJECTS` ในไฟล์ `%USERPROFILE%\hpk\.env` แล้วรัน `docker compose -f compose.release.yaml up -d` ในโฟลเดอร์นั้น คอนเทนเนอร์จะเริ่มเองทุกครั้งที่ Docker Desktop ทำงาน

### 3. macOS / Linux (คำสั่งเดียว)

เปิดเทอร์มินัลแล้วรัน:

```sh
curl -fsSL https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.sh | sh
```

สคริปต์จะดาวน์โหลด hpk แล้วตรวจกับค่า SHA-256 ที่ประกาศไว้ (ลายนิ้วมือของไฟล์) ถ้าไม่ตรงจะหยุดทันที จากนั้นติดตั้ง hpk ให้ผู้ใช้ของคุณและเปิด HPK Console

- ติดตั้งเวอร์ชันที่ระบุ: ตั้ง `HPK_VERSION` (เช่น `HPK_VERSION=0.1.0`)
- ติดตั้งโดยไม่เปิด HPK Console: ตั้ง `HPK_NO_START=1`
- บน macOS สคริปต์จะลบเครื่องหมาย "ดาวน์โหลดจากอินเทอร์เน็ต" ออก เพื่อไม่ให้ Gatekeeper ขวาง
- บน macOS และ Linux hpk เพิ่มโฟลเดอร์ของตัวเองและของ Hermes ใน PATH ด้วยบรรทัดที่มีเครื่องหมายกำกับหนึ่งบรรทัด ในไฟล์โปรไฟล์ของเชลล์ (`~/.profile`, `~/.zshrc`, `~/.bashrc`, `~/.bash_profile`) ไฟล์ไหนที่มีอยู่แล้วจะเพิ่มให้ ถ้าไม่มีสักไฟล์ (เช่นบัญชี Mac ใหม่) hpk จะสร้างไฟล์ของเชลล์คุณให้ (`~/.zshrc` สำหรับ zsh, `~/.bash_profile` สำหรับ bash บน macOS, `~/.bashrc` สำหรับ bash บน Linux ไม่เช่นนั้น `~/.profile`) และบอกว่าแก้ไฟล์ไหน
- ถ้าอยู่ในเทอร์มินัล สคริปต์จะเปิด HPK Console แบบเบื้องหน้า (กด Ctrl+C เพื่อหยุด) ถ้าไม่มีเทอร์มินัลจะเปิดเบื้องหลัง แล้วแสดงที่อยู่กับไฟล์บันทึก

คุณรัน `hpk self-install` เองได้เสมอ ถ้าไม่ต้องการให้ hpk ติดตั้งตัวเองอัตโนมัติ ให้ตั้ง `HPK_NO_SELFINSTALL=1`

### 4. Docker

ใช้วิธีนี้ถ้าอยากรัน Hermes Agent กับ hpk รวมกันในคอนเทนเนอร์ ต้องมี Docker ที่มี Compose

1. สร้างโฟลเดอร์ว่างแล้วเปิดเทอร์มินัลในโฟลเดอร์นั้น รันทุกคำสั่งด้านล่างในโฟลเดอร์นี้
2. ดาวน์โหลดไฟล์ compose บน macOS หรือ Linux:

```sh
curl -fsSLO https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/compose.release.yaml
```

บน Windows (PowerShell):

```powershell
irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/compose.release.yaml -OutFile compose.release.yaml
```

3. สร้างไฟล์ `.env` ไว้ข้างไฟล์ compose มีบรรทัดเดียวคือ `HPK_PROJECTS=<โฟลเดอร์ที่เก็บโปรเจคของคุณ>` Console จะเห็นโฟลเดอร์นั้นเป็น `/projects` ถ้าไม่สร้าง จะใช้โฟลเดอร์ `projects` ที่อยู่ข้างไฟล์ compose
4. เริ่มทำงาน:

```sh
docker compose -f compose.release.yaml up -d
```

5. รอสักครู่ให้เริ่มทำงาน แล้วแสดงลิงก์ใช้ครั้งเดียว ถ้าไม่พบไฟล์ ให้รอสักครู่แล้วลองอีกครั้ง:

```sh
docker compose -f compose.release.yaml exec hermes cat /opt/data/hpk/console-link
```

6. เปิดลิงก์นั้นในเบราว์เซอร์ ลิงก์ใช้ได้ครั้งเดียวภายใน 10 นาที แล้วเบราว์เซอร์นั้นจะเข้าใช้ได้ 30 วัน ถ้าจะใช้เบราว์เซอร์อื่นให้แสดงลิงก์ใหม่
7. เพิ่มบัญชีโมเดลในคอนเทนเนอร์:

```sh
docker compose -f compose.release.yaml exec -u hermes hermes hermes auth add <provider>
```

ถ้าใช้ Git Bash บน Windows ให้ใส่ `MSYS_NO_PATHCONV=1` นำหน้าคำสั่ง `docker` ดูเพิ่มที่ [docker.md](docker.md) (ภาษาอังกฤษ)

### 5. เปิดใช้ครั้งแรก: พบหรือติดตั้ง Hermes แล้วเพิ่มบัญชีโมเดล

1. HPK Console ค้นหา Hermes Agent ในเครื่องของคุณ
2. ถ้าเจอ ก็ใช้ตัวนั้น
3. ถ้าไม่เจอ จะเสนอติดตั้งเวอร์ชันที่ทดสอบแล้วคือ Hermes v0.23.1 ลงในโฟลเดอร์ของ hpk เอง โดยแสดงขนาดที่ต้องดาวน์โหลดและแหล่งที่มาทั้งหมดก่อน จะไม่ดาวน์โหลดอะไรจนกว่าคุณจะติ๊ก "I agree" และกด Install
4. จากนั้นจะแสดงการเปลี่ยนแปลงเดียวที่จะทำกับ Hermes (เพิ่ม hpk) กด "Add hpk to Hermes" เพื่อยอมรับ
5. สุดท้าย เพิ่มบัญชีโมเดล เปิดเทอร์มินัล**ใหม่** (เพื่อให้หา `hermes` เจอ) แล้วรัน:

```sh
hermes auth add <provider>
```

   บน macOS และ Linux ทำใน Console ได้ด้วย: **Settings** แล้วไปที่ **AI accounts** มีฟอร์มสำหรับเพิ่มคีย์ บน Windows หน้านั้นจะแสดงคำสั่งนี้ ถ้าไม่มีบัญชี เอเจนต์จะตอบไม่ได้

### 6. อัปเดต

รันการติดตั้งแบบเดิมอีกครั้ง (ข้อ 2 หรือ 3) มันจะหยุด hpk ที่กำลังทำงานอยู่ก่อน แล้วแทนที่ตัวเก่า ข้อมูลและโปรเจกต์ของคุณอยู่เหมือนเดิม

### 7. ถอนการติดตั้ง

```sh
hpk self-uninstall
```

ก่อนอื่น ถ้าคุณเพิ่ม hpk เข้า Hermes ไว้ ให้เอาออกก่อน:

```sh
hpk uninstall --yes
```

จากนั้น `hpk self-uninstall` จะลบ hpk, รายการใน PATH, ทางลัดในเมนู Start และการเริ่มตอนล็อกอิน แต่ไม่แตะข้อมูลของ hpk และโฟลเดอร์ Hermes ของคุณ จึงไม่มีอะไรหาย โฟลเดอร์ของ Hermes ที่ hpk ติดตั้งให้จะออกจาก PATH ด้วย ดังนั้นคำสั่ง `hermes` จะหาไม่เจอในเทอร์มินัลใหม่ (ไฟล์ยังอยู่ในโฟลเดอร์ข้อมูล) ถ้าจะลบทั้งหมด ให้ลบโฟลเดอร์ข้อมูลเอง: `%LOCALAPPDATA%\hpk` บน Windows, `~/Library/Application Support/hpk` บน macOS, `~/.local/share/hpk` บน Linux (`$XDG_DATA_HOME/hpk` ถ้าคุณตั้งไว้)

### 8. แก้ปัญหา

- **"Port 7878 is in use"** มีโปรแกรมอื่นหรือ hpk อีกตัวใช้พอร์ตนี้อยู่ รัน `hpk stop` หรือเริ่มด้วยพอร์ตอื่น: `hpk --addr 127.0.0.1:7879`
- **ติดตั้งแล้วแต่หา `hpk` ไม่เจอ** การเปลี่ยน PATH มีผลกับเทอร์มินัลที่เปิดหลังจากนั้นเท่านั้น ปิดเทอร์มินัลแล้วเปิดใหม่
- **macOS แจ้งว่าเปิดแอปไม่ได้ (Gatekeeper)** ใช้การติดตั้งด้วยคำสั่งเดียว (ข้อ 3) ซึ่งลบเครื่องหมายให้ หรือรัน `xattr -d com.apple.quarantine <path to hpk>`
- **Windows ขึ้นว่า "Docker is not installed" หรือ "not running"** ติดตั้งหรือเปิด Docker Desktop รอจนขึ้นว่ากำลังทำงาน แล้วรันคำสั่งติดตั้งอีกครั้ง
- **ใช้ผ่าน SSH (เครื่องนั้นไม่มีเบราว์เซอร์)** ส่งต่อพอร์ตแล้วเปิดที่อยู่บนเครื่องของคุณเอง: `ssh -L 7878:127.0.0.1:7878 you@host` แล้วเปิด http://127.0.0.1:7878 บนเครื่องคุณ
- **ปัญหาอื่น** รัน `hpk doctor` มันตรวจ Hermes, hooks, โปรเจกต์ และ Console

รายละเอียดเต็ม (ภาษาอังกฤษ): [install.md](install.md)
