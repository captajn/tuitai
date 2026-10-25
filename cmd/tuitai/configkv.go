package main

// configkv.go: áp dụng "k=v" từ `tuitai config --set` vào cấu hình.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/captajn/tuitai/internal/config"
	"github.com/captajn/tuitai/internal/schedule"
)

// applyConfigKV gán một khoá cấu hình theo tên thân thiện.
func applyConfigKV(c *config.Config, key, val string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "dir", "download_dir", "downloaddir":
		c.DownloadDir = val
	case "threads":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("threads phải là số: %q", val)
		}
		c.Threads = n
	case "max_concurrent", "concurrent":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("max_concurrent phải là số: %q", val)
		}
		c.MaxConcurrent = n
	case "rate", "rate_limit":
		n, err := parseRate(val)
		if err != nil {
			return err
		}
		c.RateLimit = n
	case "per_job_rate":
		n, err := parseRate(val)
		if err != nil {
			return err
		}
		c.PerJobRateLimit = n
	case "proxy":
		c.Proxy = val
	case "user_agent", "ua":
		c.UserAgent = val
	case "referer":
		c.Referer = val
	case "retries":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("retries phải là số: %q", val)
		}
		c.Retries = n
	case "chunk_bytes":
		n, err := parseRate(val)
		if err != nil {
			return err
		}
		c.ChunkBytes = n
	case "resume":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("resume phải là true/false: %q", val)
		}
		c.Resume = b
	case "organize", "organize_by_type":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("organize phải là true/false: %q", val)
		}
		c.OrganizeByType = b
	case "schedule", "schedule_window":
		if strings.TrimSpace(val) == "" {
			c.ScheduleWindow = ""
			break
		}
		if _, err := schedule.ParseDaily(val); err != nil {
			return err
		}
		c.ScheduleWindow = strings.TrimSpace(val)
	case "offpeak_rate":
		n, err := parseRate(val)
		if err != nil {
			return err
		}
		c.OffPeakRate = n
	case "notify":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("notify phải là true/false: %q", val)
		}
		c.NotifyOnComplete = b
	case "category_dir":
		// dạng video=Phim
		i := strings.IndexByte(val, '=')
		if i <= 0 {
			return fmt.Errorf("category_dir cần dạng loại=ThưMục, nhận %q", val)
		}
		if c.CategoryDirs == nil {
			c.CategoryDirs = map[string]string{}
		}
		c.CategoryDirs[strings.ToLower(strings.TrimSpace(val[:i]))] = strings.TrimSpace(val[i+1:])
	case "verify_tls":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("verify_tls phải là true/false: %q", val)
		}
		c.VerifyTLS = b
	case "timeout":
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("timeout phải dạng 30s/2m: %q", val)
		}
		c.Timeout = d
	case "mirror":
		c.Mirrors = append(c.Mirrors, val)
	default:
		return fmt.Errorf("khoá không hỗ trợ %q", key)
	}
	return nil
}

// parseRate hiểu "500k", "2M", "1.5M", hoặc số byte thô.
func parseRate(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "k"), strings.HasSuffix(s, "kb"):
		mult = 1000
		s = strings.TrimSuffix(strings.TrimSuffix(s, "b"), "k")
	case strings.HasSuffix(s, "kib"):
		mult = 1024
		s = strings.TrimSuffix(s, "kib")
	case strings.HasSuffix(s, "m"), strings.HasSuffix(s, "mb"):
		mult = 1000 * 1000
		s = strings.TrimSuffix(strings.TrimSuffix(s, "b"), "m")
	case strings.HasSuffix(s, "mib"):
		mult = 1024 * 1024
		s = strings.TrimSuffix(s, "mib")
	case strings.HasSuffix(s, "g"), strings.HasSuffix(s, "gb"):
		mult = 1000 * 1000 * 1000
		s = strings.TrimSuffix(strings.TrimSuffix(s, "b"), "g")
	case strings.HasSuffix(s, "gib"):
		mult = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "gib")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("giá trị số không hợp lệ: %q", s)
	}
	return int64(f * float64(mult)), nil
}
