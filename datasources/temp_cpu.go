package datasources

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/cosandr/go-motd/utils"
	"github.com/shirou/gopsutil/host"
)

// CPUTempConf extends CommonConf with a list of containers to ignore
type CPUTempConf struct {
	CommonWithWarnConf `yaml:",inline"`
	Exec               bool `yaml:"useExec"`
}

// GetCPUTemp returns CPU core temps using gopsutil or parsing sensors output
func GetCPUTemp(ret chan<- string, c *CPUTempConf) {
	var header string
	var content string
	if c.Exec {
		header, content, _ = cpuTempSensors(c.Warn, c.Crit, *c.FailedOnly)
	} else {
		header, content, _ = cpuTempGopsutil(c.Warn, c.Crit, *c.FailedOnly)
	}
	// Pad header
	var p = utils.Pad{Delims: map[string]int{padL: c.Header[0], padR: c.Header[1]}, Content: header}
	header = p.Do()
	if len(content) == 0 {
		ret <- header
		return
	}
	// Pad container list
	p = utils.Pad{Delims: map[string]int{padL: c.Content[0], padR: c.Content[1]}, Content: content}
	content = p.Do()
	ret <- header + "\n" + content
}

func cpuTempGopsutil(warnTemp int, critTemp int, warnOnly bool) (header string, content string, err error) {
	temps, err := host.SensorsTemperatures()
	if err != nil {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("CPU temp", padL, padR), utils.Warn("unavailable"))
		return
	}
	reCore := regexp.MustCompile(`coretemp\_core(\d+)\_input`)
	var tempMap = make(map[string]int)
	var sortedCPUs []string
	for _, stat := range temps {
		m := reCore.FindStringSubmatch(stat.SensorKey)
		if len(m) > 1 {
			tempMap[m[1]] = int(stat.Temperature)
			sortedCPUs = append(sortedCPUs, m[1])
		}
	}
	sort.Strings(sortedCPUs)
	var warnCount int
	var errCount int
	for _, k := range sortedCPUs {
		var v = tempMap[k]
		var wrapped = utils.Wrap(fmt.Sprintf("Core %s", k), padL, padR)
		if v < warnTemp && !warnOnly {
			content += fmt.Sprintf("%s: %s\n", wrapped, utils.Good(v))
		} else if v >= warnTemp && v < critTemp {
			content += fmt.Sprintf("%s: %s\n", wrapped, utils.Warn(v))
			warnCount++
		} else if v >= critTemp {
			warnCount++
			errCount++
			content += fmt.Sprintf("%s: %s\n", wrapped, utils.Err(v))
		}
	}
	if warnCount == 0 {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("CPU temp", padL, padR), utils.Good("OK"))
	} else if errCount > 0 {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("CPU temp", padL, padR), utils.Err("Critical"))
	} else if warnCount > 0 {
		header = fmt.Sprintf("%s: %s\n", utils.Wrap("CPU temp", padL, padR), utils.Warn("Warning"))
	}
	return
}
