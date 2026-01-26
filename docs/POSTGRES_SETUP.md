# Yandex Cloud Managed PostgreSQL Connection Setup

## Your Cluster Information

- **Cluster name**: postgresql106
- **Cluster ID**: c9q4ti8th22kbmunjceg
- **Version**: PostgreSQL 16
- **Environment**: PRODUCTION
- **Hosts**: 
  - `rc1a-fec715cq0rept3kd.mdb.yandexcloud.net` (read-write)
  - `rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net` (read-write)
- **Port**: 6432
- **User**: vodeneevbet

## Getting Connection Data

### 1. Database Hosts

Your cluster has multiple hosts for high availability. Use both hosts separated by comma in DSN string:

```
rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net
```

### 2. Get FQDN Hosts (if need to update)

1. Open [Yandex Cloud Console](https://console.cloud.yandex.ru)
2. Go to **Managed Service for PostgreSQL**
3. Select cluster **postgresql106**
4. Go to **Hosts** tab
5. Copy **FQDN** of all hosts (separated by comma for high availability)

### 2. Create Database (if not created yet)

1. In cluster console go to **Databases** tab
2. Create database (e.g., `arb_db` or use existing `db`)

### 3. Configure Access Rights

Make sure user `vodeneevbet` has permissions to create tables in the database.

### 4. Get User Password

User `vodeneevbet` password should be set in Yandex Cloud console or when creating the user.

## Connection Setup

### Option 1: Via Environment Variable (recommended)

```bash
export POSTGRES_DSN="host=<host1>,<host2> port=6432 user=<username> password=<password> dbname=<dbname> sslmode=require target_session_attrs=read-write"
```

**Example for your cluster:**
```bash
export POSTGRES_DSN="host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevbet password=your_password dbname=db sslmode=require target_session_attrs=read-write"
```

### Option 2: Via Configuration File

Edit `configs/production.yaml`:

```yaml
postgres:
  dsn: "host=<host1>,<host2> port=6432 user=<username> password=<password> dbname=<dbname> sslmode=require target_session_attrs=read-write"
```

**Example for your cluster:**
```yaml
postgres:
  dsn: "host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net port=6432 user=vodeneevbet password=your_password dbname=db sslmode=require target_session_attrs=read-write"
```

### Important Parameters

- **Multiple hosts**: Specify all hosts separated by comma for high availability
- **target_session_attrs=read-write**: Ensures connection to host with write permissions
- **sslmode=require**: Required SSL (for Go applications `require` is enough, `verify-full` is not needed)

## Testing Connection (optional)

To test connection via `psql`:

### 1. Install PostgreSQL Client

```bash
sudo apt update && sudo apt install --yes postgresql-client
```

### 2. Install Certificate (for psql with verify-full)

```bash
mkdir -p ~/.postgresql && \
wget "https://storage.yandexcloud.net/cloud-certs/CA.pem" \
    --output-document ~/.postgresql/root.crt && \
chmod 0655 ~/.postgresql/root.crt
```

### 3. Connect to Database

```bash
psql "host=rc1a-fec715cq0rept3kd.mdb.yandexcloud.net,rc1a-h8tc88gfbg9v7anh.mdb.yandexcloud.net \
    port=6432 \
    sslmode=verify-full \
    dbname=db \
    user=vodeneevbet \
    target_session_attrs=read-write"
```

### 4. Test Connection

```sql
SELECT version();
```

**Note**: For Go applications certificate is not required, `sslmode=require` is enough. Certificate is only needed for `psql` with `sslmode=verify-full`.

## Verifying Connection

After setup, run calculator:

```bash
go run ./cmd/calculator -config configs/production.yaml
```

Logs should show:
```
calculator: PostgreSQL diff storage initialized successfully
```

## Security

⚠️ **Important**: Never commit passwords to repository!

- Use environment variables for passwords
- Add `.env` file to `.gitignore`
- Use secrets in CI/CD systems

## Troubleshooting

### Error: "connection refused"
- Check that you're using correct port (6432, not 5432)
- Make sure FQDN is specified correctly

### Error: "SSL required"
- Add `sslmode=require` to DSN string

### Error: "authentication failed"
- Check username and password
- Make sure user exists in cluster

### Error: "database does not exist"
- Create database in Yandex Cloud console
- Check that database name is specified correctly
