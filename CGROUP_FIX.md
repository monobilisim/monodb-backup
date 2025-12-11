# Fix for Cgroup PID Limit Exhaustion

## Problem

When backing up MySQL databases with many tables using `backupAsTables: true`, the backup process was failing with errors like:

```
fork/exec /usr/bin/gzip: resource temporarily unavailable
cgroup: fork rejected by pids controller in /system.slice/cron.service
```

This occurs because:
1. Each table backup spawns a `mysqldump` process
2. Each dump is piped to a compression process (`gzip` or `7z`)
3. When backing up databases with hundreds of tables, all these processes accumulate and exceed the cgroup's process limit (`pids.max`)

## Solution

### Code Changes

The fix introduces a **semaphore-based concurrency control** mechanism that limits the number of concurrent dump/compression processes:

1. **New Configuration Parameter** (`config/params.go`):
   - Added `MaxConcurrentProcesses` field to control the maximum number of simultaneous dump operations
   - Default value: 10 processes
   - Can be configured in the YAML config file

2. **Process Semaphore** (`backup/mysql.go`):
   - Implemented a semaphore using a buffered channel
   - Each table dump operation acquires a semaphore slot before starting
   - Releases the slot after completion
   - Uses goroutines with `sync.WaitGroup` for concurrent but controlled execution

3. **Configuration File** (`config/config.sample.yml`):
   - Added `maxConcurrentProcesses` parameter with documentation

### Configuration

Add this parameter to your `config.yml`:

```yaml
maxConcurrentProcesses: 10  # Adjust based on your cgroup limits
```

**Recommended values:**
- **Conservative**: 5-10 (for cgroup pids.max around 500-1000)
- **Moderate**: 10-20 (for cgroup pids.max around 1000-2000)
- **Aggressive**: 20-50 (for cgroup pids.max >= 4000)

### How to Determine the Right Value

1. Check your current cgroup limit:
   ```bash
   cat /sys/fs/cgroup/system.slice/cron.service/pids.max
   ```

2. Monitor current process usage:
   ```bash
   cat /sys/fs/cgroup/system.slice/cron.service/pids.current
   ```

3. Calculate safe limit:
   - Each table dump creates ~2 processes (mysqldump + compression)
   - Reserve some processes for other cron jobs
   - Formula: `maxConcurrentProcesses ≈ (pids.max - 100) / 3`
   
   Example: If `pids.max = 932`, use: `(932 - 100) / 3 ≈ 277` concurrent processes
   However, starting with 10-20 is recommended for stability.

### Alternative: Increase Cgroup Limit

If you prefer to increase the system limit instead:

```bash
# Temporary (until reboot)
echo 4096 > /sys/fs/cgroup/system.slice/cron.service/pids.max

# Permanent (systemd)
mkdir -p /etc/systemd/system/cron.service.d/
cat > /etc/systemd/system/cron.service.d/override.conf <<EOF
[Service]
TasksMax=4096
EOF

systemctl daemon-reload
systemctl restart cron
```

## Benefits

1. **Prevents resource exhaustion**: Controlled concurrency prevents hitting cgroup limits
2. **Maintains parallelism**: Still processes multiple tables concurrently for speed
3. **Configurable**: Easy to tune based on your system's constraints
4. **Backward compatible**: Uses sensible defaults if not configured

## Testing

After applying this fix:

1. Rebuild the application:
   ```bash
   go build
   ```

2. Update your configuration file with appropriate `maxConcurrentProcesses` value

3. Run a backup and monitor:
   ```bash
   # Watch process count during backup
   watch -n 1 'cat /sys/fs/cgroup/system.slice/cron.service/pids.current'
   ```

4. Check logs for the initialization message:
   ```
   Initialized process semaphore with max X concurrent processes
   ```

## Performance Impact

- **CPU**: Slightly reduced CPU usage compared to unlimited concurrency
- **Memory**: Minimal impact, only synchronization overhead
- **Time**: Backup duration may increase slightly if previous limit was very high
- **Reliability**: Significantly improved - eliminates fork failures

## Notes

- This fix specifically addresses table-based backups (`backupAsTables: true`)
- The semaphore only controls table dump operations, not the upload phase
- The default value of 10 is conservative and should work for most systems
- Adjust based on your specific workload and cgroup configuration
