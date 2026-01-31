# GitHub Secrets Setup for Deployment

To enable async processing and Telegram notifications, you need to configure secrets in GitHub Actions.

## Required Secrets

### 1. POSTGRES_DSN (required for async mode)

Connection string to Yandex Cloud Managed PostgreSQL.

**Format:**
```
host=<host1>,<host2> port=6432 user=<username> password=<password> dbname=<dbname> sslmode=require target_session_attrs=read-write
```

**Example for your cluster:**
```
host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevm password=your_secure_password dbname=db sslmode=require target_session_attrs=read-write
```

**⚠️ Important:** User must be `vodeneevm` (not `vodeneevbet`!)

**Important:**
- Specify all hosts separated by comma for high availability
- Parameter `target_session_attrs=read-write` ensures connection to host with write permissions

**How to get:**
1. Open [Yandex Cloud Console](https://console.cloud.yandex.ru)
2. Managed Service for PostgreSQL → cluster **postgresql106**
3. **Hosts** tab → copy FQDN
4. **Users** tab → create/use user
5. **Databases** tab → create/use database

### 2. TELEGRAM_BOT_TOKEN (required for notifications)

Your Telegram bot token.

**How to get:**
1. Open [@BotFather](https://t.me/botfather) in Telegram
2. Send `/newbot` and follow instructions
3. Copy token (format: `123456789:ABCdefGHIjklMNOpqrsTUVwxyz`)

### 3. TELEGRAM_CHAT_ID (required for notifications)

Chat ID where notifications will be sent.

**How to get (choose one method):**

**Method 1: Via @userinfobot (for personal chats) - simplest**
1. Open [@userinfobot](https://t.me/userinfobot) in Telegram
2. Send `/start`
3. Bot will show your Chat ID (number, e.g.: `123456789`)

**Method 2: Via @RawDataBot (for groups)**
1. Create a group or open existing one
2. Add [@RawDataBot](https://t.me/RawDataBot) to the group
3. Send any message in the group
4. Bot will reply with JSON data - find `chat.id` (this is the Group Chat ID)

**Method 3: Via @getidsbot**
1. Open [@getidsbot](https://t.me/getidsbot) in Telegram
2. Send `/start`
3. Bot will show your Chat ID

**Method 4: Via your bot (if already running)**
1. Send any message to your bot (e.g., `/start`)
2. Check bot logs - there will be Chat ID from incoming message
3. Or temporarily add logging to bot code to output Chat ID

**Note:** 
- For personal chats use your User ID (get via @userinfobot)
- For groups use Group ID (get via @RawDataBot)
- Chat ID is a number (can be negative for groups)

## Setting Up Secrets in GitHub

### Via Web Interface:

1. Open your repository on GitHub
2. Go to **Settings** → **Secrets and variables** → **Actions**
3. If secret already exists:
   - Find the secret in the list (e.g., `POSTGRES_DSN`)
   - Click on it, then click **Update** (or **Edit**)
   - Paste new value
   - Click **Update secret**
4. If secret doesn't exist:
   - Click **New repository secret**
   - Add each secret:

     - **Name**: `POSTGRES_DSN`
     - **Secret**: `host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevm password=your_password dbname=db sslmode=require target_session_attrs=read-write`
     
     ⚠️ **Important:** Use user `vodeneevm` (not `vodeneevbet`!)

     - **Name**: `TELEGRAM_BOT_TOKEN`
     - **Secret**: `123456789:ABCdefGHIjklMNOpqrsTUVwxyz`

     - **Name**: `TELEGRAM_CHAT_ID`
     - **Secret**: `123456789`

   - Click **Add secret**

### Via GitHub CLI:

```bash
# Install GitHub CLI if not installed
# brew install gh  # macOS
# apt install gh   # Linux

# Authenticate
gh auth login

# Add or update secrets (set command updates existing secret)
gh secret set POSTGRES_DSN --body "host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevm password=your_password dbname=db sslmode=require target_session_attrs=read-write"
gh secret set TELEGRAM_BOT_TOKEN --body "123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
gh secret set TELEGRAM_CHAT_ID --body "123456789"
```

## Verifying Secrets

After adding secrets:

1. Run deployment (push to `main` or via **Actions** → **Run workflow**)
2. Check deployment logs - there should be no errors about missing secrets
3. After deployment check calculator logs:
   ```bash
   ssh vm-core-services 'sudo docker logs vodeneevbet-calculator'
   ```
4. You should see lines:
   ```
   calculator: PostgreSQL diff storage initialized successfully
   calculator: telegram notifier initialized for chat <ID>
   calculator: starting async processing with interval 30s
   ```

## Existing Secrets

You already have the following secrets configured (don't touch them):
- `VM_CORE_HOST` - Core VM IP or hostname (e.g. `158.160.222.217`)
- `VM_PARSERS_HOST` - Parsers VM IP or hostname (e.g. `158.160.168.187`)
- `VM_USER` - SSH user for both VMs (e.g. `nphne-tuxzcf6w` for Yandex Cloud); default `vodeneevm` if not set
- `SSH_PRIVATE_KEY` - SSH private key for **parsers** VM (full PEM, including `-----BEGIN ...-----` / `-----END ...-----`)
- `SSH_PRIVATE_KEY_CORE` - SSH private key for **core** VM (same PEM format). **Required if core VM uses a different key than parsers.** If not set, `SSH_PRIVATE_KEY` is used for core too (then both VMs must accept the same key).
- `GHCR_TOKEN` - token for GitHub Container Registry
- `GHCR_USERNAME` - username for GHCR (optional)

### Optional: деплой контор (bookmaker services) на отдельную VM

Чтобы после пуша в `main` автоматически деплоились конторы (fonbet, pinnacle, pinnacle888) на **158.160.159.73**:

1. **Settings** → **Secrets and variables** → **Actions** → вкладка **Variables** → **New repository variable**
2. Добавь переменную **`VM_BOOKMAKER_HOST`** со значением **`158.160.159.73`** (именно Variable, не Secret — иначе job нельзя пропустить по условию)
3. Если на этой VM другой SSH-ключ — в **Secrets** добавь **`SSH_PRIVATE_KEY_BOOKMAKER`** (полный PEM ключ). Если не добавить — используется тот же ключ, что и для parsers (`SSH_PRIVATE_KEY`).

После этого при каждом пуше в `main` workflow будет деплоить конторы на 158.160.159.73. Если переменная `VM_BOOKMAKER_HOST` не задана — job деплоя контор пропускается.

## Security

⚠️ **Important:**
- Never commit secrets to code
- Don't log secrets to console
- Regularly update passwords
- Use different passwords for different environments (dev/staging/prod)

## Troubleshooting

### Error: "Permission denied (publickey)" on Core deploy

Core job uses `SSH_PRIVATE_KEY_CORE` (or `SSH_PRIVATE_KEY` if core secret is not set). If the core VM has a **different** SSH key than the parsers VM:

1. In GitHub: **Settings** → **Secrets and variables** → **Actions**
2. Add secret **`SSH_PRIVATE_KEY_CORE`** with the **private key that works for the core VM** (the one you use to `ssh` into core, full PEM including `-----BEGIN ...-----` and `-----END ...-----`).
3. Re-run the workflow.

To get the key: if you connect to core with `ssh -i /path/to/core_key user@core-ip`, use the contents of `core_key` as `SSH_PRIVATE_KEY_CORE`.

### Error: "postgres DSN is required"
- Make sure `POSTGRES_DSN` secret is added to GitHub Secrets
- Check that value is not empty

### Error: "failed to initialize PostgreSQL storage"
- Check DSN string correctness
- Make sure FQDN, port, user, password and database name are correct
- Check that SSL is enabled (`sslmode=require`)

### Error: "telegram notifier not initialized"
- Check that `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` are added
- Make sure token is valid (can check via [@BotFather](https://t.me/botfather))
