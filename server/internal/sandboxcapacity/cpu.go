package sandboxcapacity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var cpuPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,3})?$`)

// CPU returns integer milli-CPUs, avoiding rounding in admission accounting.
// 0.01 CPU is the minimum CFS quota at the fixed 100ms period.
func CPU(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if !cpuPattern.MatchString(s) {
		return 0, fmt.Errorf("invalid CPU limit %q (use decimal cores, up to three decimal places)", raw)
	}
	parts := strings.SplitN(s, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > 1000000 {
		return 0, fmt.Errorf("CPU limit out of range")
	}
	fraction := int64(0)
	if len(parts) == 2 {
		fraction, _ = strconv.ParseInt(parts[1]+strings.Repeat("0", 3-len(parts[1])), 10, 64)
	}
	n := whole*1000 + fraction
	if n < 10 || n > 1000000000 {
		return 0, fmt.Errorf("CPU limit must be between 0.01 and 1000000 cores")
	}
	return n, nil
}
func FormatCPU(milli int64) string { return fmt.Sprintf("%d.%03d", milli/1000, milli%1000) }
func (p Policy) cpuLimits() (int64, int64, error) {
	total, def := p.TotalCPU, p.DefaultCPU
	if total == "" {
		total = "4"
	}
	if def == "" {
		def = "1"
	}
	t, err := CPU(total)
	if err != nil {
		return 0, 0, err
	}
	d, err := CPU(def)
	if err != nil {
		return 0, 0, err
	}
	if d > t {
		return 0, 0, fmt.Errorf("defaultCPU exceeds totalCPU")
	}
	return t, d, nil
}
func (m *Manager) CPU(request string) (int64, error) {
	if err := m.Policy.Validate(); err != nil {
		return 0, err
	}
	total, def, _ := m.Policy.cpuLimits()
	if request == "" {
		return def, nil
	}
	n, err := CPU(request)
	if err != nil {
		return 0, err
	}
	if n > total {
		return 0, fmt.Errorf("sandbox CPU request %s exceeds global CPU budget", request)
	}
	return n, nil
}

type reservation struct {
	Count    int   `json:"count,omitempty"`
	Memory   int64 `json:"memory"`
	MilliCPU int64 `json:"milliCPU"`
}

// Keep legacy records readable for Release, but never invent their CPU usage.
func (r *reservation) UnmarshalJSON(data []byte) error {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return json.Unmarshal(data, &r.Memory)
	}
	type plain reservation
	return json.Unmarshal(data, (*plain)(r))
}
