# ⚡ SAJON SEEKHO — Zero se Hero tak!
### Tumhari apni language, tumhara apna cloud ☁️

---

> 🎯 **Yeh guide kiske liye hai?**
> Agar tumhe programming ki koi knowledge nahi, koi DevOps nahi pata, cloud ka naam sunke bhi darr lagte ho —
> **yahi guide tumhare liye hai.**
> Ek ek cheez simple Hinglish mein, step by step, mazedaar examples ke saath!

---

## 📖 TABLE OF CONTENTS

| # | Topic | Level |
|---|-------|-------|
| 1 | [Sajon Kya Hai? — Ek Desi Story](#chapter-1) | 🟢 Beginner |
| 2 | [Setup — Sajon Ko Chalao!](#chapter-2) | 🟢 Beginner |
| 3 | [Pehla `.saj` File — Hello Cloud!](#chapter-3) | 🟢 Beginner |
| 4 | [RESOURCE Block — Database Banao](#chapter-4) | 🟡 Intermediate |
| 5 | [ENV Block — Secret Secrets!](#chapter-5) | 🟡 Intermediate |
| 6 | [ENDPOINT Block — API Routes](#chapter-6) | 🟡 Intermediate |
| 7 | [WORKER Block — Background Kaam](#chapter-7) | 🟡 Intermediate |
| 8 | [SERVER & STORAGE — EC2 aur S3](#chapter-8) | 🟡 Intermediate |
| 9 | [CLI Commands — Sajon Ka Remote Control](#chapter-9) | 🟡 Intermediate |
| 10 | [State Management — Sajon Ka Memory!](#chapter-10) | 🔴 Advanced |
| 11 | [CI/CD — Auto Deploy Magic!](#chapter-11) | 🔴 Advanced |
| 12 | [Multi-Cloud Setup — Sab Kuch Ek File Mein](#chapter-12) | 🔴 Advanced |
| 13 | [Security Best Practices](#chapter-13) | 🔴 Advanced |
| 14 | [Real World Project — Ek Puri App!](#chapter-14) | 🔴 Advanced |

---

<a name="chapter-1"></a>
## 🎬 Chapter 1: Sajon Kya Hai? — Ek Desi Story

### Imagine Karo...

Tumhe ek chai ki dukaan kholni hai. Tum chai banate ho, customers ko dete ho — lekin tumhe **dukaan setup** bhi karni padegi:
- Counter lagao
- Gas cylinder connect karo
- Pani ka connection lo
- Bijli lao

Yahi kaam ek app ke saath hota hai. App banane se pehle tumhe **cloud infrastructure** setup karni padti hai:
- Database banao (jahan data store hoga)
- Server setup karo (jahan app chalegi)
- Storage set karo (jahan files rahegi)

**Pehle yeh sab karne mein 3-4 ghante lagte the!** 😩

---

### Sajon Aaya! 🦸

**Sajon ek magic wand hai** jo tumhara cloud infrastructure automatically set karta hai.

Tum sirf ek chhoti si file likhte ho (`.saj` file) — aur Sajon sab kuch automatically:
- ✅ Database cloud par bana deta hai
- ✅ Tables create kar deta hai
- ✅ Connection strings tumhari file mein likh deta hai
- ✅ Local development ke liye Docker setup karta hai

```
Tumhara .saj file  →→→  Sajon  →→→  Live Cloud! ☁️
(30 second mein!)
```

### Real Life Comparison

| Cheez | Bina Sajon | Sajon ke Saath |
|-------|-----------|----------------|
| Database banana | 45 min (dashboards, configs) | 30 seconds |
| SQL migration likhna | 30 min | Automatic! |
| .env file setup | 20 min | Automatic! |
| CI/CD pipeline | 2-3 ghante | 1 command! |

> 💡 **Simple baat:** Sajon woh kaam karta hai jo pehle DevOps engineers karte the — aur woh bhi ek simple file likhke!

---

<a name="chapter-2"></a>
## 🚀 Chapter 2: Setup — Sajon Ko Chalao!

### Windows Par Setup (Tumhara system hai yeh!)

Tumhare paas already `sajon.exe` hai `Downloads\Sajon` folder mein. Bas itna karo:

**Step 1: PowerShell kholo**
> Start menu → "PowerShell" search karo → kholo

**Step 2: Apne folder mein jao**
```powershell
cd "C:\Users\asadur rahman\Downloads\Sajon"
```

**Step 3: Verify karo ki Sajon kaam kar raha hai**
```powershell
.\sajon.exe version
```

Agar output aaya — **Congratulations! Sajon ready hai!** 🎉

---

### Cloud Accounts Ki Zaroorat

Sajon teen cloud providers support karta hai. Tumhe jiske saath kaam karna ho, uska account chahiye:

| Cloud Provider | Kya Karta Hai | Free Tier? |
|---------------|--------------|------------|
| **Supabase** | Database + Auth + Storage | ✅ Haan (generous!) |
| **Neon** | Serverless Postgres DB | ✅ Haan |
| **AWS** | Server (EC2) + Storage (S3) + Database (RDS) | ⚠️ Limited |

> 🌟 **Beginner Tip:** Pehle **Supabase** se shuru karo — free hai, fast hai, aur Sajon ke saath best kaam karta hai!

### Supabase Token Kaise Milega?

1. `supabase.com` par jao
2. Account banao (free hai!)
3. Dashboard → Account → Access Tokens
4. "Generate New Token" click karo
5. Token copy karo

**Phir PowerShell mein yeh likho:**
```powershell
$env:SUPABASE_ACCESS_TOKEN="tumhara_token_yahan"
```

> ⚠️ **Dhyan do:** Yeh token kabhi kisi ke saath share mat karo! Jaise ATM PIN hota hai.

---

<a name="chapter-3"></a>
## 🌱 Chapter 3: Pehla `.saj` File — Hello Cloud!

### `.saj` File Kya Hoti Hai?

`.saj` extension wali file Sajon ki language mein likhi jaati hai. Yeh bahut simple hai — almost English jaisi!

Ek naya file banao: `meri_pehli_app.saj`

```ruby
# Yeh meri pehli Sajon file hai!
# (#) se shuru hone wali lines comments hain — Sajon inhe ignore karta hai

RESOURCE mera_database {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "users"
        fields: ["id:int", "name:string", "email:string"]
    }
}
```

### Isko Samjhein:

```
RESOURCE mera_database {    ← "Ek cheez banao jiska naam hai mera_database"
    provider: "supabase"    ← "Supabase cloud par banao"
    region:   "ap-south-1"  ← "India wala server use karo"
    SCHEMA {                ← "Table structure define karo"
        table:  "users"     ← "Table ka naam 'users'"
        fields: [...]       ← "Columns kya honge"
    }
}
```

### Pehle Plan Karo, Phir Deploy Karo!

```powershell
# Pehle DRY RUN — koi actual cloud call nahi hoga
.\sajon.exe plan meri_pehli_app.saj
```

Yeh tumhe batayega ki kya kya hoga — bina actually kiye! **Insurance check ki tarah!** 🛡️

```powershell
# Jab ready ho, toh deploy karo
.\sajon.exe up meri_pehli_app.saj
```

### Kya Hoga Baad Mein?

Sajon yeh sab automatically karega:
```
✔ Supabase project created
✔ Table 'users' auto-migrated in live database  
✔ sajon.env written — DATABASE_URL ready
✔ docker-compose.yml generated
```

Aur tumhari folder mein ek `sajon.env` file aayegi jisme tumhara database URL hoga! 🎊

---

<a name="chapter-4"></a>
## 🗄️ Chapter 4: RESOURCE Block — Database Banao

### Database Kya Hota Hai? (Bilkul Simple)

Database ek **organized table** ki tarah hota hai. Jaise tumhari dukaan ki register book:

| id | name | email | created_at |
|----|------|-------|-----------|
| 1 | Ali | ali@gmail.com | 2024-01-01 |
| 2 | Sara | sara@gmail.com | 2024-01-02 |

Yeh ek "users" table hai! Database mein aisi kai tables hoti hain.

---

### RESOURCE Block Ka Full Syntax

```ruby
RESOURCE naam_jo_tumhe_dena_hai {
    provider: "supabase"    # Ya "neon" ya "aws" ya "postgres"
    region:   "ap-south-1"
    SCHEMA {
        table:  "table_ka_naam"
        fields: [
            "column_naam:type",
            "column_naam:type"
        ]
    }
}
```

---

### Field Types — Sajon Ki Vocabulary

Yeh samjho jaise Hindi ke alag alag words hote hain, Sajon mein alag data types hote hain:

| Sajon Type | SQL Mein Kya Banta Hai | Use Karo Jab... |
|-----------|----------------------|----------------|
| `int` | `SERIAL PRIMARY KEY` | Numbers ke liye (id, age, quantity) |
| `string` | `VARCHAR(255)` | Chhote text ke liye (name, city) |
| `text` | `TEXT` | Bade text ke liye (bio, description) |
| `float` | `NUMERIC(10,2)` | Decimal numbers (price, rating) |
| `bool` | `BOOLEAN` | Haan/Nahi wali cheezein (active, verified) |
| `timestamp` | `TIMESTAMP DEFAULT NOW()` | Date/time (created_at, updated_at) |
| `uuid` | `UUID DEFAULT gen_random_uuid()` | Unique IDs (token, session_id) |

---

### Real World Example: Ek E-Commerce App

```ruby
# Products ki table
RESOURCE products_db {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "products"
        fields: [
            "id:int",               # Auto-incrementing ID
            "title:string",          # Product ka naam
            "description:text",      # Lamba description
            "price:float",           # Price jaise 299.99
            "in_stock:bool",         # Available hai ya nahi
            "image_url:string",      # Image ka link
            "created_at:timestamp"   # Kab add hua
        ]
    }
}
```

---

### Multiple Tables Ek File Mein!

```ruby
# Users ka database
RESOURCE user_db {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "users"
        fields: ["id:int", "name:string", "email:string", "active:bool"]
    }
}

# Orders ka database
RESOURCE order_db {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "orders"
        fields: ["id:int", "user_id:int", "total:float", "status:string", "ordered_at:timestamp"]
    }
}
```

> 💡 **Pro Tip:** Ek app mein multiple RESOURCE blocks ho sakte hain — alag alag databases ke liye!

---

<a name="chapter-5"></a>
## 🔐 Chapter 5: ENV Block — Secret Secrets!

### ENV Block Kya Hota Hai?

Environment variables woh **secret settings** hoti hain jo tumhari app ko kaam karne ke liye chahiye — jaise passwords, API keys, settings.

**Yeh kabhi bhi code mein directly mat likho!** (Jaise ATM PIN ki photo nahi leni chahiye 😄)

### ENV Block Ka Syntax

```ruby
ENV production {
    APP_ENV:   "production"
    LOG_LEVEL: "info"
    DEBUG_MODE: "false"
    PORT:       "8080"
}
```

### Real World Example

```ruby
# Development settings
ENV development {
    APP_ENV:    "development"
    LOG_LEVEL:  "debug"
    DEBUG_MODE: "true"
    PORT:       "3000"
}

# Production settings (live app ke liye)
ENV production {
    APP_ENV:    "production"
    LOG_LEVEL:  "error"
    DEBUG_MODE: "false"
    PORT:       "8080"
}
```

### Important Rules!

1. **Real secrets ENV block mein mat likho** — yeh file Git mein jaati hai!
2. Real secrets ke liye shell environment use karo:
   ```powershell
   # PowerShell mein
   $env:SECRET_KEY = "tumhara_actual_secret"
   ```
3. `.env` files `sajon.env` mein automatically generate hoti hain

> ⚠️ **Golden Rule:** Agar cheez secret hai (password, API key, token) — toh woh `.saj` file mein kabhi mat daalo!

---

<a name="chapter-6"></a>
## 🌐 Chapter 6: ENDPOINT Block — API Routes

### API Kya Hota Hai?

Socho ek restaurant ka menu. Tum waiter ko kehte ho:
- "Chai do" → Waiter kitchen jaata hai, chai laata hai
- "Bill do" → Waiter bill laata hai

**API bhi aisi hi hoti hai** — tumhari app ke different "menu items" (endpoints) hote hain.

### ENDPOINT Block Ka Syntax

```ruby
ENDPOINT METHOD "/path" {
    RETURN "response"
}
```

**Methods:**
- `GET` — Data mangna (fetching)
- `POST` — Data bhejna (creating)
- `PUT` — Data update karna
- `DELETE` — Data delete karna

---

### Real Examples

```ruby
# Health check — app chal rahi hai ya nahi?
ENDPOINT GET "/health" {
    RETURN "OK"
}

# Naye user ka signup
ENDPOINT POST "/signup" {
    RETURN "User Created"
}

# Saari users ki list
ENDPOINT GET "/users" {
    RETURN "UserList"
}

# Specific user ki profile
ENDPOINT GET "/users/profile" {
    RETURN "UserProfile"
}

# User delete karo
ENDPOINT DELETE "/users/delete" {
    RETURN "Deleted"
}
```

---

### Restaurant Analogy

```
GET    "/menu"       →  Menu dikhao        (sirf dekhna)
POST   "/order"      →  Naya order karo    (kuch banana)
PUT    "/order/edit" →  Order badlo        (update karna)
DELETE "/order/cancel" →  Order cancel karo (hatana)
```

> 💡 **Note:** Sajon ke ENDPOINT blocks abhi documentation aur planning ke liye hain. Actual HTTP server logic tumhari app (Node.js, Python, etc.) handle karti hai.

---

<a name="chapter-7"></a>
## ⚙️ Chapter 7: WORKER Block — Background Kaam

### Worker Kya Hota Hai?

Socho ek restaurant mein:
- Waiter customers ke saath baat karta hai (main app)
- Kitchen mein cook khana banata rehta hai (background worker)

**Worker** woh kaam karta hai jo **background mein** chalta rehta hai — bina user ko wait karaaye.

### WORKER Block Ka Syntax

```ruby
WORKER naam {
    queue:       "queue_ka_naam"
    concurrency: 5
}
```

- **queue**: Kaunsa kaam queue mein aayega
- **concurrency**: Ek saath kitne kaam handle karo

---

### Real World Examples

```ruby
# Email bhejne wala worker
WORKER email_sender {
    queue:       "emails"
    concurrency: 10
}

# Report generate karne wala worker
WORKER report_builder {
    queue:       "reports"
    concurrency: 2
}

# Image process karne wala worker
WORKER image_processor {
    queue:       "images"
    concurrency: 5
}

# Notification bhejne wala worker
WORKER notification_dispatcher {
    queue:       "notifications"
    concurrency: 8
}
```

---

### Kab Use Karo?

Workers use karo jab:
- ✅ Email bhejna ho
- ✅ Large file process karni ho
- ✅ PDF generate karna ho
- ✅ SMS notifications bhejna ho
- ✅ Heavy calculations karni hon

Workers mat use karo jab:
- ❌ User ko turant response chahiye
- ❌ Simple database query ho

---

<a name="chapter-8"></a>
## 🖥️ Chapter 8: SERVER & STORAGE — EC2 aur S3

### SERVER Block — AWS EC2 (Virtual Computer)

EC2 ek **virtual computer** hai jo AWS ke cloud mein rehta hai. Yahan tumhari app run hoti hai.

```ruby
SERVER api_server {
    provider:      "aws"
    instance_type: "t3.medium"
    ami:           "ami-0c55b159cbfafe1f0"
    region:        "us-east-1"
}
```

**Instance Types — Computer Ki Size:**

| Type | CPU | RAM | Use Case |
|------|-----|-----|----------|
| `t3.micro` | 2 | 1 GB | Chhoti apps, testing |
| `t3.small` | 2 | 2 GB | Light production |
| `t3.medium` | 2 | 4 GB | Medium apps ✅ |
| `t3.large` | 2 | 8 GB | Heavy apps |

---

### STORAGE Block — AWS S3 (Cloud Ki Almaari)

S3 ek **cloud storage** hai jahan tumhare files, images, videos store hote hain.

```ruby
STORAGE media_bucket {
    provider: "aws"
    bucket:   "meri-app-media"
    region:   "us-east-1"
}
```

> 💡 **Tip:** Bucket naam unique hona chahiye globally! "meri-app-media-2024" jaisa naam use karo.

---

### SERVER + STORAGE Saath Mein

```ruby
# App server
SERVER web_server {
    provider:      "aws"
    instance_type: "t3.medium"
    ami:           "ami-0c55b159cbfafe1f0"
    region:        "ap-south-1"
}

# Files ke liye storage
STORAGE user_uploads {
    provider: "aws"
    bucket:   "myapp-user-uploads-prod"
    region:   "ap-south-1"
}

STORAGE backups {
    provider: "aws"
    bucket:   "myapp-database-backups"
    region:   "ap-south-1"
}
```

---

<a name="chapter-9"></a>
## 🎮 Chapter 9: CLI Commands — Sajon Ka Remote Control

### Saare Commands Ek Jagah

```
sajon plan [file.saj]          →  Preview karo, koi cloud call nahi
sajon up [file.saj]            →  Deploy karo! Cloud par provision karo
sajon up [file.saj] --force    →  Force deploy (warnings ignore karo)
sajon down [--force]           →  Sab kuch destroy karo
sajon ci github [file.saj]     →  GitHub Actions pipeline banao
sajon version                  →  Version dekho
sajon help                     →  Help dekho
```

---

### Proper Workflow (Hamesha Is Order Mein!)

```
1️⃣  .saj file likho
         ↓
2️⃣  sajon plan    (pehle check karo!)
         ↓
3️⃣  sajon up      (deploy karo)
         ↓
4️⃣  App use karo!
         ↓
5️⃣  Jab khatam → sajon down
```

---

### Commands Ka Deep Dive

#### `sajon plan` — Inspector Jaisi Cheez

```powershell
.\sajon.exe plan app.saj
```

Yeh batata hai:
- Kya resources banaaye jaayenge
- Kya already exist karta hai
- Kya changes honge
- **Koi actual cloud call nahi hota!**

#### `sajon up` — Deployment Ka Button

```powershell
.\sajon.exe up app.saj
```

Yeh karta hai:
- `.saj` file parse karta hai
- Cloud APIs call karta hai
- Resources provision karta hai
- `sajon.env` file likhta hai
- `sajon.lock` update karta hai
- `docker-compose.yml` generate karta hai

#### `sajon down` — Sab Band Karo

```powershell
.\sajon.exe down
```

> ⚠️ **WARNING:** Yeh command sab kuch delete karta hai — database, server, storage. **Data wapas nahi aata!** Soch samajhke use karo!

#### `sajon ci github` — GitHub Pipeline Magic

```powershell
.\sajon.exe ci github app.saj
```

Yeh `.github/workflows/sajon-deploy.yml` file create karta hai — ek puri CI/CD pipeline!

---

<a name="chapter-10"></a>
## 🧠 Chapter 10: State Management — Sajon Ka Memory!

### `sajon.lock` Kya Hota Hai?

Sajon ko yaad rakhna padta hai ki usne kya kya banaya hai. Iske liye woh `sajon.lock` file use karta hai.

Socho yeh ek **notebook** hai jisme Sajon likhta hai:
> "Maine user_db naam ka Supabase project banaya, uska ID hai 'abcde12345'"

Iska matlab — jab tum dobara `sajon up` run karo toh Sajon **duplicate nahi banata** — existing resource ko reuse karta hai!

---

### Lock File Ka Format

```json
{
  "resources": {
    "user_db": {
      "provider": "supabase",
      "project_id": "abcdefghijklmnop",
      "connection_string": "postgresql://...",
      "status": "active"
    }
  }
}
```

---

### Orphan Guard — Data Bachane Wala Hero 🛡️

Socho tumne ek resource banaya:
```ruby
RESOURCE user_db {
    provider: "supabase"
    ...
}
```

Ab galti se naam change kar diya:
```ruby
RESOURCE users_database {    # ← naam badla!
    provider: "supabase"
    ...
}
```

**Bina protection ke:** Sajon sochta — "user_db toh nahi hai, delete karo!" → Data gone! 😱

**Sajon ke Orphan Guard ke saath:** Sajon kehta — "Ruko! Yeh purana resource abhi bhi hai, sure ho delete karna chahte ho?" ✅

> 💡 **Lesson:** Resource ka naam kabhi bhi change mat karo bina soche. Agar karna hi hai toh `--force` flag use karo aur data backup karo pehle!

---

### Remote State — Team Ke Saath Kaam

Agar team mein kaam kar rahe ho toh `sajon.lock` ek jagah honi chahiye — S3 bucket mein!

```powershell
# Pehle S3 bucket banao
# Phir environment variable set karo:
$env:SAJON_REMOTE_BUCKET = "tumhara-sajon-state-bucket"
```

Ab saari team ka same state share hoga!

---

### `.gitignore` Mein Daalo!

```
# .gitignore file mein yeh lines daalo:
sajon.lock    # Ismein connection strings hain!
sajon.env     # Ismein bhi secrets hain!
*.env
```

---

<a name="chapter-11"></a>
## 🔄 Chapter 11: CI/CD — Auto Deploy Magic!

### CI/CD Kya Hota Hai?

**CI = Continuous Integration** — Har code change automatically test ho
**CD = Continuous Deployment** — Test pass hone par automatically deploy ho

Easy language mein: **"Code push karo → Automatically deploy ho jaaye!"**

---

### Sajon Ke Saath CI/CD

```powershell
.\sajon.exe ci github app.saj
```

Yeh command `.github/workflows/sajon-deploy.yml` file banata hai.

---

### Generated Pipeline Kya Karta Hai?

```yaml
# Automatically generate hoti hai yeh file!

on:
  pull_request:
    branches: [main]    # PR par sajon plan run hoga
  push:
    branches: [main]    # Main par push hone par sajon up run hoga
```

**Flow:**
```
Developer code likhta hai
        ↓
Git push karta hai
        ↓
GitHub Actions automatically trigger hota hai
        ↓
sajon plan run hota hai (PR par)
        ↓
Review aur merge hota hai
        ↓
sajon up run hota hai (automatic deploy!)
        ↓
sajon.env aur sajon.lock GitHub Artifacts mein save hote hain
```

---

### GitHub Secrets Setup

CI/CD ke liye tokens GitHub par secure store karo:

1. GitHub → Repository → Settings → Secrets and Variables → Actions
2. "New repository secret" click karo
3. Yeh secrets add karo:
   - `SUPABASE_ACCESS_TOKEN`
   - `NEON_API_KEY` (agar Neon use kar rahe ho)
   - `AWS_ACCESS_KEY_ID` (agar AWS use kar rahe ho)
   - `AWS_SECRET_ACCESS_KEY`

---

<a name="chapter-12"></a>
## ☁️ Chapter 12: Multi-Cloud Setup — Sab Kuch Ek File Mein

### Real World Complete App

Yeh dekho ek complete production app ka `.saj` file:

```ruby
# ================================================================
# production_app.saj — Complete Production Setup
# ================================================================

# ── Environment ──────────────────────────────────────────────────

ENV production {
    APP_ENV:    "production"
    LOG_LEVEL:  "info"
    DEBUG_MODE: "false"
    PORT:       "8080"
}

# ── Primary Database (Supabase — Fast & Free) ─────────────────────

RESOURCE main_db {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "users"
        fields: [
            "id:int",
            "name:string",
            "email:string",
            "phone:string",
            "role:string",
            "active:bool",
            "created_at:timestamp"
        ]
    }
}

# ── Products Database ─────────────────────────────────────────────

RESOURCE products_db {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "products"
        fields: [
            "id:int",
            "name:string",
            "description:text",
            "price:float",
            "category:string",
            "stock:int",
            "image_url:string",
            "in_stock:bool",
            "created_at:timestamp"
        ]
    }
}

# ── AWS Server (App Run Hone Ki Jagah) ────────────────────────────

SERVER app_server {
    provider:      "aws"
    instance_type: "t3.medium"
    ami:           "ami-0c55b159cbfafe1f0"
    region:        "ap-south-1"
}

# ── AWS S3 Storage ────────────────────────────────────────────────

STORAGE product_images {
    provider: "aws"
    bucket:   "myapp-product-images-2024"
    region:   "ap-south-1"
}

STORAGE user_documents {
    provider: "aws"
    bucket:   "myapp-user-docs-2024"
    region:   "ap-south-1"
}

# ── API Endpoints ─────────────────────────────────────────────────

ENDPOINT GET "/health" {
    RETURN "OK"
}

ENDPOINT POST "/auth/signup" {
    RETURN "User Created"
}

ENDPOINT POST "/auth/login" {
    RETURN "Token"
}

ENDPOINT GET "/products" {
    RETURN "ProductList"
}

ENDPOINT POST "/products" {
    RETURN "Product Created"
}

ENDPOINT POST "/orders" {
    RETURN "Order Created"
}

# ── Background Workers ────────────────────────────────────────────

WORKER email_sender {
    queue:       "emails"
    concurrency: 10
}

WORKER order_processor {
    queue:       "orders"
    concurrency: 5
}

WORKER image_optimizer {
    queue:       "images"
    concurrency: 3
}
```

---

### Deploy Karo:

```powershell
# Dry run pehle
.\sajon.exe plan production_app.saj

# Deploy!
.\sajon.exe up production_app.saj

# CI/CD pipeline generate karo
.\sajon.exe ci github production_app.saj
```

**Bas! Teri poori production infrastructure ready! 🚀**

---

<a name="chapter-13"></a>
## 🔐 Chapter 13: Security Best Practices

### Golden Rules of Sajon Security

#### Rule 1: Kabhi Bhi Secrets `.saj` File Mein Mat Likho ❌

```ruby
# ❌ GALAT — Yeh kabhi mat karo!
ENV production {
    DB_PASSWORD: "mera_secret_password_123"
    API_KEY:     "sk-live-abcdefghijklmnop"
}
```

```powershell
# ✅ SAHI — Shell environment mein set karo
$env:DB_PASSWORD = "mera_secret_password_123"
$env:API_KEY = "sk-live-abcdefghijklmnop"
```

---

#### Rule 2: `.gitignore` Hamesha Update Karo ✅

```gitignore
# Sajon files — inhe Git mein push mat karo!
sajon.lock
sajon.env
*.env
.env*
sajon_secrets.*
```

---

#### Rule 3: `sajon.env` Ko Samjho

Sajon automatically `sajon.env` file banata hai jisme tumhare database URLs hote hain:

```env
# Auto-generated by Sajon — sirf tumhara software padhega yeh
SUPABASE_DATABASE_URL_MAIN_DB="postgresql://postgres:REDACTED@db.xyz.supabase.co/postgres"
SUPABASE_URL_MAIN_DB="https://xyz.supabase.co"
```

**Note:** Passwords terminal mein show nahi hote (Sajon automatically redact karta hai) ✅

---

#### Rule 4: `sajon.lock` Ko Team Ke Saath Share Karo (Safely)

Team project mein S3 use karo state ke liye:
```powershell
$env:SAJON_REMOTE_BUCKET = "meri-company-sajon-state"
```

Phir local `sajon.lock` file ko share mat karo — S3 se automatically sync hoga!

---

#### Rule 5: `sajon down` Se Pehle Backup Lo! ⚠️

```
Before sajon down:
1. Database ka backup lo (Supabase dashboard se)
2. Zaruri files S3 se download karo
3. PHIR sajon down chalao
```

---

<a name="chapter-14"></a>
## 🏆 Chapter 14: Real World Project — Ek Puri App!

### Project: "DukannApp" — Online Dukaan

Chalo ek complete online dukaan ka infrastructure banate hain!

#### Step 1: Planning

Humari dukaan ko chahiye:
- Users ka database (customers)
- Products ka database
- Orders track karne ke liye
- Image storage (products ki photos)
- Email notifications (order confirmation)
- Health check endpoint

#### Step 2: `.saj` File Banao

`dukann_app.saj` file create karo:

```ruby
# ================================================================
# dukann_app.saj — DukannApp Infrastructure
# Online dukaan ka poora cloud setup!
# ================================================================

ENV production {
    APP_ENV:    "production"
    LOG_LEVEL:  "info"
    APP_NAME:   "DukannApp"
}

# ── Customers Database ────────────────────────────────────────────
RESOURCE customer_db {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "customers"
        fields: [
            "id:int",
            "name:string",
            "email:string",
            "phone:string",
            "address:text",
            "verified:bool",
            "joined_at:timestamp"
        ]
    }
}

# ── Products Database ─────────────────────────────────────────────
RESOURCE product_db {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "products"
        fields: [
            "id:int",
            "name:string",
            "description:text",
            "price:float",
            "category:string",
            "stock_count:int",
            "image_url:string",
            "available:bool",
            "added_at:timestamp"
        ]
    }
}

# ── Orders Database ───────────────────────────────────────────────
RESOURCE order_db {
    provider: "supabase"
    region:   "ap-south-1"
    SCHEMA {
        table:  "orders"
        fields: [
            "id:int",
            "customer_id:int",
            "product_id:int",
            "quantity:int",
            "total_amount:float",
            "status:string",
            "delivery_address:text",
            "ordered_at:timestamp"
        ]
    }
}

# ── Product Images Storage ────────────────────────────────────────
STORAGE product_images {
    provider: "aws"
    bucket:   "dukannapp-product-images-2024"
    region:   "ap-south-1"
}

# ── API Endpoints ─────────────────────────────────────────────────

ENDPOINT GET "/health" {
    RETURN "DukannApp is Running!"
}

ENDPOINT POST "/customers/register" {
    RETURN "Customer Registered"
}

ENDPOINT GET "/products" {
    RETURN "ProductList"
}

ENDPOINT GET "/products/search" {
    RETURN "SearchResults"
}

ENDPOINT POST "/orders/place" {
    RETURN "Order Placed"
}

ENDPOINT GET "/orders/track" {
    RETURN "OrderStatus"
}

# ── Background Workers ────────────────────────────────────────────

WORKER order_confirmation_email {
    queue:       "order-emails"
    concurrency: 5
}

WORKER inventory_updater {
    queue:       "inventory"
    concurrency: 2
}

WORKER payment_processor {
    queue:       "payments"
    concurrency: 3
}
```

#### Step 3: Deploy Karo!

```powershell
# Credentials set karo
$env:SUPABASE_ACCESS_TOKEN = "tumhara_supabase_token"

# Pehle dry run
.\sajon.exe plan dukann_app.saj

# Sab theek laga? Deploy!
.\sajon.exe up dukann_app.saj
```

#### Step 4: `sajon.env` Use Karo Apni App Mein

Sajon automatically yeh file banayega:
```env
SUPABASE_DATABASE_URL_CUSTOMER_DB="postgresql://..."
SUPABASE_DATABASE_URL_PRODUCT_DB="postgresql://..."
SUPABASE_DATABASE_URL_ORDER_DB="postgresql://..."
```

Ab inhe apni Node.js ya Python app mein use karo:
```javascript
// Node.js mein
require('dotenv').config({ path: 'sajon.env' })
const customerDb = process.env.SUPABASE_DATABASE_URL_CUSTOMER_DB
```

#### Step 5: CI/CD Pipeline

```powershell
.\sajon.exe ci github dukann_app.saj
```

**Ab GitHub par push karo aur automatic deploy ho jaayega!** 🎊

---

## 🎓 Quick Reference Card

Yeh save karke rakho!

```
📁 File Structure
.saj file    →  Tumhara infrastructure code
sajon.lock   →  Sajon ki memory (Git mein mat daalo!)
sajon.env    →  Tumhare database URLs (Git mein mat daalo!)

🏗️ Blocks
RESOURCE  →  Database banao
SERVER    →  EC2 virtual server
STORAGE   →  S3 file storage
ENV       →  Environment variables
ENDPOINT  →  API routes define karo
WORKER    →  Background jobs

🌐 Providers
supabase  →  Best for beginners! Free tier generous
neon      →  Serverless Postgres
aws       →  Production-grade (EC2, RDS, S3)

💻 Commands
sajon plan  →  Pehle check karo (safe!)
sajon up    →  Deploy karo!
sajon down  →  Sab band karo (dangerous!)
sajon ci    →  GitHub pipeline banao

📝 Field Types
int        →  Numbers (1, 2, 100)
string     →  Chhota text ("Ali")
text       →  Lamba text (biography)
float      →  Decimal (29.99)
bool       →  True/False
timestamp  →  Date & Time
uuid       →  Unique ID
```

---

## 🚀 Agle Kadam

Ab jo Sajon seekh liya hai, yahan se aage badho:

1. **Practice karo** — `meri_pehli_app.saj` banao aur `sajon plan` run karo
2. **Supabase free account** banao aur real deploy try karo
3. **GitHub par daalo** — `sajon ci github` se pipeline banao
4. **Team ke saath** try karo — Remote state (S3) setup karo
5. **Roadmap dekho** — Sajon mein naye features aane wale hain (login, web UI, Railway support!)

---

## 💬 Yaad Rakho

> **"Sajon ne cloud infrastructure ko ek simple file mein compress kar diya hai."**

Pehle jo kaam 3 DevOps engineers 3 ghante mein karte the —
Ab tum akele 30 second mein kar sakte ho.

**Aur ab tum yeh sab jaante ho! Mubarak ho! 🎉**

---

*Guide written with ❤️ — Zero DevOps Required. Sajon FTW!*
