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

### 2. Windows (ดับเบิลคลิก)

1. เปิด https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest แล้วดาวน์โหลด `hpk.exe`
2. ดับเบิลคลิก `hpk.exe`
3. ถ้า Windows ขึ้น "Windows protected your PC" ให้กด **More info** แล้วกด **Run anyway** โปรแกรมยังไม่ได้ลงลายเซ็น Windows จึงถามหนึ่งครั้ง
4. hpk จะคัดลอกตัวเองไปที่ `%LOCALAPPDATA%\hpk\bin` เพิ่มโฟลเดอร์นั้นและโฟลเดอร์ของ Hermes (`%LOCALAPPDATA%\hpk\hermes\bin`) ใน PATH (รายชื่อโฟลเดอร์ที่ Windows ใช้หาโปรแกรม) เพิ่มทางลัดในเมนู Start ชื่อ "HPK Console" แล้วเปิด HPK Console ในเบราว์เซอร์

หลังจากนั้นเปิดได้จากเมนู Start หรือพิมพ์ `hpk` ในเทอร์มินัลที่เปิดใหม่

### 3. Windows / macOS / Linux (คำสั่งเดียว)

Windows: เปิด PowerShell แล้วรัน:

```powershell
irm https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.ps1 | iex
```

macOS และ Linux: เปิดเทอร์มินัลแล้วรัน:

```sh
curl -fsSL https://github.com/hellOoSaksit/HermesPiKa-WebUi/releases/latest/download/install.sh | sh
```

สคริปต์จะดาวน์โหลด hpk แล้วตรวจกับค่า SHA-256 ที่ประกาศไว้ (ลายนิ้วมือของไฟล์) ถ้าไม่ตรงจะหยุดทันที จากนั้นติดตั้ง hpk ให้ผู้ใช้ของคุณและเปิด HPK Console

- ติดตั้งเวอร์ชันที่ระบุ: ตั้ง `HPK_VERSION` (เช่น `HPK_VERSION=0.1.0`)
- ติดตั้งโดยไม่เปิด HPK Console: ตั้ง `HPK_NO_START=1`
- บน macOS สคริปต์จะลบเครื่องหมาย "ดาวน์โหลดจากอินเทอร์เน็ต" ออก เพื่อไม่ให้ Gatekeeper ขวาง
- บน macOS และ Linux hpk เพิ่มโฟลเดอร์ของตัวเองและของ Hermes ใน PATH ด้วยบรรทัดที่มีเครื่องหมายกำกับหนึ่งบรรทัด ในไฟล์ `~/.profile`, `~/.zshrc` และ `~/.bashrc` (เฉพาะไฟล์ที่มีอยู่แล้ว)

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

3. เปิด `compose.release.yaml` ด้วยโปรแกรมแก้ข้อความ แล้วแก้ `HPK_OWNER` เป็นอีเมลของคุณ ใช้เฉพาะตอนเริ่มครั้งแรก
4. เริ่มทำงาน:

```sh
docker compose -f compose.release.yaml up -d
```

5. แสดง owner token (กุญแจเข้าระบบครั้งแรกของคุณ):

```sh
docker compose -f compose.release.yaml exec hermes cat /opt/data/hpk/owner-token
```

6. เปิด http://127.0.0.1:7878 ในเบราว์เซอร์แล้ววาง token จากนั้นลบไฟล์ token ทิ้ง:

```sh
docker compose -f compose.release.yaml exec hermes rm /opt/data/hpk/owner-token
```

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

คำสั่งนี้ลบ hpk, รายการใน PATH และทางลัดในเมนู Start แต่ไม่แตะข้อมูลของ hpk และโฟลเดอร์ Hermes ของคุณ จึงไม่มีอะไรหาย ถ้าจะลบสองอย่างนั้นด้วย ให้ลบโฟลเดอร์เอง (บน Windows คือ `%LOCALAPPDATA%\hpk`)

### 8. แก้ปัญหา

- **"Port 7878 is in use"** มีโปรแกรมอื่นหรือ hpk อีกตัวใช้พอร์ตนี้อยู่ รัน `hpk stop` หรือเริ่มด้วยพอร์ตอื่น: `hpk --addr 127.0.0.1:7879`
- **ติดตั้งแล้วแต่หา `hpk` ไม่เจอ** การเปลี่ยน PATH มีผลกับเทอร์มินัลที่เปิดหลังจากนั้นเท่านั้น ปิดเทอร์มินัลแล้วเปิดใหม่
- **macOS แจ้งว่าเปิดแอปไม่ได้ (Gatekeeper)** ใช้การติดตั้งด้วยคำสั่งเดียว (ข้อ 3) ซึ่งลบเครื่องหมายให้ หรือรัน `xattr -d com.apple.quarantine <path to hpk>`
- **Windows SmartScreen ขวางอยู่** กด **More info** แล้วกด **Run anyway**
- **ปัญหาอื่น** รัน `hpk doctor` มันตรวจ Hermes, hooks, โปรเจกต์ และ Console

รายละเอียดเต็ม (ภาษาอังกฤษ): [install.md](install.md)
